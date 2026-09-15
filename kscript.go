package kscript

import (
	"context"
	"errors"
	"fmt"
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
	if _, err := r.Lookup(req.Task); err != nil {
		return Plan{}, err
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
	if len(t.Steps) == 0 && len(t.Deps) == 0 {
		return nil, fmt.Errorf("%w: empty task %s", ErrInvalidDefinition, t.Name)
	}
	return &Result{Status: StatusSucceeded, Task: t.Name}, nil
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
