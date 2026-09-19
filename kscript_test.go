package kscript

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestRunner(t *testing.T, def Definition, opts ...Option) *Runner {
	t.Helper()
	if def.BaseDir == "" {
		def.BaseDir = t.TempDir()
	}
	if def.Version == 0 {
		def.Version = 1
	}
	runner, err := New(def, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return runner
}

func mustRun(t *testing.T, runner *Runner, req Request) *Result {
	t.Helper()
	result, err := runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

// echoExec is a portable action used when a test only needs "it ran".
func echoExec() *ExecSpec { return &ExecSpec{Program: "go", Args: []string{"version"}} }

func TestNewFreezesDefinitionSnapshot(t *testing.T) {
	def := Definition{
		BaseDir: t.TempDir(),
		Vars:    map[string]any{"a": "1"},
		Tasks: map[string]Task{
			"t": {Vars: map[string]any{"b": "2"}, Steps: []Step{{Exec: echoExec()}}},
		},
	}
	runner := newTestRunner(t, def)
	// Mutating the caller's definition must not affect the runner.
	def.Vars["a"] = "mutated"
	def.Tasks["t"].Vars["b"] = "mutated"
	def.Tasks["t"].Steps[0].Exec.Args = []string{"mutated"}

	got, err := runner.Lookup("t")
	if err != nil {
		t.Fatal(err)
	}
	if got.Vars["b"] != "2" || got.Steps[0].Exec.Args[0] != "version" {
		t.Fatalf("definition was not frozen: %+v", got)
	}
	// Mutating a returned Lookup value must not affect the runner either.
	got.Vars["b"] = "leaked"
	again, _ := runner.Lookup("t")
	if again.Vars["b"] != "2" {
		t.Fatalf("Lookup returned shared state: %+v", again)
	}
}

func TestBaseDirMustBeAbsolute(t *testing.T) {
	_, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{"t": {Steps: []Step{{Exec: echoExec()}}}}})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestDefinitionVarsAndPriority(t *testing.T) {
	var seen map[string]any
	base := t.TempDir()
	runner := newTestRunner(t, Definition{
		BaseDir: base,
		Vars:    map[string]any{"a": "def", "d": "def"},
		Tasks: map[string]Task{
			"t": {
				Vars: map[string]any{"b": "task"},
				Steps: []Step{{
					Vars: map[string]any{"c": "step"},
					Host: &HostSpec{Name: "capture"},
				}},
			},
		},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		seen = call.Vars
		return ActionResult{}, nil
	}))
	mustRun(t, runner, Request{Task: "t", Vars: map[string]any{"d": "request"}})
	if seen["a"] != "def" || seen["b"] != "task" || seen["c"] != "step" || seen["d"] != "request" {
		t.Fatalf("vars=%v", seen)
	}
}

func TestVarLevelOrder(t *testing.T) {
	r := newTestRunner(t, Definition{
		Vars: map[string]any{
			"first":  "1",
			"second": "${vars.first}-2",
			"third":  "${vars.second}-3",
		},
		Tasks: map[string]Task{
			"t": {Steps: []Step{{Host: &HostSpec{Name: "capture"}}}},
		},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		if call.Vars["third"] != "1-2-3" {
			return ActionResult{}, fmt.Errorf("third=%v", call.Vars["third"])
		}
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t"})
}

func TestVarCycleIsRejected(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Vars:    map[string]any{"a": "${vars.b}", "b": "${vars.a}"},
		Tasks:   map[string]Task{"t": {Steps: []Step{{Exec: echoExec()}}}},
	})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestUnknownVariableReferenceFails(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"${vars.nope}"}}}}}},
	})
	_, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestArgsIndexIsOneBasedAndBounded(t *testing.T) {
	var got []string
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Host: &HostSpec{Name: "capture", Args: []any{"${args.1}", "${args.2}"}}}}}},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		for _, arg := range call.Args {
			got = append(got, arg.(string))
		}
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t", Args: []string{"one", "two"}})
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("got=%v", got)
	}
	_, err := r.Run(context.Background(), Request{Task: "t", Args: []string{"one"}})
	if err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestTemplateEscapeAndEnvNamespace(t *testing.T) {
	var seen []string
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Env:   map[string]string{"KS_LABEL": "label"},
			Steps: []Step{{Host: &HostSpec{Name: "capture", Args: []any{"$${literal}", "${env.KS_LABEL}"}}}},
		}},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		seen = []string{call.Args[0].(string), call.Args[1].(string)}
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t"})
	if seen[0] != "${literal}" || seen[1] != "label" {
		t.Fatalf("seen=%v", seen)
	}
}

