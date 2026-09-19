package formats

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gookit/kscript"
)

// LegacyOptions configures the conversion of a historical Kite task map. The
// converter is the only place that understands Kite private syntax: command
// aliases, extensions, plugins and system command fallback stay in Kite.
type LegacyOptions struct {
	// Scripts is the legacy task map, including an optional "__settings" entry.
	Scripts map[string]any
	// Settings is an already extracted "__settings" map. It is merged over the
	// one found inside Scripts.
	Settings map[string]any
	// BaseDir is the absolute directory of the source configuration.
	BaseDir string
	// DefaultShell is the legacy runner TypeShell value, for example "sh".
	DefaultShell string
	// RuntimeVars lists the names the Kite adapter injects at run time
	// (ctx.Vars keys, gvs, paths, kite, time, workdir, dirname and so on).
	// Only these names are rewritten from legacy $name/${name} into the
	// namespaced ${vars.name} form; anything else is left untouched.
	RuntimeVars []string
	// Files are script files discovered from ScriptDirs.
	Files map[string]LegacyScriptFile
}

// LegacyScriptFile describes a script file discovered by the caller.
type LegacyScriptFile struct {
	// Path is the file path, absolute or relative to BaseDir.
	Path string
	// Ext is the file extension including the dot, for example ".go".
	Ext string
	// Bin is the interpreter from ExtToBinMap; it may contain arguments. When
	// empty the extension without the dot is used, matching the legacy
	// BinName default.
	Bin string
}

// LegacyResult carries the converted definition and the conversion warnings.
// A warning records behavior that the new model does not own, such as command
// aliases or plugin support, so the Kite adapter can report it instead of
// silently dropping semantics.
type LegacyResult struct {
	Definition kscript.Definition
	Warnings   []string
}

// LegacyMap converts a legacy script map with default options.
func LegacyMap(scripts map[string]any, baseDir string) (kscript.Definition, error) {
	result, err := LegacyDefinition(LegacyOptions{Scripts: scripts, BaseDir: baseDir})
	if err != nil {
		return kscript.Definition{}, err
	}
	return result.Definition, nil
}

// LegacyDefinition converts legacy Kite task definitions into the current
// model.
func LegacyDefinition(opts LegacyOptions) (LegacyResult, error) {
	if opts.BaseDir == "" || !filepath.IsAbs(opts.BaseDir) {
		return LegacyResult{}, fmt.Errorf("legacy convert: BaseDir %q must be an absolute path", opts.BaseDir)
	}
	if opts.Scripts == nil {
		opts.Scripts = map[string]any{}
	}
	result := LegacyResult{}
	warn := func(format string, args ...any) {
		result.Warnings = append(result.Warnings, fmt.Sprintf(format, args...))
	}
	def := kscript.Definition{
		Version: 1,
		BaseDir: opts.BaseDir,
		Vars:    map[string]any{},
		Env:     map[string]string{},
		Tasks:   map[string]kscript.Task{},
		Files:   map[string]kscript.ScriptFile{},
	}
	settings := map[string]any{}
	if raw, ok := opts.Scripts["__settings"]; ok {
		asMap, ok := raw.(map[string]any)
		if !ok {
			return result, fmt.Errorf("legacy convert: __settings must be an object, got %T", raw)
		}
		for key, value := range asMap {
			settings[key] = value
		}
	}
	for key, value := range opts.Settings {
		settings[key] = value
	}
	if err := applyLegacySettings(&def, settings, warn); err != nil {
		return result, err
	}
	known := legacyKnownNames(def, opts)

	names := make([]string, 0, len(opts.Scripts))
	for name := range opts.Scripts {
		if name == "__settings" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		task, err := legacyTask(name, opts.Scripts[name], opts, known, warn)
		if err != nil {
			return result, err
		}
		if _, clash := def.Tasks[name]; clash {
			return result, fmt.Errorf("legacy convert: duplicate task %q", name)
		}
		def.Tasks[name] = task
	}

	for name, file := range opts.Files {
		converted, err := legacyScriptFile(name, file, opts.BaseDir, warn)
		if err != nil {
			return result, err
		}
		def.Files[name] = converted
	}
	result.Definition = def
	return result, nil
}

