package taskrun

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gookit/taskrun/internal/graph"

	"github.com/gookit/taskrun/internal/data"
)

// Definition is a task and script definition. New validates it and publishes a
// private snapshot; later mutations of the caller's value have no effect.
type Definition struct {
	// Version is the configuration schema version. It is independent of the Go
	// module version and must be 1 when set.
	Version int
	// BaseDir is the absolute directory that relative Dir values and registered
	// script files resolve against.
	BaseDir string
	// Vars are definition level default variables.
	Vars map[string]any
	// Env are definition level default environment variables.
	Env map[string]string
	// EnvPaths are prepended to the effective PATH, before Task and Step paths.
	EnvPaths []string
	// Tasks are the named tasks.
	Tasks map[string]Task
	// Files is the registry of named script files. A file action refers to a
	// name in this registry; configuration cannot load arbitrary programs.
	Files map[string]ScriptFile
	// Sources records where a loaded definition came from, for diagnostics.
	Sources []Source
}

// Source records the origin of decoded configuration.
type Source struct {
	// Name identifies the file or logical reader.
	Name string
	// BaseDir is the absolute directory of the source.
	BaseDir string
	// Format is "json", "yaml" or "toml" when known.
	Format string
}

// Task is one executable unit of work.
type Task struct {
	Name        string
	Desc        string
	Deps        []string
	Steps       []Step
	Vars        map[string]any
	DynamicVars map[string]DynamicVar
	Env         map[string]string
	// CleanEnv drops the Runner base environment for this task. Explicit Env
	// values are kept.
	CleanEnv bool
	EnvPaths []string
	// Dir is relative to the run base directory, or absolute.
	Dir string
	// If is an expr condition. An empty condition means true.
	If string
	// Platform lists accepted runtime.GOOS values. Empty means every platform.
	Platform []string
	// Timeout bounds this task call, including its dependencies, steps and
	// dynamic variables. Zero inherits the parent budget; negative is invalid.
	Timeout time.Duration
}

// Step is exactly one action.
type Step struct {
	Name        string
	Exec        *ExecSpec
	Shell       *ShellSpec
	File        *FileSpec
	Task        *TaskCall
	Host        *HostSpec
	Vars        map[string]any
	DynamicVars map[string]DynamicVar
	Env         map[string]string
	EnvPaths    []string
	Dir         string
	If          string
	Platform    []string
	// IgnoreError tolerates a started process with a non-zero exit code or a
	// handler business error. It never tolerates a start failure, cancellation,
	// timeout, output-limit, IO or configuration error.
	IgnoreError bool
	// Timeout bounds this step, including its dynamic variables. Zero inherits
	// the task budget; negative is invalid.
	Timeout time.Duration
}

// ExecSpec describes argv execution without shell parsing.
type ExecSpec struct {
	Program string
	Args    []string
}

// ShellSpec describes explicit shell execution. The shell must be named; the
// library never guesses a default shell.
type ShellSpec struct {
	// Name is one of sh, bash, zsh, cmd, pwsh or powershell.
	Name string
	// Script is the complete source, rendered with the same template rules as
	// other fields.
	Script string
	// Args are extra arguments inserted before the script for interpreters that
	// need them. PrefixArgs are not needed for the built-in shells.
	PrefixArgs []string
}

// FileSpec references a registered ScriptFile by name.
type FileSpec struct {
	Name string
	Args []string
}

// Interpreter identifies a script interpreter and its prefix arguments, for
// example go + [run].
type Interpreter struct {
	Program    string
	PrefixArgs []string
}

// TaskCall invokes another task. A nil Args value inherits the current call
// arguments; an empty, non-nil Args replaces them.
type TaskCall struct {
	Name string
	Args []string
	// ForwardArgs appends the root request arguments after Args.
	ForwardArgs bool
}

// HostSpec invokes a handler registered by the embedding application.
type HostSpec struct {
	Name string
	Args []any
}

