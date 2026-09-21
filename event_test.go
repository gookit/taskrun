package taskrun

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
)

// recorder collects observer events in arrival order.
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) observe(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recorder) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventKind, 0, len(r.events))
	for _, event := range r.events {
		out = append(out, event.Kind)
	}
	return out
}

func (r *recorder) find(kind EventKind, task, step string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.Kind == kind && event.Task == task && event.Step == step {
			return event, true
		}
	}
	return Event{}, false
}

func equalKinds(got []EventKind, want ...EventKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestObserverReceivesOrderedEvents(t *testing.T) {
	rec := &recorder{}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Deps: []string{"dep"}, Steps: []Step{
				{Name: "one", Exec: &ExecSpec{Program: "go", Args: []string{"version"}}},
				{Name: "two", Task: &TaskCall{Name: "child"}},
			}},
			"dep":   {Steps: []Step{{Name: "dep-step", Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}},
			"child": {Steps: []Step{{Name: "sleep", Exec: sleepExec(0)}}},
		},
	}, WithObserver(rec.observe))
	mustRun(t, runner, Request{Task: "root", IO: IO{CaptureLimit: 64}})

	got := rec.kinds()
	// A task reports started before its dependencies run, and a task call step
	// finishes after the called task finished.
	want := []EventKind{
		EventRunStarted,
		EventTaskStarted,                                                         // root
		EventTaskStarted, EventStepStarted, EventStepFinished, EventTaskFinished, // dep
		EventStepStarted, EventStepFinished, // root step one
		EventStepStarted,                                                         // root step two (task call)
		EventTaskStarted, EventStepStarted, EventStepFinished, EventTaskFinished, // child
		EventStepFinished, EventTaskFinished, // root step two, then root
		EventRunFinished,
	}
	if !equalKinds(got, want...) {
		t.Fatalf("event order:\ngot:  %v\nwant: %v", got, want)
	}

	// Depth distinguishes the root call from its dependency and child.
	if event, ok := rec.find(EventTaskStarted, "root", ""); !ok || event.Depth != 1 {
		t.Fatalf("root started event=%+v ok=%v", event, ok)
	}
	if event, ok := rec.find(EventTaskStarted, "child", ""); !ok || event.Depth != 2 {
		t.Fatalf("child started event=%+v ok=%v", event, ok)
	}
	// Step events carry the action kind and the run status does too.
	if event, ok := rec.find(EventStepFinished, "root", "one"); !ok || event.ActionKind != "exec" || event.Status != StatusSucceeded {
		t.Fatalf("step event=%+v ok=%v", event, ok)
	}
	if event, ok := rec.find(EventStepFinished, "root", "two"); !ok || event.ActionKind != "task" {
		t.Fatalf("task call step event=%+v ok=%v", event, ok)
	}
	if event, ok := rec.find(EventRunFinished, "root", ""); !ok || event.Status != StatusSucceeded {
		t.Fatalf("run finished event=%+v ok=%v", event, ok)
	}
}

func TestObserverReportsSkipsAndIgnoredFailures(t *testing.T) {
	shell := requireShell(t, "sh")
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "windows"
	}
	rec := &recorder{}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"root": {Steps: []Step{
				{Name: "off", Platform: []string{other}, Exec: &ExecSpec{Program: "go"}},
				{Name: "condition", If: "false", Exec: &ExecSpec{Program: "go"}},
				{Name: "ignored", IgnoreError: true, Shell: &ShellSpec{Name: shell, Script: "exit 4"}},
			}},
			"skipped-task": {If: "false", Steps: []Step{{Exec: &ExecSpec{Program: "go"}}}},
			"caller":       {Steps: []Step{{Task: &TaskCall{Name: "skipped-task"}}}},
		},
	}, WithObserver(rec.observe))
	result := mustRun(t, runner, Request{Task: "root", IO: IO{CaptureLimit: 64}})
	if result.Status != StatusSucceededWithWarnings {
		t.Fatalf("status=%s", result.Status)
	}
	platformEvent, ok := rec.find(EventStepSkipped, "root", "off")
	if !ok || platformEvent.Reason == "" || platformEvent.Status != StatusSkipped {
		t.Fatalf("platform skip event=%+v ok=%v", platformEvent, ok)
	}
	conditionEvent, ok := rec.find(EventStepSkipped, "root", "condition")
	if !ok || conditionEvent.Reason != "condition false" {
		t.Fatalf("condition skip event=%+v ok=%v", conditionEvent, ok)
	}
	ignoredEvent, ok := rec.find(EventStepFinished, "root", "ignored")
	if !ok || ignoredEvent.Status != StatusIgnoredFailure || ignoredEvent.Err == nil {
		t.Fatalf("ignored failure event=%+v ok=%v", ignoredEvent, ok)
	}
	if event, ok := rec.find(EventRunFinished, "root", ""); !ok || event.Status != StatusSucceededWithWarnings {
		t.Fatalf("run finished event=%+v ok=%v", event, ok)
	}

	// A skipped task reports its reason and no started event.
	rec2 := &recorder{}
	caller := newTestRunner(t, Definition{
		Tasks: map[string]Task{
			"caller":       {Steps: []Step{{Task: &TaskCall{Name: "skipped-task"}}}},
			"skipped-task": {If: "false", Steps: []Step{{Exec: &ExecSpec{Program: "go"}}}},
		},
	}, WithObserver(rec2.observe))
	mustRun(t, caller, Request{Task: "caller"})
	skippedTask, ok := rec2.find(EventTaskSkipped, "skipped-task", "")
	if !ok || skippedTask.Reason != "condition false" {
		t.Fatalf("skipped task event=%+v ok=%v", skippedTask, ok)
	}
	if _, started := rec2.find(EventTaskStarted, "skipped-task", ""); started {
		t.Fatal("a skipped task must not report a started event")
	}
}

