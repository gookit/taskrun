package kscript

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ignore_error tolerates a non-zero exit or a handler business error only. It
// must never absorb cancellation, a timeout or a start failure.
func TestIgnoreErrorDoesNotAbsorbCancel(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{IgnoreError: true, Exec: sleepExec(5)}}}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	result, err := r.Run(ctx, Request{Task: "t"})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if result.Status != StatusCanceled {
		t.Fatalf("status=%s", result.Status)
	}
}

func TestIgnoreErrorDoesNotAbsorbTimeout(t *testing.T) {
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Timeout: 300 * time.Millisecond, Steps: []Step{{IgnoreError: true, Exec: sleepExec(5)}}}},
	})
	result, err := r.Run(context.Background(), Request{Task: "t"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if result.Status != StatusTimedOut {
		t.Fatalf("status=%s", result.Status)
	}
}

func TestCancellationBlocksParent(t *testing.T) {
	var after int
	r := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Steps: []Step{
				{Task: &TaskCall{Name: "slow"}},
				{Host: &HostSpec{Name: "after"}},
			}},
			"slow": {Steps: []Step{{Exec: sleepExec(5)}}},
		},
	}, WithHandler("after", func(_ context.Context, _ HostCall) (ActionResult, error) {
		after++
		return ActionResult{}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	if _, err := r.Run(ctx, Request{Task: "root"}); err == nil {
		t.Fatal("expected cancellation")
	}
	if after != 0 {
		t.Fatal("parent continued after a dependency was canceled")
	}
}
