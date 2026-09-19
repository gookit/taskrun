package kscript

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type executionMode int

const (
	modeRun executionMode = iota
	modeInspect
)

// callState is the isolated state of one task call. Every call has its own
// identifier, arguments, directory and deadline, and rebuilds its variables and
// environment from the definition defaults and the Request instead of
// inheriting the caller's local values.
type callState struct {
	id    string
	task  Task
	args  []string
	depth int
	dir   string
}

// varScope resolves static and dynamic variables for one task call or step.
type varScope struct {
	s      *runState
	ctx    context.Context
	task   string
	callID string
	step   string
	dir    string
	env    map[string]string
	// taskEnv is the rendered task level environment, kept so a step scope can
	// rebuild the final environment without re-rendering the task level.
	taskEnv map[string]string
	// paths lists the EnvPaths directories already prepended to PATH.
	paths  []string
	args   []string
	run    map[string]any
	vars   map[string]any
	dyn    map[string]DynamicVar
	values map[string]any
}

func (v *varScope) lookup(name string) (any, bool, error) {
	if value, ok := v.vars[name]; ok {
		return value, true, nil
	}
	if v.values != nil {
		if value, ok := v.values[name]; ok {
			return value, true, nil
		}
	}
	spec, ok := v.dyn[name]
	if !ok {
		return nil, false, nil
	}
	if v.s.mode == modeInspect {
		return nil, false, errDeferred
	}
	value, err := v.s.evalDynamicVar(v, name, spec)
	if err != nil {
		return nil, false, err
	}
	if v.values == nil {
		v.values = map[string]any{}
	}
	v.values[name] = value
	return value, true, nil
}