func TestConditionNamespaces(t *testing.T) {
	var ran bool
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Vars:  map[string]any{"enabled": true},
			If:    `vars.enabled == true && env.MODE == "dev" && args[0] == "x" && host.flag == true && run.dir != ""`,
			Steps: []Step{{Host: &HostSpec{Name: "capture"}}},
		}},
	}, WithHandler("capture", func(_ context.Context, _ HostCall) (ActionResult, error) {
		ran = true
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t", Args: []string{"x"}, Env: map[string]string{"MODE": "dev"}, HostData: map[string]any{"flag": true}})
	if !ran {
		t.Fatal("condition should have been true")
	}
}

func TestBareConditionStillSupported(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {If: "enabled", Vars: map[string]any{"enabled": true}, Steps: []Step{{Exec: echoExec()}}}},
	})
	if result := mustRun(t, r, Request{Task: "t"}); result.Status != StatusSucceeded {
		t.Fatalf("status=%s", result.Status)
	}
}

func TestConditionMustBeBool(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {If: "name", Vars: map[string]any{"name": "text"}, Steps: []Step{{Exec: echoExec()}}}},
	})
	_, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestSkippedRootTask(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {If: "false", Steps: []Step{{Exec: &ExecSpec{Program: "definitely-not-a-program"}}}}},
	})
	result := mustRun(t, r, Request{Task: "t"})
	if result.Status != StatusSkipped {
		t.Fatalf("status=%s", result.Status)
	}
}

func TestSkippedDependencyIsObservableAndNonBlocking(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"a": {Deps: []string{"b"}, Steps: []Step{{Exec: echoExec()}}},
			"b": {If: "enabled", Vars: map[string]any{"enabled": false}, Steps: []Step{{Exec: echoExec()}}},
		},
	})
	result := mustRun(t, r, Request{Task: "a"})
	if result.Status != StatusSucceeded {
		t.Fatalf("status=%s", result.Status)
	}
	var skipped *TaskResult
	for i := range result.Tasks {
		if result.Tasks[i].Name == "b" && result.Tasks[i].Status == StatusSkipped {
			skipped = &result.Tasks[i]
		}
	}
	if skipped == nil || skipped.Reason != "condition false" {
		t.Fatalf("tasks=%+v", result.Tasks)
	}
}

func TestPlatformSkip(t *testing.T) {
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "windows"
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Platform: []string{other}, Steps: []Step{{Exec: &ExecSpec{Program: "definitely-not-a-program"}}}}},
	})
	result := mustRun(t, r, Request{Task: "t"})
	if result.Status != StatusSkipped || len(result.Tasks) != 1 || result.Tasks[0].Status != StatusSkipped {
		t.Fatalf("result=%+v", result)
	}
}

func TestCycleIsRejectedAtNewWithPath(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Tasks: map[string]Task{
			"a": {Steps: []Step{{Exec: echoExec()}, {Task: &TaskCall{Name: "b"}}}},
			"b": {Deps: []string{"c"}, Steps: []Step{{Exec: echoExec()}}},
			"c": {Deps: []string{"a"}, Steps: []Step{{Exec: echoExec()}}},
		},
	})
	if err == nil {
		t.Fatal("expected cycle")
	}
	if !errors.Is(err, ErrDependencyCycle) || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "a -> b -> c -> a") {
		t.Fatalf("cycle path missing: %v", err)
	}
}

func TestCycleInConditionFalseTaskStillRejected(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Tasks: map[string]Task{
			"a": {If: "false", Steps: []Step{{Task: &TaskCall{Name: "b"}}}},
			"b": {Deps: []string{"a"}, Steps: []Step{{Exec: echoExec()}}},
		},
	})
	if err == nil || !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("err=%v", err)
	}
}

