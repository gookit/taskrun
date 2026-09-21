package taskrun

import "time"

// EventKind identifies one observable execution event.
type EventKind string

const (
	// EventRunStarted is emitted once a run has a validated root task.
	EventRunStarted EventKind = "run_started"
	// EventRunFinished is emitted once the final status is known.
	EventRunFinished EventKind = "run_finished"
	// EventTaskStarted is emitted before a task call evaluates its steps.
	EventTaskStarted EventKind = "task_started"
	// EventTaskSkipped is emitted when a call is skipped by platform or condition.
	EventTaskSkipped EventKind = "task_skipped"
	// EventTaskFinished is emitted after a task call completes or fails.
	EventTaskFinished EventKind = "task_finished"
	// EventStepStarted is emitted before a step action runs.
	EventStepStarted EventKind = "step_started"
	// EventStepSkipped is emitted when a step is skipped by platform or condition.
	EventStepSkipped EventKind = "step_skipped"
	// EventStepFinished is emitted after a step completes, fails or is ignored.
	EventStepFinished EventKind = "step_finished"
)

// Event is a structured execution event for host logging and progress display.
// Events never carry captured output; use Result for that. A successful run
// reports the final status through EventRunFinished.
type Event struct {
	Kind   EventKind
	Task   string
	CallID string
	Step   string
	// ActionKind is "exec", "shell", "file", "task" or "host" for step events.
	ActionKind string
	// Depth is the task call depth, starting at 1 for the root task.
	Depth  int
	Status Status
	// Reason explains a skipped task or step.
	Reason string
	// Dir is the effective working directory of the run, task or step.
	Dir string
	// ExitCode is set for a started process.
	ExitCode *int
	// Err carries the classified failure when one occurred.
	Err  error
	Time time.Time
}

// Observer receives execution events. Observers cannot change scheduling and do
// not return errors. Callbacks of one run arrive in order from the goroutine
// running that run; different runs may call an observer concurrently, so the
// host must synchronize and return quickly.
type Observer func(Event)

// emit delivers one event. Inspect and DryRun produce no events because no
// action runs.
func (s *runState) emit(event Event) {
	if s.r.cfg.observer == nil || s.mode == modeInspect {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	s.r.cfg.observer(event)
}