func (v *varScope) deferredNames() []string {
	if len(v.dyn) == 0 {
		return nil
	}
	names := make([]string, 0, len(v.dyn))
	for name := range v.dyn {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func (v *varScope) renderVars() renderVars {
	return renderVars{
		Vars:          v.vars,
		Env:           v.env,
		Args:          v.args,
		Host:          v.s.host,
		Run:           v.run,
		Dynamic:       v.lookup,
		DeferredNames: v.deferredNames(),
	}
}

// visibleVars returns static plus already resolved dynamic values for handlers.
func (v *varScope) visibleVars() map[string]any {
	return mergeDataMaps(v.vars, v.values)
}

type runState struct {
	r       *Runner
	mode    executionMode
	ctx     context.Context
	req     Request
	host    map[string]any
	defVars map[string]any
	// defEnv holds only the rendered definition level Env values; the base
	// environment is merged later so CleanEnv can still drop it.
	defEnv      map[string]string
	defEnvPaths []string
	baseDir     string
	callSeq     int
	calls       int
	ignored     bool
	skipped     bool
	result      *Result
	plan        *Plan
}

func newRunState(ctx context.Context, r *Runner, req Request, mode executionMode) (*runState, error) {
	s := &runState{
		r:    r,
		mode: mode,
		ctx:  ctx,
		req:  req,
		host: cloneDataMap(req.HostData),
		result: &Result{
			Task: req.Task,
		},
	}
	if s.host == nil {
		s.host = map[string]any{}
	}
	s.baseDir = r.def.BaseDir
	if req.Dir != "" {
		s.baseDir = resolveDir(r.def.BaseDir, req.Dir)
	}
	baseView := renderVars{
		Env:  s.r.cfg.baseEnv,
		Args: req.Args,
		Host: s.host,
		Run:  s.runMeta("", "", s.baseDir),
	}
	vars, err := resolveVarLevel(r.def.Vars, baseView)
	if err != nil {
		return nil, err
	}
	s.defVars = mergeDataMaps(vars, req.Vars)
	// Environment values may reference variables, so they are rendered once the
	// definition variables are known.
	view := baseView
	view.Vars = s.defVars
	renderedEnv, err := s.renderEnvMap(r.def.Env, view)
	if err != nil {
		return nil, err
	}
	s.defEnv = renderedEnv
	renderedPaths, err := s.renderPaths(r.def.EnvPaths, view)
	if err != nil {
		return nil, err
	}
	s.defEnvPaths = renderedPaths
	if mode == modeInspect {
		s.plan = &Plan{Task: req.Task}
	}
	return s, nil
}

func (s *runState) runMeta(task, callID, dir string) map[string]any {
	return map[string]any{
		"task": task,
		"call": callID,
		"dir":  dir,
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
	}
}

func (s *runState) nextCallID(name string) string {
	s.callSeq++
	return fmt.Sprintf("%s#%d", name, s.callSeq)
}

// visibleEnv returns the environment visible for rendering at a given level:
// the base snapshot plus the definition defaults, or only the definition
// defaults for a task with CleanEnv.
func (s *runState) visibleEnv(cleanEnv bool) map[string]string {
	if cleanEnv {
		return cloneStringMap(s.defEnv)
	}
	return mergeStringMaps(s.r.cfg.baseEnv, s.defEnv)
}

// composeEnv merges the effective environment in priority order (base,
// definition, task, step, Request) and replaces PATH with the EnvPaths
// directories in front of the inherited value. A task with CleanEnv drops only
// the base environment snapshot.
func (s *runState) composeEnv(cleanEnv bool, taskEnv, stepEnv, reqEnv map[string]string, paths []string) map[string]string {
	parts := []map[string]string{}
	if !cleanEnv {
		parts = append(parts, s.r.cfg.baseEnv)
	}
	parts = append(parts, s.defEnv, taskEnv, stepEnv, reqEnv)
	out := mergeStringMaps(parts...)
	if len(paths) > 0 {
		existing, _ := lookupEnv(out, "PATH")
		for key := range out {
			if envKey(key) == envKey("PATH") {
				delete(out, key)
			}
		}
		joined := strings.Join(paths, string(os.PathListSeparator))
		if existing != "" {
			joined += string(os.PathListSeparator) + existing
		}
		out["PATH"] = joined
	}
	return out
}

// envView builds the read view used to render Env values and EnvPaths at one
// level. Dynamic variables must never decide the process environment, so a
// dynamic reference is an error while running and a deferred field while
// inspecting.
func (s *runState) envView(call *callState, dir string, vars map[string]any, visible map[string]string, dyn map[string]DynamicVar) renderVars {
	view := renderVars{
		Vars: vars,
		Env:  visible,
		Args: call.args,
		Host: s.host,
		Run:  s.runMeta(call.task.Name, call.id, dir),
	}
	if len(dyn) == 0 {
		return view
	}
	names := make([]string, 0, len(dyn))
	for name := range dyn {
		names = append(names, name)
	}
	sortStrings(names)
	view.DeferredNames = names
	view.Dynamic = func(name string) (any, bool, error) {
		if _, declared := dyn[name]; !declared {
			return nil, false, nil
		}
		if s.mode == modeInspect {
			return nil, false, errDeferred
		}
		return nil, false, invalidDef("environment values must not depend on the dynamic variable %q: dynamic variables cannot decide the process environment", name)
	}
	return view
}

func (s *runState) renderEnvMap(env map[string]string, view renderVars) (map[string]string, error) {
	if len(env) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(env))
	for _, key := range sortedKeys(env) {
		value, err := renderTemplate(env[key], view)
		if err != nil {
			if errors.Is(err, errDeferred) && s.mode == modeInspect {
				s.deferPlan("environment %s depends on a dynamic variable", key)
				out[key] = env[key]
				continue
			}
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

func (s *runState) renderPaths(list []string, view renderVars) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		rendered, err := renderTemplate(item, view)
		if err != nil {
			if errors.Is(err, errDeferred) && s.mode == modeInspect {
				s.deferPlan("env_paths entry depends on a dynamic variable")
				out = append(out, item)
				continue
			}
			return nil, err
		}
		out = append(out, rendered)
	}
	return out, nil
}

// resolveLevelVars resolves one variable level. While inspecting, a level that
// depends on a dynamic variable is reported as deferred instead of failing.
func (s *runState) resolveLevelVars(where string, level map[string]any, view renderVars) (map[string]any, error) {
	vars, err := resolveVarLevel(level, view)
	if err != nil {
		if errors.Is(err, errDeferred) && s.mode == modeInspect {
			s.deferPlan("%s variables depend on a dynamic variable", where)
			return nil, nil
		}
		return nil, err
	}
	return vars, nil
}

// Run executes the requested task in an isolated state. A returned error is
// always a *RunError and its kind matches the Result status.
func (r *Runner) Run(ctx context.Context, req Request) (*Result, error) {
	if ctx == nil {
		return nil, &RunError{Kind: ErrKindInvalidRequest, Task: req.Task, Err: errf("ctx is nil")}
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &RunError{Kind: ErrKindCanceled, Task: req.Task, Err: err}
	}
	req = newRequest(req)
	if req.DryRun {
		plan, err := r.Inspect(ctx, req)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		return &Result{Status: StatusDryRun, Task: req.Task, Plan: &plan, StartedAt: now, EndedAt: now}, nil
	}
	root, err := r.Lookup(req.Task)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	s, err := newRunState(ctx, r, req, modeRun)
	if err != nil {
		return nil, err
	}
	call := &callState{
		id:    s.nextCallID(root.Name),
		task:  root,
		args:  cloneStrings(req.Args),
		depth: 1,
		dir:   s.baseDir,
	}
	runErr := s.runTask(ctx, call)
	s.result.StartedAt = started
	s.result.EndedAt = time.Now()
	s.result.Status = s.finalStatus(runErr)
	return s.result, runErr
}

func (s *runState) finalStatus(runErr error) Status {
	if runErr == nil {
		switch {
		case s.ignored:
			return StatusSucceededWithWarnings
		case s.skipped:
			return StatusSkipped
		default:
			return StatusSucceeded
		}
	}
	var classified *RunError
	if errors.As(runErr, &classified) {
		switch classified.Kind {
		case ErrKindCanceled:
			return StatusCanceled
		case ErrKindTimedOut:
			return StatusTimedOut
		}
	}
	return StatusFailed
}

// runTask executes one task call: platform and condition gates, dependencies,
// then steps, each with its own deadline.
func (s *runState) runTask(parent context.Context, call *callState) error {
	s.calls++
	if s.calls > s.r.cfg.maxExpansions {
		return &RunError{Kind: ErrKindExpansionLimit, Task: call.task.Name, CallID: call.id,
			Err: errf("%w: more than %d task calls", ErrExpansionLimit, s.r.cfg.maxExpansions)}
	}
	record := func(status Status, reason string, err error, startedAt time.Time) {
		s.result.Tasks = append(s.result.Tasks, TaskResult{
			Name: call.task.Name, CallID: call.id, Status: status, Reason: reason, Err: err,
			StartedAt: startedAt, EndedAt: time.Now(),
		})
	}
	startedAt := time.Now()
	task := call.task
	if !platformMatches(task.Platform, runtime.GOOS) {
		s.skipped = s.skipped || call.depth == 1
		record(StatusSkipped, fmt.Sprintf("platform %s not in %v", runtime.GOOS, task.Platform), nil, startedAt)
		s.note("task %s skipped: platform %s", task.Name, runtime.GOOS)
		return nil
	}
	ctx, cancel := withBudget(parent, task.Timeout)
	defer cancel()
	call.dir = resolveDir(call.dir, task.Dir)
	scope, err := s.newTaskScope(ctx, call)
	if err != nil {
		return s.fail(err, call, "")
	}

	evaluator, err := compileCondition(task.If, scope.renderVars())
	if err != nil {
		if errors.Is(err, errDeferred) {
			s.deferPlan("task condition %s: %s", task.Name, task.If)
			evaluator = func() (bool, error) { return true, nil }
		} else {
			return s.fail(err, call, "")
		}
	}
	ok, err := evaluator()
	if err != nil {
		return s.fail(err, call, "")
	}
	if !ok {
		s.skipped = s.skipped || call.depth == 1
		record(StatusSkipped, "condition false", nil, startedAt)
		s.note("task %s skipped: condition false", task.Name)
		return nil
	}
	for _, dep := range task.Deps {
		child, err := s.childCall(call, dep, call.args)
		if err != nil {
			return s.fail(err, call, "")
		}
		if err := s.runTask(ctx, child); err != nil {
			return err
		}
	}
	for index, step := range task.Steps {
		if err := s.runStep(ctx, call, scope, index, step); err != nil {
			return err
		}
	}
	record(StatusSucceeded, "", nil, startedAt)
	return nil
}

// childCall builds an isolated call for a dependency or a task call. The child
// rebuilds its variables and environment from the definition defaults and the
// Request, so the caller's local values never leak in.
func (s *runState) childCall(parent *callState, name string, args []string) (*callState, error) {
	task, ok := s.r.def.Tasks[name]
	if !ok {
		return nil, invalidDef("task %s calls unknown task %s", parent.task.Name, name)
	}
	return &callState{
		id:    s.nextCallID(name),
		task:  cloneTask(task),
		args:  cloneStrings(args),
		depth: parent.depth + 1,
		dir:   s.baseDir,
	}, nil
}

// newTaskScope resolves the variables, environment and EnvPaths of one task
// call. Variables are resolved before the environment, because environment
// values may reference them, and the environment is visible to conditions and
// actions but never to the variables of the same level.
func (s *runState) newTaskScope(ctx context.Context, call *callState) (*varScope, error) {
	task := call.task
	dir := call.dir
	visible := s.visibleEnv(task.CleanEnv)
	scope := &varScope{
		s:      s,
		ctx:    ctx,
		task:   task.Name,
		callID: call.id,
		dir:    dir,
		args:   call.args,
		run:    s.runMeta(task.Name, call.id, dir),
		vars:   mergeDataMaps(s.defVars),
		dyn:    task.DynamicVars,
		env:    visible,
	}
	varsView := scope.renderVars()
	varsView.Env = visible
	vars, err := s.resolveLevelVars("task "+task.Name, task.Vars, varsView)
	if err != nil {
		return nil, err
	}
	scope.vars = mergeDataMaps(s.defVars, vars)
	envView := s.envView(call, dir, scope.vars, visible, scope.dyn)
	taskEnv, err := s.renderEnvMap(task.Env, envView)
	if err != nil {
		return nil, err
	}
	taskPaths, err := s.renderPaths(task.EnvPaths, envView)
	if err != nil {
		return nil, err
	}
	// The Request environment applies to the whole run, so it is visible to the
	// task condition as well as to the steps.
	requestEnv, err := s.renderEnvMap(s.req.Env, envView)
	if err != nil {
		return nil, err
	}
	scope.taskEnv = taskEnv
	scope.paths = append(taskPaths, s.defEnvPaths...)
	scope.env = s.composeEnv(task.CleanEnv, taskEnv, nil, requestEnv, scope.paths)
	return scope, nil
}

// newStepScope resolves the variables, environment and EnvPaths of one step on
// top of its task scope and adds the Request environment, which has the highest
// priority.
func (s *runState) newStepScope(ctx context.Context, call *callState, taskScope *varScope, index int, step Step) (*varScope, error) {
	dir := resolveDir(call.dir, step.Dir)
	scope := &varScope{
		s:      s,
		ctx:    ctx,
		task:   call.task.Name,
		callID: call.id,
		step:   stepLabel(step, index),
		dir:    dir,
		args:   call.args,
		run:    s.runMeta(call.task.Name, call.id, dir),
		vars:   taskScope.vars,
		dyn:    mergeDynamicVars(taskScope.dyn, step.DynamicVars),
		env:    taskScope.env,
	}
	varsView := scope.renderVars()
	varsView.Env = taskScope.env
	vars, err := s.resolveLevelVars("step "+scope.step, step.Vars, varsView)
	if err != nil {
		return nil, err
	}
	scope.vars = mergeDataMaps(taskScope.vars, vars)
	envView := s.envView(call, dir, scope.vars, taskScope.env, scope.dyn)
	stepEnv, err := s.renderEnvMap(step.Env, envView)
	if err != nil {
		return nil, err
	}
	requestEnv, err := s.renderEnvMap(s.req.Env, envView)
	if err != nil {
		return nil, err
	}
	stepPaths, err := s.renderPaths(step.EnvPaths, envView)
	if err != nil {
		return nil, err
	}
	scope.taskEnv = taskScope.taskEnv
	scope.paths = append(stepPaths, taskScope.paths...)
	scope.env = s.composeEnv(call.task.CleanEnv, taskScope.taskEnv, stepEnv, requestEnv, scope.paths)
	return scope, nil
}

// runStep evaluates and executes a single step.
func (s *runState) runStep(parent context.Context, call *callState, taskScope *varScope, index int, step Step) error {
	ctx, cancel := withBudget(parent, step.Timeout)
	defer cancel()
	label := stepLabel(step, index)
	if !platformMatches(step.Platform, runtime.GOOS) {
		s.recordStep(call, step, StepResult{Status: StatusSkipped, Err: nil})
		s.note("task %s step %s skipped: platform %s", call.task.Name, label, runtime.GOOS)
		return nil
	}
	scope, err := s.newStepScope(ctx, call, taskScope, index, step)
	if err != nil {
		return s.fail(err, call, label)
	}
	evaluator, err := compileCondition(step.If, scope.renderVars())
	if err != nil {
		if errors.Is(err, errDeferred) {
			s.deferPlan("step condition %s/%s: %s", call.task.Name, scope.step, step.If)
			evaluator = func() (bool, error) { return true, nil }
		} else {
			return s.fail(err, call, scope.step)
		}
	}
	ok, err := evaluator()
	if err != nil {
		return s.fail(err, call, scope.step)
	}
	if !ok {
		s.recordStep(call, step, StepResult{Status: StatusSkipped})
		s.note("task %s step %s skipped: condition false", call.task.Name, scope.step)
		return nil
	}
	switch step.actionKind() {
	case "task":
		return s.runTaskCall(ctx, call, scope, step)
	case "host":
		return s.runHost(ctx, call, scope, step)
	default:
		return s.runExternal(ctx, call, scope, step)
	}
}

func (s *runState) runTaskCall(ctx context.Context, call *callState, scope *varScope, step Step) error {
	args := step.Task.Args
	if args == nil {
		args = call.args
	} else {
		rendered, err := renderStrings(args, scope.renderVars())
		if err != nil {
			return s.fail(err, call, scope.step)
		}
		args = rendered
	}
	if step.Task.ForwardArgs {
		args = append(cloneStrings(args), s.req.Args...)
	}
	child, err := s.childCall(call, step.Task.Name, args)
	if err != nil {
		return s.fail(err, call, scope.step)
	}
	if s.mode == modeInspect {
		return s.planTaskCall(ctx, child)
	}
	if err := s.runTask(ctx, child); err != nil {
		return err
	}
	s.recordStep(call, step, StepResult{Kind: "task", Status: StatusSucceeded})
	return nil
}

// planTaskCall expands a called task while inspecting.
func (s *runState) planTaskCall(ctx context.Context, call *callState) error {
	return s.runTask(ctx, call)
}

func (s *runState) runHost(ctx context.Context, call *callState, scope *varScope, step Step) error {
	handler := s.r.cfg.handlers[step.Host.Name]
	if s.mode == modeInspect {
		s.planAction(PlannedAction{
			Task: call.task.Name, CallID: call.id, Step: scope.step, Kind: "host",
			Program: step.Host.Name, Dir: scope.dir, EnvKeys: sortedKeys(scope.env), Status: StatusPlanned,
		})
		return nil
	}
	args := make([]any, len(step.Host.Args))
	for i, arg := range step.Host.Args {
		if text, ok := arg.(string); ok {
			rendered, err := renderTemplate(text, scope.renderVars())
			if err != nil {
				return s.fail(err, call, scope.step)
			}
			args[i] = rendered
			continue
		}
		args[i] = cloneData(arg)
	}
	call_ := HostCall{
		Name: step.Host.Name,
		Args: args,
		Vars: scope.visibleVars(),
		Env:  cloneStringMap(scope.env),
		Dir:  scope.dir,
	}
	result, err := invokeHandler(ctx, handler, call_)
	if err != nil {
		classified := s.classify(err, call, scope.step, ErrKindHandler)
		if step.IgnoreError {
			s.ignored = true
			classified.Kind = ErrKindHandler
			s.recordStep(call, step, StepResult{Kind: "host", Status: StatusIgnoredFailure, Started: true, Err: classified})
			return nil
		}
		return s.fail(classified, call, scope.step)
	}
	s.recordStep(call, step, StepResult{
		Kind: "host", Status: StatusSucceeded, Started: true,
		ExitCode: result.ExitCode, Output: result.Output, ErrorOutput: result.ErrorOutput, Truncated: result.Truncated,
	})
	return nil
}

func invokeHandler(ctx context.Context, handler Handler, call HostCall) (result ActionResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &RunError{Kind: ErrKindHandler, Err: errf("handler %s panicked: %v", call.Name, recovered)}
		}
	}()
	return handler(ctx, call)
}

