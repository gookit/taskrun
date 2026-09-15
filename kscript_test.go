package kscript

import (
	"bytes"
	"context"
	"runtime"
	"testing"
)

func TestConditionSkipAndRun(t *testing.T) {
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{
		"skip": {Name: "skip", If: "enabled", Steps: []Step{{Exec: &ExecSpec{Program: "definitely-not-run"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(context.Background(), Request{Task: "skip", Vars: map[string]any{"enabled": false}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped {
		t.Fatalf("status=%s", result.Status)
	}
}

func TestShellAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture differs on Windows")
	}
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{"s": {Name: "s", Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf hi"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	res, err := r.Run(context.Background(), Request{Task: "s", IO: IO{Stdout: &out}})
	if err != nil || res.Status != StatusSucceeded || out.String() != "hi" {
		t.Fatalf("result=%v err=%v out=%q", res, err, out.String())
	}
}

func TestCaptureLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture differs on Windows")
	}
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{"s": {Name: "s", Steps: []Step{{Exec: &ExecSpec{Program: "printf", Args: []string{"abcdef"}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), Request{Task: "s", IO: IO{CaptureLimit: 3}})
	if err != nil || len(res.Steps) != 1 || string(res.Steps[0].Output) != "abc" || !res.Steps[0].Truncated {
		t.Fatalf("result=%+v err=%v", res, err)
	}
}

func TestConditionMustBeBool(t *testing.T) {
	_, err := evalCondition("name", map[string]any{"name": "text"})
	if err == nil {
		t.Fatal("expected bool error")
	}
}

func TestDependencyCycle(t *testing.T) {
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{
		"a": {Name: "a", Deps: []string{"b"}, Steps: []Step{{Exec: &ExecSpec{Program: "echo"}}}},
		"b": {Name: "b", Deps: []string{"a"}, Steps: []Step{{Exec: &ExecSpec{Program: "echo"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Run(context.Background(), Request{Task: "a"}); err == nil {
		t.Fatal("expected dependency cycle")
	}
}

func TestInspectExpandsActions(t *testing.T) {
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{"a": {Name: "a", Deps: []string{"b"}, Steps: []Step{{Exec: &ExecSpec{Program: "echo", Args: []string{"ok"}}}}}, "b": {Name: "b", Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "true"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.Inspect(context.Background(), Request{Task: "a"})
	if err != nil || len(p.Actions) != 2 {
		t.Fatalf("plan=%+v err=%v", p, err)
	}
}

func TestIgnoreError(t *testing.T) {
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{"x": {Name: "x", Steps: []Step{{IgnoreError: true, Exec: &ExecSpec{Program: "definitely-not-found"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), Request{Task: "x"})
	if err != nil || res.Status != StatusSucceededWithWarnings {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestCanceledRunReturnsResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{"x": {Name: "x", Steps: []Step{{Exec: &ExecSpec{Program: "echo"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(ctx, Request{Task: "x"})
	if err == nil || res == nil || res.Status != StatusCanceled {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
