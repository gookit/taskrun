package taskrun

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// Environment values are templates: the design example sets
// KS_LABEL: "${vars.target}".
func TestEnvValuesAreRendered(t *testing.T) {
	var seen map[string]string
	runner := newTestRunner(t, Definition{
		Env: map[string]string{"KS_DEFINED": "${vars.project}"},
		Vars: map[string]any{
			"project": "taskrun",
		},
		Tasks: map[string]Task{"t": {
			Vars: map[string]any{"target": "./..."},
			Env:  map[string]string{"KS_LABEL": "${vars.target}", "KS_LOWER": "[${env.KS_DEFINED}]"},
			Steps: []Step{{
				Env:  map[string]string{"KS_STEP": "${vars.target}-step"},
				Host: &HostSpec{Name: "capture"},
			}},
		}},
	}, WithBaseEnv(map[string]string{"KS_BASE": "base"}), WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		seen = call.Env
		return ActionResult{}, nil
	}))
	mustRun(t, runner, Request{Task: "t", Env: map[string]string{"KS_REQUEST": "${vars.target}-request"}})
	want := map[string]string{
		"KS_BASE":    "base",
		"KS_DEFINED": "taskrun",
		"KS_LABEL":   "./...",
		"KS_LOWER":   "[taskrun]",
		"KS_STEP":    "./...-step",
		"KS_REQUEST": "./...-request",
	}
	for key, value := range want {
		if seen[key] != value {
			t.Fatalf("env[%s]=%q want %q (env=%v)", key, seen[key], value, seen)
		}
	}
}

func TestEnvValuesReachTheChildProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Vars:  map[string]any{"target": "rendered-env"},
			Env:   map[string]string{"KS_LABEL": "${vars.target}"},
			Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf %s \"$KS_LABEL\""}}},
		}},
	})
	result := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 64}})
	if string(result.Steps[0].Output) != "rendered-env" {
		t.Fatalf("output=%q", result.Steps[0].Output)
	}
}

func TestEnvPathsEntriesAreRendered(t *testing.T) {
	var seen string
	runner := newTestRunner(t, Definition{
		Vars: map[string]any{"bin": "/rendered/bin"},
		Tasks: map[string]Task{"t": {
			EnvPaths: []string{"${vars.bin}"},
			Steps:    []Step{{Host: &HostSpec{Name: "capture"}}},
		}},
	}, WithBaseEnv(map[string]string{"PATH": "/base"}), WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		seen = call.Env["PATH"]
		return ActionResult{}, nil
	}))
	mustRun(t, runner, Request{Task: "t"})
	if !strings.HasPrefix(seen, "/rendered/bin") {
		t.Fatalf("PATH=%q", seen)
	}
}

// Dynamic variables must not decide the process environment.
func TestDynamicVariableCannotDecideEnv(t *testing.T) {
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			DynamicVars: map[string]DynamicVar{"stamp": {Shell: &ShellSpec{Name: "sh", Script: "printf x"}}},
			Env:         map[string]string{"KS_STAMP": "${vars.stamp}"},
			Steps:       []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf ok"}}},
		}},
	})
	_, err := runner.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "dynamic") {
		t.Fatalf("unhelpful error: %v", err)
	}
	// Inspect must not fail; the field is reported as deferred.
	plan, err := runner.Inspect(context.Background(), Request{Task: "t"})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(plan.Deferred) == 0 {
		t.Fatalf("plan=%+v", plan)
	}
}

func TestUnknownReferenceInEnvFails(t *testing.T) {
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Env:   map[string]string{"KS_BAD": "${vars.nope}"},
			Steps: []Step{{Exec: &ExecSpec{Program: "go"}}},
		}},
	})
	if _, err := runner.Run(context.Background(), Request{Task: "t"}); err == nil {
		t.Fatal("expected an unknown variable error")
	}
}