func TestObserverReportsFailures(t *testing.T) {
	rec := &recorder{}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{
			{Name: "ok", Exec: &ExecSpec{Program: "go", Args: []string{"version"}}},
			{Name: "bad", Exec: &ExecSpec{Program: "definitely-not-a-program"}},
		}}},
	}, WithObserver(rec.observe))
	if _, err := runner.Run(context.Background(), Request{Task: "t", IO: IO{CaptureLimit: 64}}); err == nil {
		t.Fatal("expected a failure")
	}
	stepEvent, ok := rec.find(EventStepFinished, "t", "bad")
	if !ok || stepEvent.Status != StatusFailed || stepEvent.Err == nil {
		t.Fatalf("failed step event=%+v ok=%v", stepEvent, ok)
	}
	taskEvent, ok := rec.find(EventTaskFinished, "t", "")
	if !ok || taskEvent.Status != StatusFailed || taskEvent.Err == nil {
		t.Fatalf("failed task event=%+v ok=%v", taskEvent, ok)
	}
	runEvent, ok := rec.find(EventRunFinished, "t", "")
	if !ok || runEvent.Status != StatusFailed {
		t.Fatalf("failed run event=%+v ok=%v", runEvent, ok)
	}
}

func TestInspectAndDryRunEmitNoEvents(t *testing.T) {
	rec := &recorder{}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}}},
	}, WithObserver(rec.observe))
	if _, err := runner.Inspect(context.Background(), Request{Task: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), Request{Task: "t", DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if events := rec.kinds(); len(events) != 0 {
		t.Fatalf("inspect and dry run produced events: %v", events)
	}
}

func TestObserverReceivesEventsFromConcurrentRuns(t *testing.T) {
	rec := &recorder{}
	shell := requireShell(t, "sh")
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: shell, Script: "printf ok"}}}}},
	}, WithObserver(rec.observe))

	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := runner.Run(context.Background(), Request{Task: "t", IO: IO{CaptureLimit: 64}})
			done <- err
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-done; err != nil {
			t.Fatalf("run failed: %v", err)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	starts, finishes := 0, 0
	for _, event := range rec.events {
		switch event.Kind {
		case EventRunStarted:
			starts++
		case EventRunFinished:
			finishes++
		}
	}
	if starts != 4 || finishes != 4 {
		t.Fatalf("starts=%d finishes=%d events=%d", starts, finishes, len(rec.events))
	}
}

func TestWithObserverRejectsNil(t *testing.T) {
	if _, err := New(Definition{BaseDir: t.TempDir()}, WithObserver(nil)); err == nil {
		t.Fatal("expected an error for a nil observer")
	}
}

func TestObserverDefaultsToNothing(t *testing.T) {
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "go", Args: []string{"version"}}}}}},
	})
	if _, err := runner.Run(context.Background(), Request{Task: "t", IO: IO{CaptureLimit: 64}}); err != nil {
		t.Fatal(err)
	}
}

// TestObserverErrorIdentity checks the observer sees the same classified error
// the caller gets.
func TestObserverErrorIdentity(t *testing.T) {
	rec := &recorder{}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{Program: "definitely-not-a-program"}}}}},
	}, WithObserver(rec.observe))
	_, err := runner.Run(context.Background(), Request{Task: "t"})
	if err == nil {
		t.Fatal("expected an error")
	}
	event, ok := rec.find(EventRunFinished, "t", "")
	if !ok || event.Err == nil {
		t.Fatalf("run finished event=%+v ok=%v", event, ok)
	}
	if !errors.Is(event.Err, ErrStart) {
		t.Fatalf("observer error is not classified: %v", event.Err)
	}
}
