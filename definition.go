package kscript

import (
	"context"
	"io"
	"time"
)

// Definition is an immutable task and script definition source.
type Definition struct {
	Version int
	BaseDir string
	Vars    map[string]any
	Tasks   map[string]Task
	Files   map[string]ScriptFile
}

// Task describes one executable task.
type Task struct {
	Name     string
	Desc     string
	Deps     []string
	Steps    []Step
	Vars     map[string]any
	Env      map[string]string
	Dir      string
	If       string
	Platform []string
	Timeout  timeDuration
}

// Step is exactly one executable action.
type Step struct {
	Name        string
	Exec        *ExecSpec
	Shell       *ShellSpec
	File        *FileSpec
	Task        *TaskCall
	Host        *HostSpec
	Vars        map[string]any
	Env         map[string]string
	Dir         string
	If          string
	IgnoreError bool
	Timeout     timeDuration
}

// ExecSpec describes argv execution without shell parsing.
type ExecSpec struct {
	Program string
	Args    []string
}

// ShellSpec describes explicit shell execution.
type ShellSpec struct {
	Name   string
	Script string
}

// FileSpec describes a registered script and interpreter.
type FileSpec struct {
	Path        string
	Interpreter Interpreter
}

// Interpreter identifies a script interpreter and prefix arguments.
type Interpreter struct {
	Program    string
	PrefixArgs []string
}

// TaskCall invokes another task.
type TaskCall struct {
	Name        string
	Args        []string
	ForwardArgs bool
}

// HostSpec invokes a handler registered by the embedding application.
type HostSpec struct {
	Name string
	Args []any
}

// ScriptFile is a named executable script.
type ScriptFile struct {
	Name        string
	Path        string
	Interpreter Interpreter
	Env         map[string]string
	Dir         string
}

// timeDuration keeps the public model independent of a config duration parser.
// It accepts time.Duration values through the Option/Go API conversion helpers.
type timeDuration int64

// Request describes one isolated run.
type Request struct {
	Task     string
	Args     []string
	Vars     map[string]any
	Env      map[string]string
	HostData map[string]any
	Dir      string
	DryRun   bool
	IO       IO
}

// IO controls process streams and bounded capture.
type IO struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	CaptureLimit int64
}

// Reader and Writer avoid forcing a CLI or logging dependency into the model.

// Status is the final status of a run or action.
type Status string

const (
	StatusSucceeded             Status = "succeeded"
	StatusSucceededWithWarnings Status = "succeeded_with_warnings"
	StatusFailed                Status = "failed"
	StatusCanceled              Status = "canceled"
	StatusTimedOut              Status = "timed_out"
	StatusSkipped               Status = "skipped"
	StatusDryRun                Status = "dry_run"
)

// Result is a structured execution result.
type Result struct {
	Status Status
	Task   string
	Steps  []StepResult
	Plan   *Plan
}

// StepResult records one step outcome.
type StepResult struct {
	Name      string
	Status    Status
	ExitCode  *int
	Output    []byte
	Truncated bool
	Err       error
}

// Plan is a side-effect-free execution preview.
type Plan struct {
	Task     string
	Actions  []PlannedAction
	Deferred []string
}

// PlannedAction describes a planned action without executing it.
type PlannedAction struct {
	Task    string
	Step    string
	Kind    string
	Program string
	Args    []string
	Dir     string
}

// ActionResult is returned by an Engine or Handler.
type ActionResult struct {
	ExitCode  *int
	Output    []byte
	Truncated bool
}

// HostCall is the isolated input to a Handler.
type HostCall struct {
	Name string
	Args []any
	Vars map[string]any
	Env  map[string]string
	Dir  string
}

// Handler executes an explicitly registered host action.
type Handler func(context.Context, HostCall) (ActionResult, error)

// Engine executes prepared external actions.
type Engine interface {
	Execute(context.Context, PreparedAction, IO) (ActionResult, error)
}

// PreparedAction is validated and rendered before execution.
type PreparedAction struct {
	Kind    string
	Program string
	Args    []string
	Script  string
	Dir     string
	Env     map[string]string
}

// Option configures a Runner.
type Option func(*runnerConfig) error
type runnerConfig struct {
	engine   Engine
	handlers map[string]Handler
}

// WithEngine sets the external action engine.
func WithEngine(e Engine) Option { return func(c *runnerConfig) error { c.engine = e; return nil } }

// WithHandler registers a host action before Runner creation.
func WithHandler(name string, h Handler) Option {
	return func(c *runnerConfig) error {
		if c.handlers == nil {
			c.handlers = map[string]Handler{}
		}
		c.handlers[name] = h
		return nil
	}
}