// DynamicVar is a variable produced by running a command. Only one action may
// be set.
type DynamicVar struct {
	Exec  *ExecSpec
	Shell *ShellSpec
	File  *FileSpec
}

func (d DynamicVar) kind() string {
	switch {
	case d.Exec != nil:
		return "exec"
	case d.Shell != nil:
		return "shell"
	case d.File != nil:
		return "file"
	default:
		return ""
	}
}

// ScriptFile is a named executable script. Path is absolute after New.
type ScriptFile struct {
	Name        string
	Path        string
	Interpreter Interpreter
	Args        []string
	Env         map[string]string
	Dir         string
}

func (s Step) actionKind() string {
	switch {
	case s.Exec != nil:
		return "exec"
	case s.Shell != nil:
		return "shell"
	case s.File != nil:
		return "file"
	case s.Task != nil:
		return "task"
	case s.Host != nil:
		return "host"
	default:
		return ""
	}
}

func (s Step) actionCount() int {
	n := 0
	for _, set := range []bool{s.Exec != nil, s.Shell != nil, s.File != nil, s.Task != nil, s.Host != nil} {
		if set {
			n++
		}
	}
	return n
}

// cloneDefinition deep copies a definition so a Runner never shares mutable
// state with its caller or with another Runner.
func cloneDefinition(def Definition) Definition {
	out := def
	out.Vars = data.CloneMap(def.Vars)
	out.Env = data.CloneStringMap(def.Env)
	out.EnvPaths = data.CloneStrings(def.EnvPaths)
	out.Sources = append([]Source(nil), def.Sources...)
	out.Tasks = make(map[string]Task, len(def.Tasks))
	for name, task := range def.Tasks {
		out.Tasks[name] = cloneTask(task)
	}
	out.Files = make(map[string]ScriptFile, len(def.Files))
	for name, file := range def.Files {
		out.Files[name] = cloneScriptFile(file)
	}
	return out
}

func cloneTask(t Task) Task {
	out := t
	out.Deps = data.CloneStrings(t.Deps)
	out.Vars = data.CloneMap(t.Vars)
	out.Env = data.CloneStringMap(t.Env)
	out.EnvPaths = data.CloneStrings(t.EnvPaths)
	out.Platform = data.CloneStrings(t.Platform)
	out.DynamicVars = cloneDynamicVars(t.DynamicVars)
	out.Steps = make([]Step, len(t.Steps))
	for i, step := range t.Steps {
		out.Steps[i] = cloneStep(step)
	}
	return out
}

func cloneStep(s Step) Step {
	out := s
	out.Vars = data.CloneMap(s.Vars)
	out.Env = data.CloneStringMap(s.Env)
	out.EnvPaths = data.CloneStrings(s.EnvPaths)
	out.Platform = data.CloneStrings(s.Platform)
	out.DynamicVars = cloneDynamicVars(s.DynamicVars)
	if s.Exec != nil {
		spec := *s.Exec
		spec.Args = data.CloneStrings(s.Exec.Args)
		out.Exec = &spec
	}
	if s.Shell != nil {
		spec := *s.Shell
		spec.PrefixArgs = data.CloneStrings(s.Shell.PrefixArgs)
		out.Shell = &spec
	}
	if s.File != nil {
		spec := *s.File
		spec.Args = data.CloneStrings(s.File.Args)
		out.File = &spec
	}
	if s.Task != nil {
		call := *s.Task
		if s.Task.Args != nil {
			call.Args = data.CloneStrings(s.Task.Args)
		} else {
			call.Args = nil
		}
		out.Task = &call
	}
	if s.Host != nil {
		call := *s.Host
		call.Args = make([]any, len(s.Host.Args))
		for i, arg := range s.Host.Args {
			call.Args[i] = data.Clone(arg)
		}
		out.Host = &call
	}
	return out
}

