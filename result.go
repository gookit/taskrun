package taskrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// errf builds a plain error, used as the cause of a classified RunError.
func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }

// errDeferred marks a value that only exists at run time. Inspect reports it as
// deferred instead of executing anything.
var errDeferred = errors.New("taskrun: value requires execution")

// Status is the final status of a run, a task call or a step.
type Status string

const (
	// StatusSucceeded means every executed action completed without error.
	StatusSucceeded Status = "succeeded"
	// StatusSucceededWithWarnings means at least one error was tolerated by
	// ignore_error and the remaining steps completed.
	StatusSucceededWithWarnings Status = "succeeded_with_warnings"
	// StatusIgnoredFailure is a step-only status for a tolerated error.
	StatusIgnoredFailure Status = "ignored_failure"
	// StatusFailed means the run stopped on an error.
	StatusFailed Status = "failed"
	// StatusCanceled means the run was canceled through its context.
	StatusCanceled Status = "canceled"
	// StatusTimedOut means a task or step deadline expired.
	StatusTimedOut Status = "timed_out"
	// StatusSkipped means a task or step was skipped by a condition or platform rule.
	StatusSkipped Status = "skipped"
	// StatusDryRun is the status of a preview produced by Request.DryRun.
	StatusDryRun Status = "dry_run"
	// StatusDeferred is a plan-only status: the value depends on a dynamic
	// variable or other runtime data that Inspect must not evaluate.
	StatusDeferred Status = "deferred"
	// StatusPlanned is a plan-only status for an action that will run.
	StatusPlanned Status = "planned"
)

// ErrorKind classifies a RunError. Consumers should normally test with
// errors.Is against the package sentinel errors instead of comparing kinds.
type ErrorKind string

const (
	// ErrKindInvalidDefinition is a rejected definition or reference.
	ErrKindInvalidDefinition ErrorKind = "invalid_definition"
	// ErrKindInvalidRequest is a rejected Request.
	ErrKindInvalidRequest ErrorKind = "invalid_request"
	// ErrKindNotFound means the requested root task name does not exist.
	ErrKindNotFound ErrorKind = "not_found"
	// ErrKindLoad is a configuration load failure.
	ErrKindLoad ErrorKind = "load"
	// ErrKindStart means a program could not be started.
	ErrKindStart ErrorKind = "start"
	// ErrKindExit is a non-zero process exit.
	ErrKindExit ErrorKind = "exit"
	// ErrKindHandler is a host handler failure.
	ErrKindHandler ErrorKind = "handler"
	// ErrKindCanceled is a canceled run.
	ErrKindCanceled ErrorKind = "canceled"
	// ErrKindTimedOut is an expired task or step deadline.
	ErrKindTimedOut ErrorKind = "timed_out"
	// ErrKindOutputLimit means captured output exceeded its bound.
	ErrKindOutputLimit ErrorKind = "output_limit"
	// ErrKindExpansionLimit means the run expanded too many task calls.
	ErrKindExpansionLimit ErrorKind = "expansion_limit"
	// ErrKindIO is a stream or capture failure.
	ErrKindIO ErrorKind = "io"
)

var (
	// ErrNotFound reports that the requested root task name is unknown.
	// A missing dependency is invalid_definition, not ErrNotFound.
	ErrNotFound = errors.New("taskrun: task not found")
	// ErrInvalidRequest reports a rejected Request.
	ErrInvalidRequest = errors.New("taskrun: invalid request")
	// ErrInvalidDefinition reports a rejected definition or reference.
	ErrInvalidDefinition = errors.New("taskrun: invalid definition")
	// ErrDependencyCycle reports a dependency or task-call cycle.
	ErrDependencyCycle = errors.New("taskrun: task dependency cycle")
	// ErrExpansionLimit reports that the run exceeded its call budget.
	ErrExpansionLimit = errors.New("taskrun: task call expansion limit exceeded")
	// ErrStart reports that a program could not be started.
	ErrStart = errors.New("taskrun: program start failed")
	// ErrExit reports a non-zero process exit.
	ErrExit = errors.New("taskrun: non-zero exit")
	// ErrHandler reports a host handler failure.
	ErrHandler = errors.New("taskrun: handler failed")
	// ErrOutputLimit reports captured output above its bound.
	ErrOutputLimit = errors.New("taskrun: output limit exceeded")
	// ErrIO reports a stream or capture failure.
	ErrIO = errors.New("taskrun: io failure")
)