func (s *runState) runExternal(ctx context.Context, call *callState, scope *varScope, step Step) error {
	action, err := s.prepareAction(scope, step)
	if err != nil {
		if errors.Is(err, errDeferred) && s.mode == modeInspect {
			s.deferPlan("action %s/%s depends on a dynamic variable", call.task.Name, scope.step)
			s.planDeferredAction(call, scope, step)
			return nil
		}
		return s.fail(err, call, scope.step)
	}
	if s.mode == modeInspect {
		s.planAction(PlannedAction{
			Task: call.task.Name, CallID: call.id, Step: scope.step, Kind: action.Kind,
			Program: action.Program, Args: action.Args, Script: action.Script,
			Dir: action.Dir, EnvKeys: sortedKeys(scope.env), Status: StatusPlanned,
		})
		return nil
	}
	started := time.Now()
	result, execErr := s.r.cfg.engine.Execute(ctx, action, s.req.IO)
	stepResult := StepResult{
		Kind: action.Kind, Started: result.Started, ExitCode: result.ExitCode,
		Output: result.Output, ErrorOutput: result.ErrorOutput, Truncated: result.Truncated,
		StartedAt: started, EndedAt: time.Now(),
	}
	if execErr != nil {
		classified := s.classify(execErr, call, scope.step, ErrKindExit)
		stepResult.Err = classified
		if step.IgnoreError && toleratedFailure(classified.Kind) {
			s.ignored = true
			stepResult.Status = StatusIgnoredFailure
			s.recordStep(call, step, stepResult)
			return nil
		}
		stepResult.Status = statusForKind(classified.Kind)
		s.recordStep(call, step, stepResult)
		return classified
	}
	stepResult.Status = StatusSucceeded
	s.recordStep(call, step, stepResult)
	return nil
}