func TestMaxCallDepthRejectedAtNew(t *testing.T) {
	tasks := map[string]Task{}
	for i := 0; i < 8; i++ {
		next := fmt.Sprintf("t%d", i+1)
		tasks[fmt.Sprintf("t%d", i)] = Task{Deps: []string{next}, Steps: []Step{{Exec: echoExec()}}}
	}
	tasks["t8"] = Task{Steps: []Step{{Exec: echoExec()}}}
	_, err := New(Definition{BaseDir: t.TempDir(), Tasks: tasks}, WithMaxCallDepth(3))
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestExpansionLimitAtRuntime(t *testing.T) {
	steps := make([]Step, 0, 10)
	for i := 0; i < 10; i++ {
		steps = append(steps, Step{Task: &TaskCall{Name: "leaf"}})
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Steps: steps},
			"leaf": {Steps: []Step{{Exec: echoExec()}}},
		},
	}, WithMaxExpansions(4))
	_, err := r.Run(context.Background(), Request{Task: "root"})
	if err == nil || !errors.Is(err, ErrExpansionLimit) {
		t.Fatalf("err=%v", err)
	}
}

func TestDiamondDependencyRunsTwice(t *testing.T) {
	var count int
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"a": {Deps: []string{"b", "c"}, Steps: []Step{{Exec: echoExec()}}},
			"b": {Deps: []string{"d"}, Steps: []Step{{Exec: echoExec()}}},
			"c": {Deps: []string{"d"}, Steps: []Step{{Exec: echoExec()}}},
			"d": {Steps: []Step{{Host: &HostSpec{Name: "count"}}}},
		},
	}, WithHandler("count", func(_ context.Context, _ HostCall) (ActionResult, error) {
		count++
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "a"})
	if count != 2 {
		t.Fatalf("shared dependency ran %d times, want 2", count)
	}
}

func TestTaskCallArgsInheritanceAndOverride(t *testing.T) {
	recorded := map[string]bool{}
	child := func(name, condition string) Task {
		return Task{If: condition, Steps: []Step{{
			Vars: map[string]any{"who": name},
			Host: &HostSpec{Name: "record"},
		}}}
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Steps: []Step{
				{Task: &TaskCall{Name: "inherit"}},
				{Task: &TaskCall{Name: "empty", Args: []string{}}},
				{Task: &TaskCall{Name: "explicit", Args: []string{"explicit"}}},
				{Task: &TaskCall{Name: "forward", Args: []string{"a"}, ForwardArgs: true}},
			}},
			"inherit":  child("inherit", `len(args) == 2 && args[1] == "req2"`),
			"empty":    child("empty", `len(args) == 0`),
			"explicit": child("explicit", `len(args) == 1 && args[0] == "explicit"`),
			"forward":  child("forward", `len(args) == 3 && args[0] == "a" && args[1] == "req1" && args[2] == "req2"`),
		},
	}, WithHandler("record", func(_ context.Context, call HostCall) (ActionResult, error) {
		if who, ok := call.Vars["who"].(string); ok {
			recorded[who] = true
		}
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "root", Args: []string{"req1", "req2"}})
	for _, name := range []string{"inherit", "empty", "explicit", "forward"} {
		if !recorded[name] {
			t.Fatalf("call %s did not observe the expected arguments; recorded=%v", name, recorded)
		}
	}
}

func TestChildDoesNotInheritParentLocalVars(t *testing.T) {
	var child map[string]any
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"parent": {Vars: map[string]any{"local": "parent-only"}, Steps: []Step{{Task: &TaskCall{Name: "child"}}}},
			"child":  {Steps: []Step{{Host: &HostSpec{Name: "capture"}}}},
		},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		child = call.Vars
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "parent", Vars: map[string]any{"global": "yes"}})
	if _, leaked := child["local"]; leaked {
		t.Fatalf("child inherited parent local vars: %v", child)
	}
	if child["global"] != "yes" {
		t.Fatalf("child lost request vars: %v", child)
	}
}