// legacyKnownNames collects every name the legacy renderer could resolve, so
// template translation only rewrites references that really are variables.
func legacyKnownNames(def kscript.Definition, opts LegacyOptions) map[string]bool {
	known := map[string]bool{}
	for name := range def.Vars {
		known[name] = true
	}
	for _, name := range opts.RuntimeVars {
		known[name] = true
	}
	// Legacy built-in render variables.
	for _, name := range []string{"workdir", "dirname", "cur_dir", "time", "gvs", "paths", "kite", "@", "*"} {
		known[name] = true
	}
	return known
}

func applyLegacySettings(def *kscript.Definition, settings map[string]any, warn func(string, ...any)) error {
	if len(settings) == 0 {
		return nil
	}
	for key := range settings {
		switch key {
		case "vars", "groups", "default_group", "env", "env_path", "env_paths":
		default:
			warn("__settings.%s is not part of the new model and was ignored", key)
		}
	}
	groups := map[string]map[string]any{}
	if raw, ok := settings["groups"].(map[string]any); ok {
		for group, value := range raw {
			if valueMap, ok := value.(map[string]any); ok {
				groups[group] = valueMap
			}
		}
	}
	if raw, ok := settings["default_group"]; ok {
		group := fmt.Sprint(raw)
		if values, ok := groups[group]; ok {
			for key, value := range values {
				def.Vars[key] = value
			}
		}
	}
	if raw, ok := settings["vars"].(map[string]any); ok {
		for key, value := range raw {
			def.Vars[key] = value
		}
	}
	if raw, ok := settings["env"].(map[string]any); ok {
		for key, value := range raw {
			def.Env[key] = fmt.Sprint(value)
		}
	}
	for _, key := range []string{"env_path", "env_paths"} {
		if raw, ok := settings[key]; ok {
			paths, err := legacyStrings(raw)
			if err != nil {
				return fmt.Errorf("legacy convert: __settings.%s: %w", key, err)
			}
			def.EnvPaths = append(def.EnvPaths, paths...)
		}
	}
	return nil
}