// toleratedFailure reports whether ignore_error may absorb a failure kind.
func toleratedFailure(kind ErrorKind) bool {
	return kind == ErrKindExit || kind == ErrKindHandler
}

func statusForKind(kind ErrorKind) Status {
	switch kind {
	case ErrKindCanceled:
		return StatusCanceled
	case ErrKindTimedOut:
		return StatusTimedOut
	default:
		return StatusFailed
	}
}

// prepareAction renders and validates an external action.
func (s *runState) prepareAction(scope *varScope, step Step) (PreparedAction, error) {
	rv := scope.renderVars()
	action := PreparedAction{Dir: scope.dir, Env: envList(scope.env)}
	switch {
	case step.Exec != nil:
		program, err := renderTemplate(step.Exec.Program, rv)
		if err != nil {
			return action, err
		}
		args, err := renderStrings(step.Exec.Args, rv)
		if err != nil {
			return action, err
		}
		if strings.TrimSpace(program) == "" {
			return action, invalidDef("step %s has an empty exec program", scope.step)
		}
		action.Kind = "exec"
		action.Program = program
		action.Args = args
	case step.Shell != nil:
		script, err := renderTemplate(step.Shell.Script, rv)
		if err != nil {
			return action, err
		}
		action.Kind = "shell"
		action.Program = normalizeShellName(step.Shell.Name)
		action.Script = script
		if len(step.Shell.PrefixArgs) > 0 {
			args, err := renderStrings(step.Shell.PrefixArgs, rv)
			if err != nil {
				return action, err
			}
			action.Args = args
		}
	case step.File != nil:
		file, ok := s.r.def.Files[step.File.Name]
		if !ok {
			return action, invalidDef("step %s references unknown script file %q", scope.step, step.File.Name)
		}
		program, err := renderTemplate(file.Interpreter.Program, rv)
		if err != nil {
			return action, err
		}
		args, err := renderStrings(file.Interpreter.PrefixArgs, rv)
		if err != nil {
			return action, err
		}
		args = append(args, file.Path)
		extra, err := renderStrings(step.File.Args, rv)
		if err != nil {
			return action, err
		}
		args = append(args, extra...)
		if file.Dir != "" {
			action.Dir = resolveDir(scope.dir, file.Dir)
		}
		if len(file.Env) > 0 {
			action.Env = envList(mergeStringMaps(scope.env, file.Env))
		}
		action.Kind = "file"
		action.Program = program
		action.Args = args
	default:
		return action, invalidDef("step %s has no external action", scope.step)
	}
	return action, nil
}

