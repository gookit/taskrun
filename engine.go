package taskrun

import "context"

// Engine executes a prepared external action. It must honor ctx cancellation,
// must not schedule tasks or call handlers, and must return a ProcessError (or
// an error wrapping a context cause) so the runner can classify the failure.
type Engine interface {
	Execute(ctx context.Context, action PreparedAction, streams IO) (ActionResult, error)
}

// Handler executes an explicitly registered host action. Handlers are
// registered before New and cannot be replaced afterwards.
type Handler func(context.Context, HostCall) (ActionResult, error)

// PreparedAction is an action that has been validated and rendered. It never
// represents a task call or a host action, so an Engine can never re-enter the
// scheduler.
type PreparedAction struct {
	// Kind is "exec", "shell" or "file".
	Kind string
	// Program is the executable for exec and file, and the requested shell name
	// for shell ("sh", "bash", "zsh", "cmd", "pwsh" or "powershell").
	Program string
	// Args is the argv for exec and file. Exec arguments keep their boundaries:
	// the engine must not re-split them.
	Args []string
	// Script is the complete source for shell actions.
	Script string
	// Dir is the absolute working directory, empty when BaseDir is used.
	Dir string
	// Env is the complete environment for the child process. It is never nil.
	Env []string
}
