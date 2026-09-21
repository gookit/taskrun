package taskrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEngineCapturesAndForwardsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	var forwarded strings.Builder
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf abcdef"}}}}},
	})
	result := mustRun(t, r, Request{Task: "t", IO: IO{Stdout: &forwarded, CaptureLimit: 3}})
	if forwarded.String() != "abcdef" {
		t.Fatalf("forwarded=%q", forwarded.String())
	}
	if string(result.Steps[0].Output) != "abc" || !result.Steps[0].Truncated {
		t.Fatalf("step=%+v", result.Steps[0])
	}
}

func TestEngineCopiesFiveHundredKilobytesBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	var forwarded strings.Builder
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "yes 0123456789 | head -c 500000"}}}}},
	})
	start := time.Now()
	result := mustRun(t, r, Request{Task: "t", IO: IO{Stdout: &forwarded, CaptureLimit: 128}})
	if len(result.Steps[0].Output) != 128 || !result.Steps[0].Truncated {
		t.Fatalf("captured=%d truncated=%v", len(result.Steps[0].Output), result.Steps[0].Truncated)
	}
	if forwarded.Len() != 500000 {
		t.Fatalf("forwarded=%d", forwarded.Len())
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("draining took too long: %v", elapsed)
	}
}

func TestEngineSeparatesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf out; printf err >&2"}}}}},
	})
	result := mustRun(t, r, Request{Task: "t", IO: IO{CaptureLimit: 64}})
	if string(result.Steps[0].Output) != "out" || string(result.Steps[0].ErrorOutput) != "err" {
		t.Fatalf("step=%+v", result.Steps[0])
	}
}

type failingWriter struct{ limit int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return 0, errors.New("writer closed")
	}
	w.limit--
	return len(p), nil
}

func TestWriterErrorTerminatesAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "i=0; while [ $i -lt 200 ]; do printf 'line line line line\n'; i=$((i+1)); done"}}}}},
	})
	start := time.Now()
	_, err := r.Run(context.Background(), Request{Task: "t", IO: IO{Stdout: &failingWriter{limit: 1}}})
	if err == nil || !errors.Is(err, ErrIO) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("action did not terminate: %v", elapsed)
	}
}

func TestTimeoutClassifiedAsTimedOut(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Timeout: 300 * time.Millisecond, Steps: []Step{{Exec: sleepExec(5)}}}},
	})
	start := time.Now()
	result, err := r.Run(context.Background(), Request{Task: "t"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cause not preserved: %v", err)
	}
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Kind != ErrKindTimedOut {
		t.Fatalf("err=%#v", err)
	}
	if result.Status != StatusTimedOut {
		t.Fatalf("status=%s", result.Status)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("timeout not enforced: %v", elapsed)
	}
}

func TestCancelClassifiedAsCanceled(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: sleepExec(5)}}}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	result, err := r.Run(ctx, Request{Task: "t"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if result.Status != StatusCanceled {
		t.Fatalf("status=%s", result.Status)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("cancel not enforced: %v", elapsed)
	}
}

func TestCancelStopsSchedulingNewSteps(t *testing.T) {
	var second int
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{
			{Exec: sleepExec(5)},
			{Host: &HostSpec{Name: "second"}},
		}}},
	}, WithHandler("second", func(_ context.Context, _ HostCall) (ActionResult, error) {
		second++
		return ActionResult{}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	if _, err := r.Run(ctx, Request{Task: "t"}); err == nil {
		t.Fatal("expected cancellation")
	}
	if second != 0 {
		t.Fatalf("a step ran after cancelation")
	}
}

func TestStepTimeoutIsEarlierThanTaskTimeout(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Timeout: 30 * time.Second,
			Steps:   []Step{{Timeout: 300 * time.Millisecond, Exec: sleepExec(5)}},
		}},
	})
	start := time.Now()
	if _, err := r.Run(context.Background(), Request{Task: "t"}); err == nil {
		t.Fatal("expected step timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("step timeout not enforced: %v", elapsed)
	}
}

func TestZeroTimeoutInheritsParentBudget(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Timeout: 0, Exec: sleepExec(0)}}}},
	})
	if result := mustRun(t, r, Request{Task: "t"}); result.Status != StatusSucceeded {
		t.Fatalf("status=%s", result.Status)
	}
}

// TestProcessTreeIsKilled verifies that a canceled action does not leave an
// owned descendant running: the grandchild would create the marker file only
// after 2 seconds, which must never happen once the tree is killed.
func TestProcessTreeIsKilled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	script := treeScript(marker)
	if script == "" {
		t.Skip("no shell fixture for this platform")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: shellForTest(), Script: script}}}}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	if _, err := r.Run(ctx, Request{Task: "t"}); err == nil {
		t.Fatal("expected cancellation")
	}
	time.Sleep(2500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("descendant process survived cancellation and wrote %s", marker)
	}
}