// prepareDynamicAction builds the command that produces a dynamic variable.
func (s *runState) prepareDynamicAction(scope *varScope, spec DynamicVar) (PreparedAction, error) {
	rv := scope.renderVars()
	action := PreparedAction{Dir: scope.dir, Env: envList(scope.env)}
	switch {
	case spec.Exec != nil:
		program, err := renderTemplate(spec.Exec.Program, rv)
		if err != nil {
			return action, err
		}
		args, err := renderStrings(spec.Exec.Args, rv)
		if err != nil {
			return action, err
		}
		action.Kind = "exec"
		action.Program = program
		action.Args = args
	case spec.Shell != nil:
		script, err := renderTemplate(spec.Shell.Script, rv)
		if err != nil {
			return action, err
		}
		action.Kind = "shell"
		action.Program = normalizeShellName(spec.Shell.Name)
		action.Script = script
	case spec.File != nil:
		file, ok := s.r.def.Files[spec.File.Name]
		if !ok {
			return action, invalidDef("dynamic variable references unknown script file %q", spec.File.Name)
		}
		program, err := renderTemplate(file.Interpreter.Program, rv)
		if err != nil {
			return action, err
		}
		args, err := renderStrings(file.Interpreter.PrefixArgs, rv)
		if err != nil {
			return action, err
		}
		args = append(args, file.Path)
		action.Kind = "file"
		action.Program = program
		action.Args = args
	default:
		return action, invalidDef("dynamic variable has no action")
	}
	return action, nil
}