func kindMatches(kind ErrorKind, target error) bool {
	switch {
	case target == nil:
		return false
	case errors.Is(target, ErrNotFound):
		return kind == ErrKindNotFound
	case errors.Is(target, ErrInvalidRequest):
		return kind == ErrKindInvalidRequest
	case errors.Is(target, ErrInvalidDefinition):
		return kind == ErrKindInvalidDefinition
	case errors.Is(target, ErrStart):
		return kind == ErrKindStart
	case errors.Is(target, ErrExit):
		return kind == ErrKindExit
	case errors.Is(target, ErrHandler):
		return kind == ErrKindHandler
	case errors.Is(target, ErrOutputLimit):
		return kind == ErrKindOutputLimit
	case errors.Is(target, ErrExpansionLimit):
		return kind == ErrKindExpansionLimit
	case errors.Is(target, ErrIO):
		return kind == ErrKindIO
	case errors.Is(target, context.Canceled):
		return kind == ErrKindCanceled
	case errors.Is(target, context.DeadlineExceeded):
		return kind == ErrKindTimedOut
	}
	return false
}

// RunError is the classified error returned by Run and Inspect. It carries the
// task, call, step and source coordinates of the failure and preserves the
// underlying cause, including context.Canceled and context.DeadlineExceeded.
type RunError struct {
	Kind   ErrorKind
	Task   string
	CallID string
	Step   string
	Source string
	Err    error
}

func (e *RunError) Error() string {
	var b strings.Builder
	b.WriteString("taskrun: ")
	if e.Kind != "" {
		b.WriteString(string(e.Kind))
	} else {
		b.WriteString("error")
	}
	if e.Task != "" {
		b.WriteString(" task=")
		b.WriteString(e.Task)
	}
	if e.CallID != "" {
		b.WriteString(" call=")
		b.WriteString(e.CallID)
	}
	if e.Step != "" {
		b.WriteString(" step=")
		b.WriteString(e.Step)
	}
	if e.Source != "" {
		b.WriteString(" source=")
		b.WriteString(e.Source)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap exposes the underlying cause.
func (e *RunError) Unwrap() error { return e.Err }

// Is reports whether the error matches a package sentinel, a context cause or
// the classification of the underlying cause.
func (e *RunError) Is(target error) bool {
	if kindMatches(e.Kind, target) {
		return true
	}
	if e.Err != nil && errors.Is(e.Err, target) {
		return true
	}
	return false
}

// ProcessError is the classified error returned by the default process engine.
type ProcessError struct {
	Kind     ErrorKind
	ExitCode *int
	Started  bool
	Err      error
}

func (e *ProcessError) Error() string {
	if e.Err == nil {
		return "taskrun: " + string(e.Kind)
	}
	return "taskrun: " + string(e.Kind) + ": " + e.Err.Error()
}

// Unwrap exposes the underlying cause.
func (e *ProcessError) Unwrap() error { return e.Err }

// Is reports whether the error matches a package sentinel or context cause.
func (e *ProcessError) Is(target error) bool {
	if kindMatches(e.Kind, target) {
		return true
	}
	return e.Err != nil && errors.Is(e.Err, target)
}

// Result is a structured execution result. Status never contradicts the error
// returned by Run: a non-nil error means the status is Failed, Canceled or
// TimedOut.
type Result struct {
	Status    Status
	Task      string
	Tasks     []TaskResult
	Steps     []StepResult
	Plan      *Plan
	StartedAt time.Time
	EndedAt   time.Time
}

// TaskResult records one task call, including skipped calls and their reason.
type TaskResult struct {
	Name      string
	CallID    string
	Status    Status
	Reason    string
	Err       error
	StartedAt time.Time
	EndedAt   time.Time
}

// StepResult records one step outcome.
type StepResult struct {
	Name        string
	CallID      string
	Kind        string
	Status      Status
	Started     bool
	ExitCode    *int
	Output      []byte
	ErrorOutput []byte
	Truncated   bool
	Err         error
	StartedAt   time.Time
	EndedAt     time.Time
}

// Plan is a side-effect-free execution preview produced by Inspect.
type Plan struct {
	Task     string
	Actions  []PlannedAction
	Deferred []string
	Skipped  []string
}

// PlannedAction describes an action in execution order without running it.
type PlannedAction struct {
	Task    string
	CallID  string
	Step    string
	Kind    string
	Program string
	Args    []string
	Script  string
	Dir     string
	EnvKeys []string
	Status  Status
	Reason  string
}

// ActionResult is returned by an Engine or Handler.
type ActionResult struct {
	Started     bool
	ExitCode    *int
	Output      []byte
	ErrorOutput []byte
	Truncated   bool
}

// exitCodePtr returns a pointer to a copy of code, for use in results.
func exitCodePtr(code int) *int {
	c := code
	return &c
}

var _ = fmt.Sprintf