func cloneDynamicVars(in map[string]DynamicVar) map[string]DynamicVar {
	if in == nil {
		return nil
	}
	out := make(map[string]DynamicVar, len(in))
	for name, dyn := range in {
		item := DynamicVar{}
		if dyn.Exec != nil {
			spec := *dyn.Exec
			spec.Args = data.CloneStrings(dyn.Exec.Args)
			item.Exec = &spec
		}
		if dyn.Shell != nil {
			spec := *dyn.Shell
			spec.PrefixArgs = data.CloneStrings(dyn.Shell.PrefixArgs)
			item.Shell = &spec
		}
		if dyn.File != nil {
			spec := *dyn.File
			spec.Args = data.CloneStrings(dyn.File.Args)
			item.File = &spec
		}
		out[name] = item
	}
	return out
}

func cloneScriptFile(f ScriptFile) ScriptFile {
	out := f
	out.Args = data.CloneStrings(f.Args)
	out.Env = data.CloneStringMap(f.Env)
	out.Interpreter.PrefixArgs = data.CloneStrings(f.Interpreter.PrefixArgs)
	return out
}

// shellNames lists the accepted explicit shells.
var shellNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true,
	"cmd": true, "pwsh": true, "powershell": true,
}

// normalizeShellName lower-cases a shell name and drops a Windows extension.
func normalizeShellName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	lower = strings.TrimSuffix(lower, ".exe")
	return lower
}

// platformMatches reports whether goos is accepted by a platform list.
func platformMatches(list []string, goos string) bool {
	if len(list) == 0 {
		return true
	}
	for _, item := range list {
		if item == goos {
			return true
		}
	}
	return false
}

// prepareDefinition clones, normalizes and validates a definition.
func prepareDefinition(def Definition, cfg runnerConfig) (Definition, error) {
	out := cloneDefinition(def)
	if out.Version == 0 {
		out.Version = 1
	}
	if out.Version != 1 {
		return out, invalidDef("unsupported schema version %d", out.Version)
	}
	if out.BaseDir == "" {
		return out, invalidDef("BaseDir is required")
	}
	if !filepath.IsAbs(out.BaseDir) {
		return out, invalidDef("BaseDir %q must be an absolute path", out.BaseDir)
	}
	abs, err := filepath.Abs(out.BaseDir)
	if err != nil {
		return out, invalidDef("BaseDir %q cannot be resolved: %v", out.BaseDir, err)
	}
	out.BaseDir = abs
	if out.Tasks == nil {
		out.Tasks = map[string]Task{}
	}
	if out.Files == nil {
		out.Files = map[string]ScriptFile{}
	}
	if out.Vars == nil {
		out.Vars = map[string]any{}
	}
	if err := validateData(out.Vars, "Definition.Vars"); err != nil {
		return out, err
	}
	if err := detectVarCycle(out.Vars); err != nil {
		return out, err
	}
	// Registered script files resolve to absolute paths at load time so that a
	// later request working directory cannot change them.
	for name, file := range out.Files {
		if err := validateScriptFile(name, &file, out.BaseDir); err != nil {
			return out, err
		}
		out.Files[name] = file
	}
	for name, task := range out.Tasks {
		if err := validateTask(name, &task, out, cfg); err != nil {
			return out, err
		}
		out.Tasks[name] = task
	}
	if err := checkGraph(out, cfg.maxCallDepth); err != nil {
		return out, err
	}
	return out, nil
}

// checkGraph rejects cycles and over-deep call chains for the whole definition,
// not only for the tasks a caller may run. It builds the plain node and edge
// lists the graph package works with and maps its errors onto the package
// sentinels.
func checkGraph(def Definition, maxDepth int) error {
	nodes := make([]string, 0, len(def.Tasks))
	edges := make(map[string][]string, len(def.Tasks))
	for name, task := range def.Tasks {
		nodes = append(nodes, name)
		edges[name] = taskEdges(task)
	}
	err := graph.Check(nodes, edges, maxDepth)
	if err == nil {
		return nil
	}
	var cycle *graph.CycleError
	if errors.As(err, &cycle) {
		return &RunError{Kind: ErrKindInvalidDefinition, Err: &cycleCause{path: cycle.Path}}
	}
	// The depth error keeps its original text and classification.
	return invalidDef("%s", err)
}

