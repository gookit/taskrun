package taskrun

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDynamicVarActionVariants covers the three action kinds a dynamic variable
// may use and the two invalid shapes.
func TestDynamicVarActionVariants(t *testing.T) {
	t.Run("exec", func(t *testing.T) {
		var seen any
		runner := newTestRunner(t, Definition{
			Tasks: map[string]Task{"t": {
				DynamicVars: map[string]DynamicVar{
					"target": {Exec: &ExecSpec{Program: "go", Args: []string{"env", "GOOS"}}},
				},
				Steps: []Step{{Host: &HostSpec{Name: "capture", Args: []any{"${vars.target}"}}}},
			}},
		}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
			seen = call.Args[0]
			return ActionResult{}, nil
		}))
		mustRun(t, runner, Request{Task: "t"})
		if seen == nil || seen == "" {
			t.Fatalf("exec dynamic value = %v", seen)
		}
	})

	t.Run("file", func(t *testing.T) {
		if testing.Short() {
			t.Skip("compiles a Go program")
		}
		goBin, err := exec.LookPath("go")
		if err != nil {
			t.Skip("go toolchain is unavailable")
		}
		dir := t.TempDir()
		script := filepath.Join(dir, "scripts", "stamp.go")
		if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(script, []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Print(\"from-file\") }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var seen any
		runner := newTestRunner(t, Definition{
			BaseDir: dir,
			Files: map[string]ScriptFile{
				"stamp": {Path: "scripts/stamp.go", Interpreter: Interpreter{Program: goBin, PrefixArgs: []string{"run"}}},
			},
			Tasks: map[string]Task{"t": {
				DynamicVars: map[string]DynamicVar{"stamp": {File: &FileSpec{Name: "stamp"}}},
				Steps:       []Step{{Host: &HostSpec{Name: "capture", Args: []any{"${vars.stamp}"}}}},
			}},
		}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
			seen = call.Args[0]
			return ActionResult{}, nil
		}))
		mustRun(t, runner, Request{Task: "t"})
		if seen != "from-file" {
			t.Fatalf("file dynamic value = %v", seen)
		}
	})

	t.Run("unknown file", func(t *testing.T) {
		assertDynamicVarRejected(t, DynamicVar{File: &FileSpec{Name: "missing"}})
	})

	t.Run("no action", func(t *testing.T) {
		assertDynamicVarRejected(t, DynamicVar{})
	})
}

// assertDynamicVarRejected accepts the rejection either at New (pre-flight) or
// at Run, and requires an invalid_definition classification. The step references
// the variable so the runtime path is reachable as well.
func assertDynamicVarRejected(t *testing.T, spec DynamicVar) {
	t.Helper()
	def := Definition{Version: 1, BaseDir: t.TempDir(), Tasks: map[string]Task{"t": {
		DynamicVars: map[string]DynamicVar{"x": spec},
		Steps:       []Step{{Name: "s", Host: &HostSpec{Name: "capture", Args: []any{"${vars.x}"}}}},
	}}}
	if _, err := New(def); err != nil {
		if !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("New error = %v, want invalid_definition", err)
		}
		return
	}
	runner := newTestRunner(t, def, WithHandler("capture", func(context.Context, HostCall) (ActionResult, error) {
		return ActionResult{}, nil
	}))
	if _, err := runner.Run(context.Background(), Request{Task: "t"}); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("Run error = %v, want invalid_definition", err)
	}
}