func (s *runState) evalDynamicVar(scope *varScope, name string, spec DynamicVar) (any, error) {
	action, err := s.prepareDynamicAction(scope, spec)
	if err != nil {
		return nil, s.classify(err, nil, scope.step, ErrKindInvalidDefinition)
	}
	result, err := s.r.cfg.engine.Execute(scope.ctx, action, IO{CaptureLimit: s.r.cfg.dynamicLimit})
	if err != nil {
		return nil, s.classify(err, nil, scope.step, ErrKindExit)
	}
	if result.Truncated {
		return nil, &RunError{Kind: ErrKindOutputLimit, Task: scope.task, CallID: scope.callID, Step: scope.step,
			Err: errf("%w: dynamic variable %q output exceeded %d bytes", ErrOutputLimit, name, s.r.cfg.dynamicLimit)}
	}
	return strings.TrimRight(string(result.Output), "\r\n"), nil
}

// classify converts any error into a RunError carrying run coordinates.
func (s *runState) classify(err error, call *callState, step string, fallback ErrorKind) *RunError {
	if err == nil {
		return nil
	}
	var existing *RunError
	if errors.As(err, &existing) {
		out := *existing
		if out.Task == "" && call != nil {
			out.Task = call.task.Name
		}
		if out.CallID == "" && call != nil {
			out.CallID = call.id
		}
		if out.Step == "" {
			out.Step = step
		}
		if out.Source == "" {
			out.Source = s.sourceName()
		}
		if out.Err == nil {
			out.Err = err
		} else if existing.Err == err {
			out.Err = fallbackCause(err, fallback)
		}
		return &out
	}
	kind := fallback
	var procErr *ProcessError
	switch {
	case errors.Is(err, context.Canceled):
		kind = ErrKindCanceled
	case errors.Is(err, context.DeadlineExceeded):
		kind = ErrKindTimedOut
	case errors.As(err, &procErr):
		kind = procErr.Kind
	}
	out := &RunError{Kind: kind, Step: step, Source: s.sourceName(), Err: err}
	if call != nil {
		out.Task = call.task.Name
		out.CallID = call.id
	}
	return out
}

