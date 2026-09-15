package kscript

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/expr-lang/expr"
)

var ErrNotFound = errors.New("task not found")
var ErrInvalidDefinition = errors.New("invalid kscript definition")
var ErrDependencyCycle = errors.New("task dependency cycle")

type Runner struct {
	def Definition
	cfg runnerConfig
}

func New(def Definition, opts ...Option) (*Runner, error) {
	if def.Version == 0 {
		def.Version = 1
	}
	if def.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidDefinition, def.Version)
	}
	if def.BaseDir == "" {
		return nil, fmt.Errorf("%w: BaseDir is required", ErrInvalidDefinition)
	}
	c := runnerConfig{handlers: map[string]Handler{}}
	for _, o := range opts {
		if err := o(&c); err != nil {
			return nil, err
		}
	}
	if c.engine == nil {
		c.engine = ProcessEngine{}
	}
	if err := validateDefinition(def, c); err != nil {
		return nil, err
	}
	return &Runner{def: cloneDefinition(def), cfg: c}, nil
}
func (r *Runner) Lookup(name string) (Task, error) {
	t, ok := r.def.Tasks[name]
	if !ok {
		return Task{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return cloneTask(t), nil
}
func (r *Runner) List() []Task {
	out := make([]Task, 0, len(r.def.Tasks))
	for _, t := range r.def.Tasks {
		out = append(out, cloneTask(t))
	}
	return out
}
func (r *Runner) Inspect(ctx context.Context, req Request) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	t, err := r.Lookup(req.Task)
	if err != nil {
		return Plan{}, err
	}
	if t.If != "" {
		return Plan{Task: req.Task, Deferred: []string{"task condition: " + t.If}}, nil
	}
	return Plan{Task: req.Task}, nil
}
func (r *Runner) Run(ctx context.Context, req Request) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, err := r.Lookup(req.Task)
	if err != nil {
		return nil, err
	}
	if req.DryRun {
		p := Plan{Task: t.Name}
		return &Result{Status: StatusDryRun, Task: t.Name, Plan: &p}, nil
	}
	if ok, err := evalCondition(t.If, req.Vars); err != nil {
		return nil, err
	} else if !ok {
		return &Result{Status: StatusSkipped, Task: t.Name}, nil
	}
	if len(t.Steps) == 0 && len(t.Deps) == 0 {
		return nil, fmt.Errorf("%w: empty task %s", ErrInvalidDefinition, t.Name)
	}
	result := &Result{Status: StatusSucceeded, Task: t.Name}
	if err := r.runTask(ctx, t, req, result, map[string]bool{}); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runner) runTask(ctx context.Context, t Task, req Request, result *Result, stack map[string]bool) error {
	if t.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t.Timeout))
		defer cancel()
	}
	if stack[t.Name] {
		return fmt.Errorf("%w: %s", ErrDependencyCycle, t.Name)
	}
	stack[t.Name] = true
	defer delete(stack, t.Name)
	if ok, err := evalCondition(t.If, req.Vars); err != nil {
		return err
	} else if !ok {
		return nil
	}
	for _, dep := range t.Deps {
		dt, err := r.Lookup(dep)
		if err != nil {
			return err
		}
		if err := r.runTask(ctx, dt, req, result, stack); err != nil {
			return err
		}
	}
	for _, step := range t.Steps {
		stepCtx := ctx
		if step.Timeout > 0 {
			var cancel context.CancelFunc
			stepCtx, cancel = context.WithTimeout(ctx, time.Duration(step.Timeout))
			defer cancel()
		}
		if step.If != "" {
			ok, err := evalCondition(step.If, req.Vars)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		if step.Task != nil {
			ct, err := r.Lookup(step.Task.Name)
			if err != nil {
				return err
			}
			if err := r.runTask(ctx, ct, req, result, stack); err != nil {
				return err
			}
		} else if step.Host != nil {
			h := r.cfg.handlers[step.Host.Name]
			ar, err := h(stepCtx, HostCall{Name: step.Host.Name, Args: step.Host.Args, Vars: req.Vars, Env: req.Env, Dir: req.Dir})
			if err != nil {
				return err
			}
			result.Steps = append(result.Steps, StepResult{Name: step.Name, Status: StatusSucceeded, ExitCode: ar.ExitCode, Output: ar.Output})
		} else {
			action := PreparedAction{Dir: req.Dir, Env: req.Env}
			if step.Exec != nil {
				action.Kind = "exec"
				action.Program = step.Exec.Program
				action.Args = step.Exec.Args
			}
			if step.Shell != nil {
				action.Kind = "shell"
				action.Program = step.Shell.Name
				action.Script = step.Shell.Script
			}
			if step.File != nil {
				action.Kind = "file"
				action.Program = step.File.Interpreter.Program
				action.Args = append(append([]string{}, step.File.Interpreter.PrefixArgs...), step.File.Path)
			}
			if action.Program == "" {
				return fmt.Errorf("%w: step %s has no action", ErrInvalidDefinition, step.Name)
			}
			ar, err := r.cfg.engine.Execute(stepCtx, action, req.IO)
			if err != nil {
				return err
			}
			result.Steps = append(result.Steps, StepResult{Name: step.Name, Status: StatusSucceeded, ExitCode: ar.ExitCode, Output: ar.Output})
		}
	}
	return nil
}

func evalCondition(source string, vars map[string]any) (bool, error) {
	if source == "" {
		return true, nil
	}
	program, err := expr.Compile(source, expr.Env(vars))
	if err != nil {
		return false, fmt.Errorf("invalid condition: %w", err)
	}
	value, err := expr.Run(program, vars)
	if err != nil {
		return false, fmt.Errorf("condition evaluation failed: %w", err)
	}
	ok, isBool := value.(bool)
	if !isBool {
		return false, fmt.Errorf("condition must return bool, got %T", value)
	}
	return ok, nil
}
func validateDefinition(d Definition, c runnerConfig) error {
	for n, t := range d.Tasks {
		if n == "" || t.Name != "" && t.Name != n {
			return fmt.Errorf("%w: task name %s", ErrInvalidDefinition, n)
		}
		for _, dep := range t.Deps {
			if _, ok := d.Tasks[dep]; !ok {
				return fmt.Errorf("%w: task %s depends on %s", ErrInvalidDefinition, n, dep)
			}
		}
		for _, s := range t.Steps {
			if s.Task != nil {
				if _, ok := d.Tasks[s.Task.Name]; !ok {
					return fmt.Errorf("%w: task call %s", ErrInvalidDefinition, s.Task.Name)
				}
			}
			if s.Host != nil {
				if _, ok := c.handlers[s.Host.Name]; !ok {
					return fmt.Errorf("%w: host %s", ErrInvalidDefinition, s.Host.Name)
				}
			}
		}
	}
	return nil
}
func cloneDefinition(d Definition) Definition { return d }
func cloneTask(t Task) Task                   { return t }