// taskEdges returns the static edges of a task: dependencies in declaration
// order, followed by task calls in step order. Both kinds participate in cycle
// detection even when a condition would skip them.
func taskEdges(task Task) []string {
	edges := append([]string(nil), task.Deps...)
	for _, step := range task.Steps {
		if step.Task != nil {
			edges = append(edges, step.Task.Name)
		}
	}
	return edges
}

// cycleCause reports a cycle and matches both the cycle and definition
// sentinels.
type cycleCause struct {
	path []string
}

func (e *cycleCause) Error() string {
	return "dependency cycle: " + strings.Join(e.path, " -> ")
}

// Is reports the error as both a cycle and a definition error.
func (e *cycleCause) Is(target error) bool {
	return target == ErrDependencyCycle || target == ErrInvalidDefinition
}

func invalidDef(format string, args ...any) error {
	return &RunError{Kind: ErrKindInvalidDefinition, Err: fmt.Errorf(format, args...)}
}

// validateData rejects caller supplied data the library cannot copy. The
// internal/data helper reports the offending path; the classification stays
// invalid_request.
func validateData(value any, path string) error {
	if err := data.Validate(value, path); err != nil {
		return &RunError{Kind: ErrKindInvalidRequest, Err: err}
	}
	return nil
}

func validateScriptFile(name string, file *ScriptFile, baseDir string) error {
	if name == "" {
		return invalidDef("script file has an empty name")
	}
	if file.Name != "" && file.Name != name {
		return invalidDef("script file %q has mismatching Name %q", name, file.Name)
	}
	file.Name = name
	if file.Path == "" {
		return invalidDef("script file %q has an empty path", name)
	}
	if file.Interpreter.Program == "" {
		return invalidDef("script file %q has no interpreter program", name)
	}
	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	file.Path = filepath.Clean(path)
	return nil
}

func validateTask(name string, task *Task, def Definition, cfg runnerConfig) error {
	if name == "" {
		return invalidDef("task has an empty name")
	}
	if task.Name != "" && task.Name != name {
		return invalidDef("task %q has mismatching Name %q", name, task.Name)
	}
	task.Name = name
	if task.Timeout < 0 {
		return invalidDef("task %s has a negative Timeout", name)
	}
	if !validPlatforms(task.Platform) {
		return invalidDef("task %s has an unknown platform entry in %v", name, task.Platform)
	}
	if err := validateData(task.Vars, "task "+name+".Vars"); err != nil {
		return err
	}
	if err := detectVarCycle(task.Vars); err != nil {
		return err
	}
	if err := validateDynamicVars("task "+name, task.DynamicVars, task.Vars); err != nil {
		return err
	}
	for _, dep := range task.Deps {
		if dep == "" {
			return invalidDef("task %s has an empty dependency", name)
		}
		if _, ok := def.Tasks[dep]; !ok {
			return invalidDef("task %s depends on unknown task %s", name, dep)
		}
	}
	if len(task.Deps) == 0 && len(task.Steps) == 0 && task.If == "" {
		return invalidDef("task %s has no dependencies, no steps and no condition", name)
	}
	seen := map[string]bool{}
	for i := range task.Steps {
		step := &task.Steps[i]
		if err := validateStep(name, i, step, def, cfg); err != nil {
			return err
		}
		key := step.Name
		if key == "" {
			key = fmt.Sprintf("#%d", i+1)
		}
		if seen[key] {
			return invalidDef("task %s has duplicate step name %q", name, key)
		}
		seen[key] = true
	}
	return nil
}