func legacyTask(name string, raw any, opts LegacyOptions, known map[string]bool, warn func(string, ...any)) (kscript.Task, error) {
	task := kscript.Task{Name: name}
	switch value := raw.(type) {
	case string:
		steps, err := legacyCommands(name, value, opts.DefaultShell, known, warn)
		if err != nil {
			return task, err
		}
		task.Steps = steps
		return task, nil
	case []string:
		steps, err := legacyCommands(name, value, opts.DefaultShell, known, warn)
		if err != nil {
			return task, err
		}
		task.Steps = steps
		return task, nil
	case []any:
		steps, err := legacyCommands(name, value, opts.DefaultShell, known, warn)
		if err != nil {
			return task, err
		}
		task.Steps = steps
		return task, nil
	case map[string]any:
	default:
		return task, fmt.Errorf("legacy convert: task %q has unsupported value type %T", name, raw)
	}
	value := raw.(map[string]any)
	shell := opts.DefaultShell
	if typ, ok := legacyString(value, "type"); ok {
		shell = typ
	}
	if dir, ok := legacyStringOne(value, "dir", "workdir"); ok {
		task.Dir = translateLegacyTemplate(dir, known, warn, "task "+name+".dir")
	}
	if desc, ok := legacyStringOne(value, "desc", "description"); ok {
		task.Desc = desc
	}
	if condition, ok := legacyString(value, "if"); ok {
		// A legacy condition is an expr over bare variable names, so it is
		// passed through unchanged.
		task.If = condition
	}
	if deps, ok := legacyStringListOne(value, "deps", "depends"); ok {
		task.Deps = deps
	}
	if timeout, ok := legacyStringOne(value, "timeout", "cmd_timeout"); ok {
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			return task, fmt.Errorf("legacy convert: task %q timeout %q is invalid", name, timeout)
		}
		task.Timeout = duration
	}
	if env, ok := legacyStringMap(value, "env"); ok {
		task.Env = map[string]string{}
		for key, item := range env {
			task.Env[key] = translateLegacyTemplate(item, known, warn, "task "+name+".env."+key)
		}
	}
	if paths, ok := legacyStringListOne(value, "env_path", "env_paths"); ok {
		task.EnvPaths = paths
	}
	if vars, ok := legacyAnyMap(value, "vars"); ok {
		static := map[string]any{}
		dynamic := map[string]kscript.DynamicVar{}
		for key, item := range vars {
			text, isText := item.(string)
			if !isText {
				static[key] = item
				continue
			}
			if kind, command, ok := legacyDynamicVar(text); ok {
				spec, err := legacyDynamicSpec(kind, command, shell, known, warn, fmt.Sprintf("task %s.var %s", name, key))
				if err != nil {
					return task, err
				}
				dynamic[key] = spec
				continue
			}
			static[key] = translateLegacyTemplate(text, known, warn, fmt.Sprintf("task %s.vars.%s", name, key))
		}
		if len(static) > 0 {
			task.Vars = static
		}
		if len(dynamic) > 0 {
			task.DynamicVars = dynamic
		}
	}
	for _, key := range []string{"alias", "aliases", "args", "scope", "ext", "for", "silent", "output"} {
		if _, ok := value[key]; ok {
			warn("task %s: legacy field %q is not owned by the library and was skipped", name, key)
		}
	}
	for _, goos := range []string{"windows", "linux", "darwin"} {
		sub, ok := value[goos].(map[string]any)
		if !ok {
			continue
		}
		if goos != runtime.GOOS {
			warn("task %s: platform override for %s was ignored on %s", name, goos, runtime.GOOS)
			continue
		}
		if typ, ok := legacyString(sub, "type"); ok {
			shell = typ
		}
		if cmds, ok := legacyRunValue(sub); ok {
			steps, err := legacyCommands(name, cmds, shell, known, warn)
			if err != nil {
				return task, err
			}
			task.Steps = steps
			warn("task %s: applied the %s platform override at conversion time", name, goos)
			return task, nil
		}
	}
	cmds, ok := legacyRunValue(value)
	if !ok {
		return task, nil
	}
	steps, err := legacyCommands(name, cmds, shell, known, warn)
	if err != nil {
		return task, err
	}
	task.Steps = steps
	return task, nil
}

func legacyRunValue(value map[string]any) (any, bool) {
	for _, key := range []string{"run", "cmds", "cmd"} {
		if item, ok := value[key]; ok {
			return item, true
		}
	}
	return nil, false
}