func fallbackCause(err error, fallback ErrorKind) error { return err }

func (s *runState) sourceName() string {
	if len(s.r.def.Sources) == 0 {
		return ""
	}
	names := make([]string, 0, len(s.r.def.Sources))
	for _, source := range s.r.def.Sources {
		if source.Name != "" {
			names = append(names, source.Name)
		}
	}
	return strings.Join(names, ", ")
}

func (s *runState) fail(err error, call *callState, step string) error {
	if err == nil {
		return nil
	}
	var callPtr *callState
	if call != nil {
		callPtr = call
	}
	return s.classify(err, callPtr, step, ErrKindInvalidDefinition)
}

func (s *runState) recordStep(call *callState, step Step, result StepResult) {
	if s.mode == modeInspect {
		return
	}
	if call != nil {
		result.CallID = call.id
	}
	if result.Name == "" {
		result.Name = step.Name
	}
	if result.StartedAt.IsZero() {
		result.StartedAt = time.Now()
	}
	if result.EndedAt.IsZero() {
		result.EndedAt = result.StartedAt
	}
	s.result.Steps = append(s.result.Steps, result)
}

func (s *runState) planAction(action PlannedAction) {
	if s.plan != nil {
		s.plan.Actions = append(s.plan.Actions, action)
	}
}