func TestEnvPriorityAndCleanEnv(t *testing.T) {
	var seen map[string]string
	r := newTestRunner(t, Definition{
		Env: map[string]string{"KS_LEVEL": "definition", "KS_DEF": "1"},
		Tasks: map[string]Task{
			"t": {
				Env: map[string]string{"KS_LEVEL": "task"},
				Steps: []Step{{
					Env:  map[string]string{"KS_LEVEL": "step"},
					Host: &HostSpec{Name: "capture"},
				}},
			},
			"clean": {
				CleanEnv: true,
				Env:      map[string]string{"KS_LEVEL": "task"},
				Steps:    []Step{{Host: &HostSpec{Name: "capture"}}},
			},
		},
	}, WithBaseEnv(map[string]string{"KS_BASE": "1", "KS_LEVEL": "base"}), WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		seen = call.Env
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t", Env: map[string]string{"KS_LEVEL": "request"}})
	if seen["KS_LEVEL"] != "request" || seen["KS_BASE"] != "1" || seen["KS_DEF"] != "1" {
		t.Fatalf("env=%v", seen)
	}
	mustRun(t, r, Request{Task: "clean"})
	if _, ok := seen["KS_BASE"]; ok {
		t.Fatalf("CleanEnv kept the base env: %v", seen)
	}
	if seen["KS_LEVEL"] != "task" {
		t.Fatalf("CleanEnv dropped explicit env: %v", seen)
	}
}

func TestEnvPathsPrependToPath(t *testing.T) {
	var path string
	r := newTestRunner(t, Definition{
		EnvPaths: []string{"/definition"},
		Tasks: map[string]Task{"t": {
			EnvPaths: []string{"/task"},
			Steps: []Step{{
				EnvPaths: []string{"/step"},
				Host:     &HostSpec{Name: "capture"},
			}},
		}},
	}, WithBaseEnv(map[string]string{"PATH": "/base"}), WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		path = call.Env["PATH"]
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t"})
	want := strings.Join([]string{"/step", "/task", "/definition", "/base"}, string(os.PathListSeparator))
	if path != want {
		t.Fatalf("PATH=%q want %q", path, want)
	}
}

func TestDirResolution(t *testing.T) {
	base := t.TempDir()
	var dirs []string
	r := newTestRunner(t, Definition{
		BaseDir: base,
		Tasks: map[string]Task{"t": {
			Dir: "taskdir",
			Steps: []Step{
				{Dir: "stepdir", Host: &HostSpec{Name: "capture"}},
				{Dir: base, Host: &HostSpec{Name: "capture"}},
			},
		}},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		dirs = append(dirs, call.Dir)
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t"})
	if dirs[0] != base+string(os.PathSeparator)+"taskdir"+string(os.PathSeparator)+"stepdir" {
		t.Fatalf("step dir=%s", dirs[0])
	}
	if dirs[1] != base {
		t.Fatalf("absolute dir=%s", dirs[1])
	}
}

func TestIgnoreErrorToleratesExitCodeOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix exit code fixture")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{
			{IgnoreError: true, Shell: &ShellSpec{Name: "sh", Script: "exit 3"}},
			{Shell: &ShellSpec{Name: "sh", Script: "printf ok"}},
		}}},
	})
	var out strings.Builder
	result := mustRun(t, r, Request{Task: "t", IO: IO{Stdout: &out, CaptureLimit: 64}})
	if result.Status != StatusSucceededWithWarnings {
		t.Fatalf("status=%s", result.Status)
	}
	if result.Steps[0].Status != StatusIgnoredFailure {
		t.Fatalf("step=%+v", result.Steps[0])
	}
	if out.String() != "ok" {
		t.Fatalf("later step did not run: %q", out.String())
	}
}

func TestIgnoreErrorDoesNotTolerateStartFailure(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{IgnoreError: true, Exec: &ExecSpec{Program: "definitely-not-a-program"}}}}},
	})
	_, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrStart) {
		t.Fatalf("err=%v", err)
	}
}