func legacyCommands(taskName string, raw any, shell string, known map[string]bool, warn func(string, ...any)) ([]kscript.Step, error) {
	var items []any
	switch value := raw.(type) {
	case string:
		items = []any{value}
	case []string:
		for _, item := range value {
			items = append(items, item)
		}
	case []any:
		items = value
	default:
		return nil, fmt.Errorf("legacy convert: task %q commands must be a string or list, got %T", taskName, raw)
	}
	steps := make([]kscript.Step, 0, len(items))
	for index, item := range items {
		switch value := item.(type) {
		case string:
			step, err := legacyCommandStep(taskName, index, value, "", legacyCommandExtras{}, shell, known, warn)
			if err != nil {
				return nil, err
			}
			if step != nil {
				steps = append(steps, *step)
			}
		case map[string]any:
			commandShell := shell
			if typ, ok := legacyString(value, "type"); ok {
				commandShell = typ
			}
			name, _ := legacyString(value, "name")
			run, _ := legacyStringOne(value, "run", "cmd", "cmds")
			extra, err := legacyCommandExtrasOf(value, taskName, index, known, warn)
			if err != nil {
				return nil, err
			}
			taskRef, _ := legacyString(value, "task")
			if taskRef == "" && !strings.HasPrefix(strings.TrimSpace(run), "@task:") {
				step, err := legacyCommandStep(taskName, index, run, name, extra, commandShell, known, warn)
				if err != nil {
					return nil, err
				}
				if step != nil {
					steps = append(steps, *step)
				}
				continue
			}
			target := taskRef
			if strings.HasPrefix(strings.TrimSpace(run), "@task:") {
				target = strings.TrimSpace(run[len("@task:"):])
			}
			step := kscript.Step{Name: legacyStepName(name, index), Task: &kscript.TaskCall{Name: target, ForwardArgs: true}}
			applyLegacyExtras(&step, extra)
			steps = append(steps, step)
		default:
			return nil, fmt.Errorf("legacy convert: task %q command #%d has unsupported type %T", taskName, index, item)
		}
	}
	return steps, nil
}

// legacyCommandExtras carries the per-command settings the new step model owns.
type legacyCommandExtras struct {
	Vars        map[string]any
	DynamicVars map[string]kscript.DynamicVar
	Env         map[string]string
	Dir         string
	If          string
	Timeout     time.Duration
	IgnoreError bool
}

func legacyCommandExtrasOf(value map[string]any, taskName string, index int, known map[string]bool, warn func(string, ...any)) (legacyCommandExtras, error) {
	extras := legacyCommandExtras{}
	if env, ok := legacyStringMap(value, "env"); ok {
		extras.Env = map[string]string{}
		for key, item := range env {
			extras.Env[key] = translateLegacyTemplate(item, known, warn, fmt.Sprintf("task %s command #%d env.%s", taskName, index, key))
		}
	}
	if dir, ok := legacyStringOne(value, "workdir", "dir"); ok {
		extras.Dir = translateLegacyTemplate(dir, known, warn, fmt.Sprintf("task %s command #%d dir", taskName, index))
	}
	if condition, ok := legacyString(value, "if"); ok {
		// Conditions are expr programs over bare variable names.
		extras.If = condition
	}
	if timeout, ok := legacyString(value, "timeout"); ok {
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			return extras, fmt.Errorf("legacy convert: task %q command #%d timeout %q is invalid", taskName, index, timeout)
		}
		extras.Timeout = duration
	}
	if flag, ok := legacyBoolOne(value, "ignore_err", "safe_run"); ok {
		extras.IgnoreError = flag
	}
	if vars, ok := legacyAnyMap(value, "vars"); ok {
		static := map[string]any{}
		dynamic := map[string]kscript.DynamicVar{}
		for key, item := range vars {
			text, isText := item.(string)
			if !isText {
				static[key] = item
				continue
			}
			if kind, command, ok := legacyDynamicVar(text); ok {
				spec, err := legacyDynamicSpec(kind, command, "", known, warn, fmt.Sprintf("task %s command #%d var %s", taskName, index, key))
				if err != nil {
					return extras, err
				}
				dynamic[key] = spec
				continue
			}
			static[key] = translateLegacyTemplate(text, known, warn, fmt.Sprintf("task %s command #%d vars.%s", taskName, index, key))
		}
		if len(static) > 0 {
			extras.Vars = static
		}
		if len(dynamic) > 0 {
			extras.DynamicVars = dynamic
		}
	}
	for _, key := range []string{"silent", "fail_msg", "output", "result_type"} {
		if _, ok := value[key]; ok {
			warn("task %s command #%d: legacy field %q is not owned by the library and was skipped", taskName, index, key)
		}
	}
	return extras, nil
}