func (s *runState) planDeferredAction(call *callState, scope *varScope, step Step) {
	kind := step.actionKind()
	action := PlannedAction{Task: call.task.Name, CallID: call.id, Step: scope.step, Kind: kind, Status: StatusDeferred, Reason: "depends on a dynamic variable"}
	switch {
	case step.Exec != nil:
		action.Program = step.Exec.Program
		action.Args = cloneStrings(step.Exec.Args)
	case step.Shell != nil:
		action.Program = step.Shell.Name
		action.Script = step.Shell.Script
	case step.File != nil:
		action.Program = step.File.Name
		action.Args = cloneStrings(step.File.Args)
	case step.Host != nil:
		action.Program = step.Host.Name
	}
	action.Dir = scope.dir
	action.EnvKeys = sortedKeys(scope.env)
	s.planAction(action)
}

func (s *runState) note(format string, args ...any) {
	if s.plan != nil {
		s.plan.Skipped = append(s.plan.Skipped, fmt.Sprintf(format, args...))
	}
}

func (s *runState) deferPlan(format string, args ...any) {
	if s.plan != nil {
		s.plan.Deferred = append(s.plan.Deferred, fmt.Sprintf(format, args...))
	}
}

// Inspect validates the request and expands the call graph without running any
// action, handler or dynamic variable command.
func (r *Runner) Inspect(ctx context.Context, req Request) (Plan, error) {
	if ctx == nil {
		return Plan{}, &RunError{Kind: ErrKindInvalidRequest, Task: req.Task, Err: errf("ctx is nil")}
	}
	if err := req.validate(); err != nil {
		return Plan{}, err
	}
	if err := ctx.Err(); err != nil {
		return Plan{}, &RunError{Kind: ErrKindCanceled, Task: req.Task, Err: err}
	}
	req = newRequest(req)
	root, err := r.Lookup(req.Task)
	if err != nil {
		return Plan{}, err
	}
	s, err := newRunState(ctx, r, req, modeInspect)
	if err != nil {
		return Plan{}, err
	}
	call := &callState{id: s.nextCallID(root.Name), task: root, args: cloneStrings(req.Args), depth: 1, dir: s.baseDir}
	if err := s.runTask(ctx, call); err != nil {
		return Plan{}, err
	}
	return *s.plan, nil
}

func renderStrings(values []string, rv renderVars) ([]string, error) {
	out := make([]string, len(values))
	for i, value := range values {
		rendered, err := renderTemplate(value, rv)
		if err != nil {
			return nil, err
		}
		out[i] = rendered
	}
	return out, nil
}

func withBudget(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func resolveDir(base, dir string) string {
	if dir == "" {
		return base
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Clean(filepath.Join(base, dir))
}

func mergeStringMaps(maps ...map[string]string) map[string]string {
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	out := make(map[string]string, total)
	for _, m := range maps {
		for key, value := range m {
			if existing, ok := lookupEnvKey(out, key); ok && existing != key {
				delete(out, existing)
			}
			out[key] = value
		}
	}
	return out
}

func mergeDynamicVars(maps ...map[string]DynamicVar) map[string]DynamicVar {
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	if total == 0 {
		return nil
	}
	out := make(map[string]DynamicVar, total)
	for _, m := range maps {
		for name, dyn := range m {
			out[name] = dyn
		}
	}
	return out
}

// lookupEnvKey finds the stored spelling of an environment name, so Windows
// case-insensitive duplicates collapse to one entry.
func lookupEnvKey(env map[string]string, name string) (string, bool) {
	if _, ok := env[name]; ok {
		return name, true
	}
	if !isWindows {
		return "", false
	}
	want := envKey(name)
	for key := range env {
		if envKey(key) == want {
			return key, true
		}
	}
	return "", false
}
