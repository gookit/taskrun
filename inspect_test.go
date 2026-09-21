package taskrun

import (
	"context"
	"testing"
)

func TestInspectHasNoSideEffects(t *testing.T) {
	var hostCalls, engineCalls int
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Steps: []Step{
				{Task: &TaskCall{Name: "child"}},
				{Host: &HostSpec{Name: "capture"}},
				{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}},
			}},
			"child": {Deps: []string{"dep"}, Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "exit 42"}}}},
			"dep":   {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}},
		},
	}, WithEngine(&countingEngine{inner: ProcessEngine{}, runs: &engineCalls}),
		WithHandler("capture", func(_ context.Context, _ HostCall) (ActionResult, error) {
			hostCalls++
			return ActionResult{}, nil
		}))
	plan, err := runner.Inspect(context.Background(), Request{Task: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if hostCalls != 0 || engineCalls != 0 {
		t.Fatalf("inspect ran side effects: host=%d engine=%d", hostCalls, engineCalls)
	}
	// Deps run before the parent steps, so their action is planned first.
	kinds := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		kinds = append(kinds, action.Kind)
	}
	want := []string{"exec", "shell", "host", "exec"}
	if len(kinds) != len(want) {
		t.Fatalf("actions=%+v", plan.Actions)
	}
	for i, kind := range want {
		if kinds[i] != kind {
			t.Fatalf("order=%v want=%v", kinds, want)
		}
	}
}

func TestDryRunResultCarriesPlanOnly(t *testing.T) {
	var engineCalls int
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}}},
	}, WithEngine(&countingEngine{inner: ProcessEngine{}, runs: &engineCalls}))
	result := mustRun(t, runner, Request{Task: "t", DryRun: true})
	if result.Status != StatusDryRun {
		t.Fatalf("status=%s", result.Status)
	}
	if result.Plan == nil || len(result.Plan.Actions) != 1 {
		t.Fatalf("plan=%+v", result.Plan)
	}
	if len(result.Steps) != 0 || engineCalls != 0 {
		t.Fatalf("dry run executed: steps=%d calls=%d", len(result.Steps), engineCalls)
	}
}

func TestInspectMarksDynamicValuesAsDeferred(t *testing.T) {
	if testing.Short() {
		t.Skip("shell fixture")
	}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			DynamicVars: map[string]DynamicVar{"stamp": {Shell: &ShellSpec{Name: "sh", Script: "printf x"}}},
			If:          "vars.stamp != \"\"",
			Steps:       []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf ${vars.stamp}"}}},
		}},
	})
	plan, err := runner.Inspect(context.Background(), Request{Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Deferred) == 0 {
		t.Fatalf("plan=%+v", plan)
	}
	var deferredAction bool
	for _, action := range plan.Actions {
		if action.Status == StatusDeferred {
			deferredAction = true
		}
	}
	if !deferredAction {
		t.Fatalf("action was not deferred: %+v", plan.Actions)
	}
}

func TestInspectEvaluatesStaticConditions(t *testing.T) {
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Steps: []Step{
				{Task: &TaskCall{Name: "on"}},
				{Task: &TaskCall{Name: "off"}},
			}},
			"on":  {If: "true", Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}},
			"off": {If: "false", Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}},
		},
	})
	plan, err := runner.Inspect(context.Background(), Request{Task: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("actions=%+v", plan.Actions)
	}
	if len(plan.Skipped) == 0 {
		t.Fatalf("skipped conditions not reported: %+v", plan)
	}
}

func TestInspectValidatesArgumentReferences(t *testing.T) {
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"${args.2}"}}}}}},
	})
	if _, err := runner.Inspect(context.Background(), Request{Task: "t", Args: []string{"one"}}); err == nil {
		t.Fatal("expected argument validation error")
	}
	if _, err := runner.Inspect(context.Background(), Request{Task: "t", Args: []string{"one", "two"}}); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
}