func applyLegacyExtras(step *kscript.Step, extras legacyCommandExtras) {
	if extras.Vars != nil {
		step.Vars = extras.Vars
	}
	if extras.DynamicVars != nil {
		step.DynamicVars = extras.DynamicVars
	}
	if extras.Env != nil {
		step.Env = extras.Env
	}
	if extras.Dir != "" {
		step.Dir = extras.Dir
	}
	if extras.If != "" {
		step.If = extras.If
	}
	if extras.Timeout > 0 {
		step.Timeout = extras.Timeout
	}
	if extras.IgnoreError {
		step.IgnoreError = true
	}
}

// legacyCommandStep converts one legacy command line. It returns nil for an
// empty command, matching the legacy behavior of skipping blank entries.
func legacyCommandStep(taskName string, index int, run, name string, extras legacyCommandExtras, shell string, known map[string]bool, warn func(string, ...any)) (*kscript.Step, error) {
	run = strings.TrimSpace(run)
	if run == "" {
		return nil, nil
	}
	label := legacyStepName(name, index)
	where := fmt.Sprintf("task %s command #%d", taskName, index)
	step := kscript.Step{Name: label}

	// @task:name refers to another task and keeps its own call semantics.
	if strings.HasPrefix(run, "@task:") {
		target := strings.TrimSpace(run[len("@task:"):])
		step.Task = &kscript.TaskCall{Name: target, ForwardArgs: true}
		applyLegacyExtras(&step, extras)
		return &step, nil
	}
	// @type: command selects an explicit backend, for example @sh: or @exec:.
	if strings.HasPrefix(run, "@") {
		if pos := strings.Index(run, ":"); pos > 1 {
			typ := run[1:pos]
			if legacyShellTypes[typ] {
				shell = typ
				run = strings.TrimSpace(run[pos+1:])
			}
		} else {
			// A bare @ means "safe and silent" execution.
			run = strings.TrimSpace(run[1:])
			step.IgnoreError = true
			warn("%s: a bare @ prefix is converted to ignore_error only; legacy silent mode is Kite output policy", where)
		}
	}
	translated := translateLegacyTemplate(run, known, warn, where)
	switch legacyShellName(shell) {
	case "":
		program, args, err := legacySplitCommandLine(translated)
		if err != nil {
			return nil, fmt.Errorf("legacy convert: %s: %w", where, err)
		}
		if program == "" {
			return nil, nil
		}
		step.Exec = &kscript.ExecSpec{Program: program, Args: args}
	case "exec":
		program, args, err := legacySplitCommandLine(translated)
		if err != nil {
			return nil, fmt.Errorf("legacy convert: %s: %w", where, err)
		}
		if program == "" {
			return nil, nil
		}
		step.Exec = &kscript.ExecSpec{Program: program, Args: args}
	default:
		step.Shell = &kscript.ShellSpec{Name: legacyShellName(shell), Script: translated}
	}
	applyLegacyExtras(&step, extras)
	return &step, nil
}

var legacyShellTypes = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "cmd": true, "pwsh": true, "exec": true,
}

func legacyShellName(shell string) string {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "", "exec":
		return strings.ToLower(strings.TrimSpace(shell))
	case "sh", "bash", "zsh", "cmd", "pwsh":
		return strings.ToLower(strings.TrimSpace(shell))
	default:
		return ""
	}
}

func legacyStepName(name string, index int) string {
	if name != "" {
		return name
	}
	return fmt.Sprintf("cmd%d", index)
}

// legacyDynamicVar reports whether a legacy variable value declares a dynamic
// command, for example "@sh: git rev-parse HEAD".
func legacyDynamicVar(value string) (kind, command string, ok bool) {
	if !strings.HasPrefix(value, "@") {
		return "", "", false
	}
	pos := strings.Index(value, ":")
	if pos <= 1 {
		return "", "", false
	}
	kind = value[1:pos]
	if !legacyShellTypes[kind] {
		return "", "", false
	}
	return kind, strings.TrimSpace(value[pos+1:]), true
}