func validateStep(taskName string, index int, step *Step, def Definition, cfg runnerConfig) error {
	where := fmt.Sprintf("task %s step %s", taskName, stepLabel(*step, index))
	if step.Timeout < 0 {
		return invalidDef("%s has a negative Timeout", where)
	}
	if !validPlatforms(step.Platform) {
		return invalidDef("%s has an unknown platform entry in %v", where, step.Platform)
	}
	if err := validateData(step.Vars, where+".Vars"); err != nil {
		return err
	}
	if err := detectVarCycle(step.Vars); err != nil {
		return err
	}
	if err := validateDynamicVars(where, step.DynamicVars, step.Vars); err != nil {
		return err
	}
	switch step.actionCount() {
	case 0:
		return invalidDef("%s has no action; exactly one of exec, shell, file, task or host is required", where)
	case 1:
	default:
		return invalidDef("%s declares more than one action", where)
	}
	switch {
	case step.Exec != nil:
		if strings.TrimSpace(step.Exec.Program) == "" {
			return invalidDef("%s has an empty exec program", where)
		}
	case step.Shell != nil:
		name := normalizeShellName(step.Shell.Name)
		if name == "" {
			return invalidDef("%s does not name a shell", where)
		}
		if !shellNames[name] {
			return invalidDef("%s requests unsupported shell %q; supported: sh, bash, zsh, cmd, pwsh, powershell", where, step.Shell.Name)
		}
		if strings.TrimSpace(step.Shell.Script) == "" {
			return invalidDef("%s has an empty shell script", where)
		}
	case step.File != nil:
		if step.File.Name == "" {
			return invalidDef("%s does not name a registered script file", where)
		}
		if _, ok := def.Files[step.File.Name]; !ok {
			return invalidDef("%s references unknown script file %q", where, step.File.Name)
		}
	case step.Task != nil:
		if step.Task.Name == "" {
			return invalidDef("%s does not name a task", where)
		}
		if _, ok := def.Tasks[step.Task.Name]; !ok {
			return invalidDef("%s calls unknown task %q", where, step.Task.Name)
		}
	case step.Host != nil:
		if step.Host.Name == "" {
			return invalidDef("%s does not name a handler", where)
		}
		if _, ok := cfg.handlers[step.Host.Name]; !ok {
			return invalidDef("%s calls unregistered handler %q", where, step.Host.Name)
		}
		for i, arg := range step.Host.Args {
			if err := validateData(arg, fmt.Sprintf("%s.Host.Args[%d]", where, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func stepLabel(step Step, index int) string {
	if step.Name != "" {
		return step.Name
	}
	return fmt.Sprintf("#%d", index+1)
}

func validateDynamicVars(where string, dyn map[string]DynamicVar, static map[string]any) error {
	names := make([]string, 0, len(dyn))
	for name := range dyn {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		item := dyn[name]
		if name == "" {
			return invalidDef("%s has a dynamic variable with an empty name", where)
		}
		if _, clash := static[name]; clash {
			return invalidDef("%s declares %q as both a static and a dynamic variable", where, name)
		}
		switch item.kind() {
		case "":
			return invalidDef("%s dynamic variable %q has no action", where, name)
		case "exec":
			if strings.TrimSpace(item.Exec.Program) == "" {
				return invalidDef("%s dynamic variable %q has an empty program", where, name)
			}
		case "shell":
			shell := normalizeShellName(item.Shell.Name)
			if !shellNames[shell] {
				return invalidDef("%s dynamic variable %q requests unsupported shell %q", where, name, item.Shell.Name)
			}
			if item.Shell.Script == "" {
				return invalidDef("%s dynamic variable %q has an empty script", where, name)
			}
		case "file":
			if item.File.Name == "" {
				return invalidDef("%s dynamic variable %q does not name a script file", where, name)
			}
		}
	}
	return nil
}

func validPlatforms(list []string) bool {
	for _, item := range list {
		if item == "" {
			return false
		}
		for _, r := range item {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				continue
			}
			return false
		}
	}
	return true
}