func TestNoImplicitRetry(t *testing.T) {
	var count int
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Host: &HostSpec{Name: "fail"}}}}},
	}, WithHandler("fail", func(_ context.Context, _ HostCall) (ActionResult, error) {
		count++
		return ActionResult{}, errors.New("boom")
	}))
	_, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrHandler) {
		t.Fatalf("err=%v", err)
	}
	if count != 1 {
		t.Fatalf("handler ran %d times", count)
	}
}

func TestUnknownRootTaskUsesErrNotFound(t *testing.T) {
	r := newTestRunner(t, Definition{Tasks: map[string]Task{"t": {Steps: []Step{{Exec: echoExec()}}}}})
	_, err := r.Run(context.Background(), Request{Task: "missing"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Kind != ErrKindNotFound {
		t.Fatalf("err=%#v", err)
	}
}

func TestResultStatusNeverContradictsError(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{
			{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}},
			{Exec: &ExecSpec{Program: "definitely-not-a-program"}},
		}}},
	})
	result, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil {
		t.Fatal("expected error")
	}
	if result.Status != StatusFailed {
		t.Fatalf("status=%s with err=%v", result.Status, err)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps=%+v", result.Steps)
	}
}

func TestListIsStableAndSorted(t *testing.T) {
	r := newTestRunner(t, Definition{Tasks: map[string]Task{
		"z": {Steps: []Step{{Exec: echoExec()}}},
		"a": {Steps: []Step{{Exec: echoExec()}}},
	}})
	items := r.List()
	if len(items) != 2 || items[0].Name != "a" || items[1].Name != "z" {
		t.Fatalf("items=%+v", items)
	}
}

func TestConcurrentRunsAreIsolated(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			If:    `vars.who == env.WHO`,
			Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf %s \"${vars.who}\""}}},
		}},
	})
	done := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			who := fmt.Sprintf("w%d", i)
			var out strings.Builder
			_, err := r.Run(context.Background(), Request{
				Task: "t", Vars: map[string]any{"who": who}, Env: map[string]string{"WHO": who},
				IO: IO{Stdout: &out, CaptureLimit: 64},
			})
			if err != nil {
				done <- "err:" + err.Error()
				return
			}
			done <- out.String()
		}(i)
	}
	for i := 0; i < 8; i++ {
		got := <-done
		if strings.HasPrefix(got, "err:") {
			t.Fatal(got)
		}
	}
}

func TestDynamicVarIsEvaluatedOnceAndTrimmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	var runs int
	counting := &countingEngine{inner: ProcessEngine{}, runs: &runs}
	var seen any
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			DynamicVars: map[string]DynamicVar{"stamp": {Shell: &ShellSpec{Name: "sh", Script: "printf 'x\\n\\n'"}}},
			Steps:       []Step{{Host: &HostSpec{Name: "capture", Args: []any{"${vars.stamp}"}}}},
		}},
	}, WithEngine(counting), WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		seen = call.Args[0]
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t"})
	if seen != "x" {
		t.Fatalf("dynamic value=%q", seen)
	}
	if runs != 1 {
		t.Fatalf("dynamic command ran %d times", runs)
	}
}

func TestDynamicVarOutputLimitFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			DynamicVars: map[string]DynamicVar{"big": {Shell: &ShellSpec{Name: "sh", Script: "printf '%0.sx' 1 2 3 4 5 6 7 8"}}},
			Steps:       []Step{{Host: &HostSpec{Name: "capture", Args: []any{"${vars.big}"}}}},
		}},
	}, WithDynamicOutputLimit(4), WithHandler("capture", func(_ context.Context, _ HostCall) (ActionResult, error) {
		return ActionResult{}, nil
	}))
	_, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("err=%v", err)
	}
}

func TestDuplicateStepNamesRejected(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Tasks: map[string]Task{"t": {Steps: []Step{
			{Name: "same", Exec: echoExec()},
			{Name: "same", Exec: echoExec()},
		}}},
	})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestStepRequiresExactlyOneAction(t *testing.T) {
	if _, err := New(Definition{BaseDir: t.TempDir(), Tasks: map[string]Task{"t": {Steps: []Step{{Name: "x"}}}}}); err == nil {
		t.Fatal("expected empty action error")
	}
	if _, err := New(Definition{BaseDir: t.TempDir(), Tasks: map[string]Task{"t": {Steps: []Step{{
		Exec:  echoExec(),
		Shell: &ShellSpec{Name: "sh", Script: "true"},
	}}}}}); err == nil {
		t.Fatal("expected multiple action error")
	}
}