func legacyDynamicSpec(kind, command, defaultShell string, known map[string]bool, warn func(string, ...any), where string) (kscript.DynamicVar, error) {
	if kind == "exec" {
		program, args, err := legacySplitCommandLine(translateLegacyTemplate(command, known, warn, where))
		if err != nil {
			return kscript.DynamicVar{}, fmt.Errorf("legacy convert: %s: %w", where, err)
		}
		return kscript.DynamicVar{Exec: &kscript.ExecSpec{Program: program, Args: args}}, nil
	}
	if kind == "" {
		kind = defaultShell
	}
	shell := legacyShellName(kind)
	if shell == "" {
		return kscript.DynamicVar{}, fmt.Errorf("legacy convert: %s: unsupported shell %q", where, kind)
	}
	return kscript.DynamicVar{Shell: &kscript.ShellSpec{Name: shell, Script: translateLegacyTemplate(command, known, warn, where)}}, nil
}

func legacyScriptFile(name string, file LegacyScriptFile, baseDir string, warn func(string, ...any)) (kscript.ScriptFile, error) {
	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	bin := strings.TrimSpace(file.Bin)
	if bin == "" {
		ext := file.Ext
		if ext == "" {
			ext = filepath.Ext(name)
		}
		bin = strings.TrimPrefix(ext, ".")
	}
	if bin == "" {
		return kscript.ScriptFile{}, fmt.Errorf("legacy convert: script file %q has no interpreter", name)
	}
	program, prefix, err := legacySplitCommandLine(bin)
	if err != nil {
		return kscript.ScriptFile{}, fmt.Errorf("legacy convert: script file %q interpreter %q: %w", name, bin, err)
	}
	return kscript.ScriptFile{
		Name:        name,
		Path:        path,
		Interpreter: kscript.Interpreter{Program: program, PrefixArgs: prefix},
	}, nil
}