// TestGracefulExitIsPreferred verifies the grace period: a process tree that
// exits on its own after the first signal is not force killed, and the run
// still reports cancellation.
func TestGracefulExitIsPreferred(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("graceful signal fixture is unix specific")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{
			IgnoreError: false,
			Shell:       &ShellSpec{Name: "sh", Script: "trap 'exit 0' TERM; sleep 5 & wait"},
		}}}},
	}, WithKillGrace(time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	result, err := r.Run(ctx, Request{Task: "t"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if result.Status != StatusCanceled {
		t.Fatalf("status=%s", result.Status)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("graceful exit was not used: %v", elapsed)
	}
}

func TestProgramResolutionUsesEffectivePath(t *testing.T) {
	dir := t.TempDir()
	name := "ksprobe"
	target := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		target += ".cmd"
		if err := os.WriteFile(target, []byte("@echo off\r\necho from-temp-path\r\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(target, []byte("#!/bin/sh\necho from-temp-path\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"PATH": dir, "PATHEXT": ".COM;.EXE;.BAT;.CMD"}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: name}}}}},
	}, WithBaseEnv(env))
	result := mustRun(t, r, Request{Task: "t", IO: IO{CaptureLimit: 64}})
	if got := strings.TrimSpace(string(result.Steps[0].Output)); got != "from-temp-path" {
		t.Fatalf("output=%q", got)
	}
}

func TestMissingProgramIsStartError(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "definitely-not-a-program"}}}}},
	})
	result, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, ErrStart) {
		t.Fatalf("err=%v", err)
	}
	if result.Steps[0].Started || result.Steps[0].ExitCode != nil {
		t.Fatalf("step should not report a start or exit code: %+v", result.Steps[0])
	}
}

func TestShellSelectionIsExplicit(t *testing.T) {
	if runtime.GOOS == "windows" {
		r := newTestRunner(t, Definition{
			Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "cmd", Script: "echo hi"}}}}},
		})
		result := mustRun(t, r, Request{Task: "t", IO: IO{CaptureLimit: 64}})
		if !strings.Contains(string(result.Steps[0].Output), "hi") {
			t.Fatalf("output=%q", result.Steps[0].Output)
		}
		return
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf hi"}}}}},
	})
	result := mustRun(t, r, Request{Task: "t", IO: IO{CaptureLimit: 64}})
	if string(result.Steps[0].Output) != "hi" {
		t.Fatalf("output=%q", result.Steps[0].Output)
	}
	// The requested shell is used rather than a platform default.
	r2 := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: "bash", Script: "printf bash-ok"}}}}},
	})
	result2 := mustRun(t, r2, Request{Task: "t", IO: IO{CaptureLimit: 64}})
	if string(result2.Steps[0].Output) != "bash-ok" {
		t.Fatalf("bash output=%q", result2.Steps[0].Output)
	}
}

func TestShellScriptIsRendered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Vars:  map[string]any{"target": "rendered"},
			Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "printf %s \"${vars.target}\""}}},
		}},
	})
	result := mustRun(t, r, Request{Task: "t", IO: IO{CaptureLimit: 64}})
	if string(result.Steps[0].Output) != "rendered" {
		t.Fatalf("output=%q", result.Steps[0].Output)
	}
}

func TestNoGlobalProcessStateLeak(t *testing.T) {
	before := os.Getenv("KS_LEAK")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell fixture")
	}
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Env:   map[string]string{"KS_LEAK": "1"},
			Dir:   dir,
			Steps: []Step{{Shell: &ShellSpec{Name: "sh", Script: "pwd"}}},
		}},
	})
	result := mustRun(t, r, Request{Task: "t", IO: IO{CaptureLimit: 128}})
	if after := os.Getenv("KS_LEAK"); after != before {
		t.Fatalf("process env changed: %q -> %q", before, after)
	}
	now, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if now != wd {
		t.Fatalf("process cwd changed: %s -> %s", wd, now)
	}
	if !strings.Contains(strings.ToLower(string(result.Steps[0].Output)), strings.ToLower(filepath.Base(dir))) {
		t.Fatalf("step ran in %q, want %q", result.Steps[0].Output, dir)
	}
}

func sleepExec(seconds int) *ExecSpec {
	if runtime.GOOS == "windows" {
		return &ExecSpec{Program: "ping", Args: []string{"-n", strconv.Itoa(seconds + 1), "127.0.0.1"}}
	}
	return &ExecSpec{Program: "sleep", Args: []string{strconv.Itoa(seconds)}}
}

func shellForTest() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

// treeScript returns a script whose background descendant would create marker
// after two seconds while the foreground process keeps the action alive.
func treeScript(marker string) string {
	if runtime.GOOS == "windows" {
		return `start /b cmd /c "ping -n 3 127.0.0.1 >NUL & echo x > "` + marker + `"" & ping -n 8 127.0.0.1 >NUL`
	}
	return `( sleep 2 && touch '` + marker + `' ) & sleep 8`
}