func TestUnregisteredHandlerRejected(t *testing.T) {
	_, err := New(Definition{BaseDir: t.TempDir(), Tasks: map[string]Task{"t": {Steps: []Step{{Host: &HostSpec{Name: "missing"}}}}}})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestUnknownScriptFileRejected(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Tasks:   map[string]Task{"t": {Steps: []Step{{File: &FileSpec{Name: "missing"}}}}},
	})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestScriptFilePathIsAbsolute(t *testing.T) {
	base := t.TempDir()
	r := newTestRunner(t, Definition{
		BaseDir: base,
		Files: map[string]ScriptFile{
			"gen": {Path: "scripts/gen.go", Interpreter: Interpreter{Program: "go", PrefixArgs: []string{"run"}}},
		},
		Tasks: map[string]Task{"t": {Steps: []Step{{File: &FileSpec{Name: "gen"}}}}},
	})
	plan, err := r.Inspect(context.Background(), Request{Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("plan=%+v", plan)
	}
	if !strings.HasPrefix(plan.Actions[0].Args[1], base) {
		t.Fatalf("script path not absolute: %v", plan.Actions[0].Args)
	}
}

func TestUnsupportedShellRejected(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Tasks:   map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "fish", Script: "true"}}}}},
	})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestNegativeTimeoutRejected(t *testing.T) {
	_, err := New(Definition{
		BaseDir: t.TempDir(),
		Tasks:   map[string]Task{"t": {Timeout: -time.Second, Steps: []Step{{Exec: echoExec()}}}},
	})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestEmptyTaskRejected(t *testing.T) {
	_, err := New(Definition{BaseDir: t.TempDir(), Tasks: map[string]Task{"t": {}}})
	if err == nil || !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectsUnsupportedRequestData(t *testing.T) {
	r := newTestRunner(t, Definition{Tasks: map[string]Task{"t": {Steps: []Step{{Exec: echoExec()}}}}})
	_, err := r.Run(context.Background(), Request{Task: "t", Vars: map[string]any{"f": func() {}}})
	if err == nil || !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

// TestNestedVariablePathsRender covers legacy style nested access such as
// ${vars.gvs.app} and ${vars.time.datetime}.
func TestNestedVariablePathsRender(t *testing.T) {
	var seen []string
	r := newTestRunner(t, Definition{
		Vars: map[string]any{
			"gvs":  map[string]any{"app": "kite", "deep": map[string]any{"n": int64(2)}},
			"time": map[string]any{"datetime": "2026-09-19 00:00:00"},
		},
		Tasks: map[string]Task{"t": {Steps: []Step{{Host: &HostSpec{
			Name: "capture",
			Args: []any{"${vars.gvs.app}", "${vars.gvs.deep.n}", "${vars.time.datetime}"},
		}}}}},
	}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
		for _, arg := range call.Args {
			seen = append(seen, arg.(string))
		}
		return ActionResult{}, nil
	}))
	mustRun(t, r, Request{Task: "t"})
	want := []string{"kite", "2", "2026-09-19 00:00:00"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen=%v want=%v", seen, want)
		}
	}
	// A missing nested segment is still an error.
	bad := newTestRunner(t, Definition{
		Vars:  map[string]any{"gvs": map[string]any{"app": "kite"}},
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"${vars.gvs.missing}"}}}}}},
	})
	if _, err := bad.Run(context.Background(), Request{Task: "t"}); err == nil {
		t.Fatal("expected an unknown reference error")
	}
}

type countingEngine struct {
	inner Engine
	runs  *int
}

func (e *countingEngine) Execute(ctx context.Context, action PreparedAction, streams IO) (ActionResult, error) {
	*e.runs++
	return e.inner.Execute(ctx, action, streams)
}
