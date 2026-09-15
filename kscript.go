package kscript

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
	p := Plan{Task: req.Task}
	var walk func(Task) error
	walk = func(task Task) error {
		if task.If != "" {
			p.Deferred = append(p.Deferred, "task condition: "+task.Name+": "+task.If)
		}
		for _, dep := range task.Deps {
			dt, err := r.Lookup(dep)
			if err != nil {
				return err
			}
			if err := walk(dt); err != nil {
				return err
			}
		}
		for _, step := range task.Steps {
			if step.If != "" {
				p.Deferred = append(p.Deferred, "step condition: "+step.Name+": "+step.If)
			}
			kind := ""
			program := ""
			args := []string{}
			if step.Exec != nil {
				kind = "exec"
				program = step.Exec.Program
				args = step.Exec.Args
			}
			if step.Shell != nil {
				kind = "shell"
				program = step.Shell.Name
			}
			if step.File != nil {
				kind = "file"
				program = step.File.Interpreter.Program
				args = append(step.File.Interpreter.PrefixArgs, step.File.Path)
			}
			if step.Task != nil {
				ct, err := r.Lookup(step.Task.Name)
				if err != nil {
					return err
				}
				if err := walk(ct); err != nil {
					return err
				}
				continue
			}
			p.Actions = append(p.Actions, PlannedAction{Task: task.Name, Step: step.Name, Kind: kind, Program: program, Args: args, Dir: req.Dir})
		}
		return nil
	}
	if err := walk(t); err != nil {
		return Plan{}, err
	}
	return p, nil
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
		if errors.Is(err, context.Canceled) {
			result.Status = StatusCanceled
        return result, err
		if errors.Is(err, context.DeadlineExceeded) {
			result.Status = StatusTimedOut
		}
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
	if t.Dir != "" {
		req.Dir = t.Dir
	}
	if req.Vars == nil {
		req.Vars = map[string]any{}
	}
	if len(t.Vars) > 0 {
		m := map[string]any{}
		for k, v := range req.Vars {
			m[k] = v
		}
		for k, v := range t.Vars {
			m[k] = v
		}
		req.Vars = m
	}
	if len(t.Env) > 0 {
		if req.Env == nil {
			req.Env = map[string]string{}
		}
		merged := map[string]string{}
		for k, v := range req.Env {
			merged[k] = v
		}
		for k, v := range t.Env {
			merged[k] = v
		}
		req.Env = merged
	}
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
		stepDir := req.Dir
		if step.Dir != "" {
			stepDir = step.Dir
		}
		stepVars := req.Vars
		if len(step.Vars) > 0 {
			m := map[string]any{}
			for k, v := range req.Vars {
				m[k] = v
			}
			for k, v := range step.Vars {
				m[k] = v
			}
			stepVars = m
		}
		stepCtx := ctx
		if step.Timeout > 0 {
			var cancel context.CancelFunc
			stepCtx, cancel = context.WithTimeout(ctx, time.Duration(step.Timeout))
			defer cancel()
		}
		if step.If != "" {
			ok, err := evalCondition(step.If, stepVars)
			if err != nil {
				return err
			}
			if !ok {
				result.Steps = append(result.Steps, StepResult{Name: step.Name, Status: StatusSkipped})
				continue
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
			action := PreparedAction{Dir: stepDir, Env: req.Env}
			if step.Exec != nil {
				action.Kind = "exec"
				action.Program = render(step.Exec.Program, stepVars, req)
				action.Args = make([]string, len(step.Exec.Args))
				for i, v := range step.Exec.Args {
					action.Args[i] = render(v, stepVars, req)
				}
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
				if step.IgnoreError {
					result.Status = StatusSucceededWithWarnings
					result.Steps = append(result.Steps, StepResult{Name: step.Name, Status: StatusSucceededWithWarnings, Err: err})
					continue
				}
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

var variablePattern = regexp.MustCompile(`\$\{(vars|env|args)\.([^}]+)\}`)

func render(value string, vars map[string]any, req Request) string {
	return variablePattern.ReplaceAllStringFunc(value, func(token string) string {
		parts := variablePattern.FindStringSubmatch(token)
		if len(parts) != 3 {
			return token
		}
		switch parts[1] {
		case "vars":
			if v, ok := vars[parts[2]]; ok {
				return fmt.Sprint(v)
			}
		case "env":
			if v, ok := req.Env[parts[2]]; ok {
				return v
			}
		case "args":
			if i, err := strconv.Atoi(parts[2]); err == nil && i > 0 && i <= len(req.Args) {
				return req.Args[i-1]
			}
		}
		return token
	})
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