// translateLegacyTemplate rewrites the legacy variable syntax into the
// namespaced form the library renders:
//
//	$name / ${name}      -> ${vars.name}  (only for known runtime names)
//	$1 / ${1}            -> ${args.1}
//	$@                   -> ${vars.@}
//	$*                   -> ${vars.*}
//
// Unknown names, $ENV_NAME lookups and shell constructs such as $$ are left
// untouched, matching the legacy renderer fallback.
func translateLegacyTemplate(text string, known map[string]bool, warn func(string, ...any), where string) string {
	if !strings.Contains(text, "$") {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		if text[i] != '$' {
			b.WriteByte(text[i])
			i++
			continue
		}
		// $$ and $! and $? stay untouched.
		if i+1 >= len(text) {
			b.WriteByte(text[i])
			i++
			continue
		}
		next := text[i+1]
		if next == '{' {
			end := strings.IndexByte(text[i+2:], '}')
			if end < 0 {
				b.WriteString(text[i:])
				break
			}
			inner := text[i+2 : i+2+end]
			b.WriteString(translateLegacyName(inner, known))
			i += 2 + end + 1
			continue
		}
		if next == '@' || next == '*' {
			b.WriteString("${vars." + string(next) + "}")
			i += 2
			continue
		}
		if next >= '0' && next <= '9' {
			j := i + 1
			for j < len(text) && text[j] >= '0' && text[j] <= '9' {
				j++
			}
			b.WriteString("${args." + text[i+1:j] + "}")
			i = j
			continue
		}
		if isIdentifierStart(next) {
			j := legacyPathEnd(text, i+1)
			name := text[i+1 : j]
			if known[topSegment(name)] {
				b.WriteString("${vars." + name + "}")
			} else {
				b.WriteString(text[i:j])
			}
			i = j
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String()
}

func translateLegacyName(inner string, known map[string]bool) string {
	switch {
	case strings.HasPrefix(inner, "vars."), strings.HasPrefix(inner, "env."),
		strings.HasPrefix(inner, "args."), strings.HasPrefix(inner, "host."),
		strings.HasPrefix(inner, "run."):
		return "${" + inner + "}"
	case inner == "@" || inner == "*":
		return "${vars." + inner + "}"
	case inner != "" && allDigits(inner):
		return "${args." + inner + "}"
	case known[topSegment(inner)]:
		return "${vars." + inner + "}"
	default:
		return "${" + inner + "}"
	}
}

// topSegment returns the first dotted segment of a legacy variable path.
func topSegment(name string) string {
	if index := strings.IndexByte(name, '.'); index >= 0 {
		return name[:index]
	}
	return name
}

// legacyPathEnd returns the end offset of a dotted legacy path starting at
// start, for example "gvs.var" or "time.datetime".
func legacyPathEnd(text string, start int) int {
	index := start
	for index < len(text) && isIdentifierPart(text[index]) {
		index++
	}
	for index < len(text) && text[index] == '.' && index+1 < len(text) && isIdentifierStart(text[index+1]) {
		index++
		for index < len(text) && isIdentifierPart(text[index]) {
			index++
		}
	}
	return index
}

func allDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return text != ""
}

func isIdentifierStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentifierPart(c byte) bool {
	return isIdentifierStart(c) || c >= '0' && c <= '9'
}

// legacySplitCommandLine splits a legacy command line into argv using shell
// style quoting. It is converter-only behavior: the library never splits an
// exec argument.
func legacySplitCommandLine(line string) (string, []string, error) {
	var (
		fields  []string
		current strings.Builder
		quote   byte
		escaped bool
		hasText bool
	)
	flush := func() {
		if hasText {
			fields = append(fields, current.String())
			current.Reset()
			hasText = false
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			current.WriteByte(c)
			hasText = true
			escaped = false
			continue
		}
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
				continue
			}
			current.WriteByte(c)
			hasText = true
		case c == '\'' || c == '"':
			quote = c
			hasText = true
		case c == '\\':
			escaped = true
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		default:
			current.WriteByte(c)
			hasText = true
		}
	}
	if escaped {
		current.WriteByte('\\')
	}
	if quote != 0 {
		return "", nil, fmt.Errorf("unterminated %q quote", string(quote))
	}
	flush()
	if len(fields) == 0 {
		return "", nil, nil
	}
	return fields[0], fields[1:], nil
}

// ---- small legacy accessors ----

func legacyString(value map[string]any, key string) (string, bool) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return "", false
	}
	text, ok := raw.(string)
	return text, ok
}

func legacyStringOne(value map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if text, ok := legacyString(value, key); ok {
			return text, true
		}
	}
	return "", false
}

func legacyBoolOne(value map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			if flag, ok := raw.(bool); ok {
				return flag, true
			}
		}
	}
	return false, false
}

func legacyStringMap(value map[string]any, key string) (map[string]string, bool) {
	raw, ok := value[key].(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(raw))
	for name, item := range raw {
		out[name] = fmt.Sprint(item)
	}
	return out, true
}

func legacyAnyMap(value map[string]any, key string) (map[string]any, bool) {
	raw, ok := value[key].(map[string]any)
	return raw, ok
}

func legacyStringListOne(value map[string]any, keys ...string) ([]string, bool) {
	for _, key := range keys {
		if raw, ok := value[key]; ok {
			list, err := legacyStrings(raw)
			if err == nil {
				return list, true
			}
		}
	}
	return nil, false
}

func legacyStrings(raw any) ([]string, error) {
	switch value := raw.(type) {
	case string:
		if value == "" {
			return nil, nil
		}
		parts := strings.Split(value, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out, nil
	case []string:
		return value, nil
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, fmt.Sprint(item))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a string or list, got %T", raw)
	}
}
