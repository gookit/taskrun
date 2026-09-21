package taskrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestErrorMessagesCarryCoordinates pins the text a user reads: the
// classification, the run coordinates and the preserved cause.
func TestErrorMessagesCarryCoordinates(t *testing.T) {
	cause := errors.New("exit status 2")
	runErr := &RunError{
		Kind: ErrKindExit, Task: "build", CallID: "call-1", Step: "compile",
		Source: "tasks.yml", Err: cause,
	}
	message := runErr.Error()
	for _, want := range []string{
		"taskrun: exit", "task=build", "call=call-1", "step=compile",
		"source=tasks.yml", "exit status 2",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("RunError.Error() = %q, want it to contain %q", message, want)
		}
	}
	if !errors.Is(runErr, ErrExit) {
		t.Error("RunError must match its sentinel")
	}
	if !errors.Is(runErr, cause) {
		t.Error("RunError must expose its cause")
	}
	if runErr.Unwrap() != cause {
		t.Error("RunError.Unwrap must return the cause")
	}

	// An unclassified error still renders, and must not match a sentinel.
	bare := &RunError{}
	if got := bare.Error(); got != "taskrun: error" {
		t.Errorf("empty RunError.Error() = %q, want %q", got, "taskrun: error")
	}
	if bare.Unwrap() != nil {
		t.Error("empty RunError.Unwrap() must be nil")
	}
	if errors.Is(bare, ErrExit) {
		t.Error("an empty kind must not match ErrExit")
	}

	processErr := &ProcessError{Kind: ErrKindStart, Err: cause}
	if got := processErr.Error(); !strings.Contains(got, "taskrun: start") || !strings.Contains(got, "exit status 2") {
		t.Errorf("ProcessError.Error() = %q", got)
	}
	if got := (&ProcessError{Kind: ErrKindTimedOut}).Error(); got != "taskrun: timed_out" {
		t.Errorf("ProcessError.Error() without a cause = %q", got)
	}
	if processErr.Unwrap() != cause {
		t.Error("ProcessError.Unwrap must return the cause")
	}
	if !errors.Is(processErr, ErrStart) || !errors.Is(processErr, cause) {
		t.Error("ProcessError must match its sentinel and its cause")
	}
}

// TestWithKillGraceIsValidated covers the option contract, including the
// rejected negative value and an explicit zero grace.
func TestWithKillGraceIsValidated(t *testing.T) {
	def := Definition{Version: 1, BaseDir: t.TempDir(), Tasks: map[string]Task{
		"t": {Steps: []Step{{Name: "s", Exec: echoExec()}}},
	}}

	if _, err := New(def, WithKillGrace(-time.Millisecond)); err == nil {
		t.Fatal("a negative kill grace was accepted")
	}
	runner := newTestRunner(t, def, WithKillGrace(0))
	if got := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 1 << 12}}).Status; got != StatusSucceeded {
		t.Fatalf("status=%s", got)
	}
}

// TestSourceReportsProvenance checks that Source returns the recorded loader
// provenance as a copy.
func TestSourceReportsProvenance(t *testing.T) {
	dir := t.TempDir()
	runner := newTestRunner(t, Definition{
		Sources: []Source{{Name: "tasks.yml", BaseDir: dir, Format: "yaml"}},
		Tasks:   map[string]Task{"t": {Steps: []Step{{Name: "s", Exec: echoExec()}}}},
	})
	got := runner.Source()
	if len(got) != 1 || got[0].Name != "tasks.yml" || got[0].Format != "yaml" {
		t.Fatalf("Source() = %+v", got)
	}
	got[0].Name = "mutated"
	if runner.Source()[0].Name != "tasks.yml" {
		t.Fatal("Source() must return a copy")
	}

	plain := newTestRunner(t, Definition{Tasks: map[string]Task{
		"t": {Steps: []Step{{Name: "s", Exec: echoExec()}}},
	}})
	if len(plain.Source()) != 0 {
		t.Fatalf("Source() = %+v, want none", plain.Source())
	}
}

// TestVarLevelVisibility pins which variables each level may reference:
// definition defaults are rendered before Request.Vars is merged, so they may
// not reference a request variable, while task and step levels may.
func TestVarLevelVisibility(t *testing.T) {
	t.Run("definition level cannot see request vars", func(t *testing.T) {
		runner := newTestRunner(t, Definition{
			Vars:  map[string]any{"repo": "${vars.target}/repo"},
			Tasks: map[string]Task{"t": {Steps: []Step{{Name: "s", Exec: echoExec()}}}},
		})
		_, err := runner.Run(context.Background(), Request{
			Task: "t", Vars: map[string]any{"target": "/tmp/work"},
		})
		if err == nil || !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("err=%v, want invalid_definition", err)
		}
		if !strings.Contains(err.Error(), "unknown variable") || !strings.Contains(err.Error(), "target") {
			t.Fatalf("unhelpful error: %v", err)
		}
	})

	t.Run("task and step levels can see request vars", func(t *testing.T) {
		var seen map[string]any
		runner := newTestRunner(t, Definition{
			Vars: map[string]any{"skill": "hello"},
			Tasks: map[string]Task{"t": {
				Vars: map[string]any{"repo": "${vars.target}/repo"},
				Steps: []Step{{
					Vars: map[string]any{"script": "${vars.repo}/skills/${vars.skill}/install.go"},
					Host: &HostSpec{Name: "capture"},
				}},
			}},
		}, WithHandler("capture", func(_ context.Context, call HostCall) (ActionResult, error) {
			seen = call.Vars
			return ActionResult{}, nil
		}))
		mustRun(t, runner, Request{Task: "t", Vars: map[string]any{"target": "/tmp/work"}})
		if seen["repo"] != "/tmp/work/repo" {
			t.Fatalf("task level repo = %v", seen["repo"])
		}
		if seen["script"] != "/tmp/work/repo/skills/hello/install.go" {
			t.Fatalf("step level script = %v", seen["script"])
		}
	})
}
