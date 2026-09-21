package formats

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gookit/taskrun"
)

// decodeError locates a decoding problem in its source file and field path.
type decodeError struct {
	source string
	path   string
	msg    string
}

func (e *decodeError) Error() string {
	if e.path == "" {
		return fmt.Sprintf("load %s: %s", e.source, e.msg)
	}
	return fmt.Sprintf("load %s: %s: %s", e.source, e.path, e.msg)
}

type decoder struct {
	source  string
	baseDir string
}

func (d *decoder) fail(path, format string, args ...any) error {
	return &decodeError{source: d.source, path: path, msg: fmt.Sprintf(format, args...)}
}

func (d *decoder) checkKeys(value map[string]any, path string, allowed ...string) error {
	for _, key := range sortedKeys(value) {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return d.fail(path, "unknown field %q; allowed fields: %s", key, strings.Join(allowed, ", "))
		}
	}
	return nil
}

func (d *decoder) getMap(value map[string]any, path, key string) (map[string]any, bool, error) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return nil, false, nil
	}
	asMap, ok := raw.(map[string]any)
	if !ok {
		return nil, false, d.fail(path, "%s must be an object, got %T", key, raw)
	}
	return asMap, true, nil
}

func (d *decoder) getString(value map[string]any, path, key string) (string, bool, error) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return "", false, nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", false, d.fail(path, "%s must be a string, got %T", key, raw)
	}
	return text, true, nil
}

func (d *decoder) getBool(value map[string]any, path, key string) (bool, bool, error) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return false, false, nil
	}
	flag, ok := raw.(bool)
	if !ok {
		return false, false, d.fail(path, "%s must be a boolean, got %T", key, raw)
	}
	return flag, true, nil
}

func (d *decoder) getList(value map[string]any, path, key string) ([]any, bool, error) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return nil, false, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, false, d.fail(path, "%s must be an array, got %T", key, raw)
	}
	return list, true, nil
}

func (d *decoder) getStringList(value map[string]any, path, key string) ([]string, bool, error) {
	list, ok, err := d.getList(value, path, key)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make([]string, 0, len(list))
	for i, item := range list {
		text, ok := item.(string)
		if !ok {
			return nil, false, d.fail(fmt.Sprintf("%s.%s[%d]", path, key, i), "must be a string, got %T", item)
		}
		if text == "" {
			return nil, false, d.fail(fmt.Sprintf("%s.%s[%d]", path, key, i), "must not be empty")
		}
		out = append(out, text)
	}
	return out, true, nil
}

func (d *decoder) getStringMap(value map[string]any, path, key string) (map[string]string, bool, error) {
	raw, ok, err := d.getMap(value, path, key)
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make(map[string]string, len(raw))
	for _, name := range sortedKeys(raw) {
		text, ok := raw[name].(string)
		if !ok {
			return nil, false, d.fail(path+"."+key+"."+name, "must be a string, got %T", raw[name])
		}
		out[name] = text
	}
	return out, true, nil
}

func (d *decoder) getDataMap(value map[string]any, path, key string) (map[string]any, bool, error) {
	raw, ok, err := d.getMap(value, path, key)
	if err != nil || !ok {
		return nil, ok, err
	}
	return raw, true, nil
}

// getTimeout accepts an explicit duration string such as "500ms" or "2s".
func (d *decoder) getTimeout(value map[string]any, path, key string) (time.Duration, bool, error) {
	raw, ok := value[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	text, isString := raw.(string)
	if !isString {
		return 0, false, d.fail(path, "%s must be a duration string such as \"500ms\" or \"2s\", got %T", key, raw)
	}
	duration, err := time.ParseDuration(strings.TrimSpace(text))
	if err != nil {
		return 0, false, d.fail(path, "%s %q is not a valid duration", key, text)
	}
	if duration < 0 {
		return 0, false, d.fail(path, "%s must not be negative", key)
	}
	return duration, true, nil
}

func decodeDefinition(raw map[string]any, source, baseDir, format string) (taskrun.Definition, error) {
	d := &decoder{source: source, baseDir: baseDir}
	def := taskrun.Definition{
		Version: 1,
		BaseDir: baseDir,
		Vars:    map[string]any{},
		Env:     map[string]string{},
		Tasks:   map[string]taskrun.Task{},
		Files:   map[string]taskrun.ScriptFile{},
		Sources: []taskrun.Source{{Name: source, BaseDir: baseDir, Format: format}},
	}
	if err := d.checkKeys(raw, "", "version", "vars", "env", "env_paths", "tasks", "files"); err != nil {
		return taskrun.Definition{}, err
	}
	if versionRaw, ok := raw["version"]; ok && versionRaw != nil {
		version, err := asInt(versionRaw)
		if err != nil {
			return taskrun.Definition{}, d.fail("", "version must be an integer: %v", err)
		}
		if version != 1 {
			return taskrun.Definition{}, d.fail("", "unsupported schema version %d; this library implements version 1", version)
		}
		def.Version = version
	}
	vars, ok, err := d.getDataMap(raw, "", "vars")
	if err != nil {
		return taskrun.Definition{}, err
	}
	if ok {
		def.Vars = vars
	}
	env, ok, err := d.getStringMap(raw, "", "env")
	if err != nil {
		return taskrun.Definition{}, err
	}
	if ok {
		def.Env = env
	}
	envPaths, ok, err := d.getStringList(raw, "", "env_paths")
	if err != nil {
		return taskrun.Definition{}, err
	}
	if ok {
		def.EnvPaths = envPaths
	}
	tasksRaw, ok, err := d.getMap(raw, "", "tasks")
	if err != nil {
		return taskrun.Definition{}, err
	}
	if !ok {
		return taskrun.Definition{}, d.fail("", "tasks is required")
	}
	for _, name := range sortedKeys(tasksRaw) {
		task, err := d.decodeTask(name, tasksRaw[name])
		if err != nil {
			return taskrun.Definition{}, err
		}
		def.Tasks[name] = task
	}
	filesRaw, ok, err := d.getMap(raw, "", "files")
	if err != nil {
		return taskrun.Definition{}, err
	}
	if ok {
		for _, name := range sortedKeys(filesRaw) {
			file, err := d.decodeFile(name, filesRaw[name])
			if err != nil {
				return taskrun.Definition{}, err
			}
			def.Files[name] = file
		}
	}
	return def, nil
}

func (d *decoder) decodeTask(name string, raw any) (taskrun.Task, error) {
	path := "tasks." + name
	value, ok := raw.(map[string]any)
	if !ok {
		return taskrun.Task{}, d.fail(path, "task must be an object, got %T", raw)
	}
	if name == "" {
		return taskrun.Task{}, d.fail(path, "task name must not be empty")
	}
	task := taskrun.Task{Name: name}
	if err := d.checkKeys(value, path, "desc", "if", "platform", "deps", "dir",
		"timeout", "clean_env", "env", "env_paths", "vars", "dynamic_vars", "steps"); err != nil {
		return taskrun.Task{}, err
	}
	if desc, ok, err := d.getString(value, path, "desc"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Desc = desc
	}
	if condition, ok, err := d.getString(value, path, "if"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.If = condition
	}
	if platforms, ok, err := d.getStringList(value, path, "platform"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Platform = platforms
	}
	if deps, ok, err := d.getStringList(value, path, "deps"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Deps = deps
	}
	if dir, ok, err := d.getString(value, path, "dir"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Dir = dir
	}
	if timeout, ok, err := d.getTimeout(value, path, "timeout"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Timeout = timeout
	}
	if clean, ok, err := d.getBool(value, path, "clean_env"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.CleanEnv = clean
	}
	if env, ok, err := d.getStringMap(value, path, "env"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Env = env
	}
	if paths, ok, err := d.getStringList(value, path, "env_paths"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.EnvPaths = paths
	}
	if vars, ok, err := d.getDataMap(value, path, "vars"); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.Vars = vars
	}
	if dyn, ok, err := d.decodeDynamicVars(value, path); err != nil {
		return taskrun.Task{}, err
	} else if ok {
		task.DynamicVars = dyn
	}
	stepsRaw, ok, err := d.getList(value, path, "steps")
	if err != nil {
		return taskrun.Task{}, err
	}
	for i, stepRaw := range stepsRaw {
		step, err := d.decodeStep(fmt.Sprintf("%s.steps[%d]", path, i), stepRaw)
		if err != nil {
			return taskrun.Task{}, err
		}
		if step.Name == "" {
			step.Name = fmt.Sprintf("#%d", i+1)
		}
		task.Steps = append(task.Steps, step)
	}
	_ = ok
	return task, nil
}

func (d *decoder) decodeStep(path string, raw any) (taskrun.Step, error) {
	value, ok := raw.(map[string]any)
	if !ok {
		return taskrun.Step{}, d.fail(path, "step must be an object, got %T", raw)
	}
	if err := d.checkKeys(value, path, "name", "if", "platform", "dir", "timeout", "ignore_error",
		"env", "env_paths", "vars", "dynamic_vars",
		"exec", "shell", "file", "task", "host"); err != nil {
		return taskrun.Step{}, err
	}
	step := taskrun.Step{}
	if name, ok, err := d.getString(value, path, "name"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.Name = name
	}
	if condition, ok, err := d.getString(value, path, "if"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.If = condition
	}
	if platforms, ok, err := d.getStringList(value, path, "platform"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.Platform = platforms
	}
	if dir, ok, err := d.getString(value, path, "dir"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.Dir = dir
	}
	if timeout, ok, err := d.getTimeout(value, path, "timeout"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.Timeout = timeout
	}
	if ignore, ok, err := d.getBool(value, path, "ignore_error"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.IgnoreError = ignore
	}
	if env, ok, err := d.getStringMap(value, path, "env"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.Env = env
	}
	if paths, ok, err := d.getStringList(value, path, "env_paths"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.EnvPaths = paths
	}
	if vars, ok, err := d.getDataMap(value, path, "vars"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.Vars = vars
	}
	if dyn, ok, err := d.decodeDynamicVars(value, path); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		step.DynamicVars = dyn
	}
	if spec, ok, err := d.getMap(value, path, "exec"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		exec, err := d.decodeExec(path+".exec", spec)
		if err != nil {
			return taskrun.Step{}, err
		}
		step.Exec = exec
	}
	if spec, ok, err := d.getMap(value, path, "shell"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		shell, err := d.decodeShell(path+".shell", spec)
		if err != nil {
			return taskrun.Step{}, err
		}
		step.Shell = shell
	}
	if spec, ok, err := d.getMap(value, path, "file"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		file, err := d.decodeFileRef(path+".file", spec)
		if err != nil {
			return taskrun.Step{}, err
		}
		step.File = file
	}
	if spec, ok, err := d.getMap(value, path, "task"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		call, err := d.decodeTaskCall(path+".task", spec)
		if err != nil {
			return taskrun.Step{}, err
		}
		step.Task = call
	}
	if spec, ok, err := d.getMap(value, path, "host"); err != nil {
		return taskrun.Step{}, err
	} else if ok {
		host, err := d.decodeHost(path+".host", spec)
		if err != nil {
			return taskrun.Step{}, err
		}
		step.Host = host
	}
	// YAML and TOML make it easy to declare two actions by accident, so reject
	// the mixture while decoding instead of at New.
	actions := 0
	for _, set := range []bool{step.Exec != nil, step.Shell != nil, step.File != nil, step.Task != nil, step.Host != nil} {
		if set {
			actions++
		}
	}
	switch actions {
	case 0:
		return taskrun.Step{}, d.fail(path, "step must declare exactly one of exec, shell, file, task or host")
	case 1:
	default:
		return taskrun.Step{}, d.fail(path, "step declares more than one action; exactly one of exec, shell, file, task or host is allowed")
	}
	return step, nil
}

func (d *decoder) decodeExec(path string, spec map[string]any) (*taskrun.ExecSpec, error) {
	if err := d.checkKeys(spec, path, "program", "args"); err != nil {
		return nil, err
	}
	program, ok, err := d.getString(spec, path, "program")
	if err != nil {
		return nil, err
	}
	if !ok || program == "" {
		return nil, d.fail(path, "program is required")
	}
	args, _, err := d.getStringList(spec, path, "args")
	if err != nil {
		return nil, err
	}
	return &taskrun.ExecSpec{Program: program, Args: args}, nil
}

func (d *decoder) decodeShell(path string, spec map[string]any) (*taskrun.ShellSpec, error) {
	if err := d.checkKeys(spec, path, "name", "script", "prefix_args"); err != nil {
		return nil, err
	}
	name, ok, err := d.getString(spec, path, "name")
	if err != nil {
		return nil, err
	}
	if !ok || name == "" {
		return nil, d.fail(path, "name is required; supported shells: sh, bash, zsh, cmd, pwsh, powershell")
	}
	script, ok, err := d.getString(spec, path, "script")
	if err != nil {
		return nil, err
	}
	if !ok || script == "" {
		return nil, d.fail(path, "script is required")
	}
	prefix, _, err := d.getStringList(spec, path, "prefix_args")
	if err != nil {
		return nil, err
	}
	return &taskrun.ShellSpec{Name: name, Script: script, PrefixArgs: prefix}, nil
}

func (d *decoder) decodeFileRef(path string, spec map[string]any) (*taskrun.FileSpec, error) {
	if err := d.checkKeys(spec, path, "name", "args"); err != nil {
		return nil, err
	}
	name, ok, err := d.getString(spec, path, "name")
	if err != nil {
		return nil, err
	}
	if !ok || name == "" {
		return nil, d.fail(path, "name is required and must reference the files section")
	}
	args, _, err := d.getStringList(spec, path, "args")
	if err != nil {
		return nil, err
	}
	return &taskrun.FileSpec{Name: name, Args: args}, nil
}

func (d *decoder) decodeTaskCall(path string, spec map[string]any) (*taskrun.TaskCall, error) {
	if err := d.checkKeys(spec, path, "name", "args", "forward_args"); err != nil {
		return nil, err
	}
	name, ok, err := d.getString(spec, path, "name")
	if err != nil {
		return nil, err
	}
	if !ok || name == "" {
		return nil, d.fail(path, "name is required")
	}
	call := &taskrun.TaskCall{Name: name}
	// A present args key replaces the inherited arguments even when empty.
	if raw, present := spec["args"]; present {
		args, _, err := d.getStringList(spec, path, "args")
		if err != nil {
			return nil, err
		}
		if args == nil {
			args = []string{}
		}
		call.Args = args
		_ = raw
	}
	forward, ok, err := d.getBool(spec, path, "forward_args")
	if err != nil {
		return nil, err
	}
	if ok {
		call.ForwardArgs = forward
	}
	return call, nil
}

func (d *decoder) decodeHost(path string, spec map[string]any) (*taskrun.HostSpec, error) {
	if err := d.checkKeys(spec, path, "name", "args"); err != nil {
		return nil, err
	}
	name, ok, err := d.getString(spec, path, "name")
	if err != nil {
		return nil, err
	}
	if !ok || name == "" {
		return nil, d.fail(path, "name is required")
	}
	host := &taskrun.HostSpec{Name: name}
	if args, ok, err := d.getList(spec, path, "args"); err != nil {
		return nil, err
	} else if ok {
		host.Args = args
	}
	return host, nil
}

func (d *decoder) decodeDynamicVars(value map[string]any, path string) (map[string]taskrun.DynamicVar, bool, error) {
	raw, ok, err := d.getMap(value, path, "dynamic_vars")
	if err != nil || !ok {
		return nil, ok, err
	}
	out := make(map[string]taskrun.DynamicVar, len(raw))
	for _, name := range sortedKeys(raw) {
		itemPath := path + ".dynamic_vars." + name
		spec, ok := raw[name].(map[string]any)
		if !ok {
			return nil, false, d.fail(itemPath, "dynamic variable must be an object, got %T", raw[name])
		}
		if err := d.checkKeys(spec, itemPath, "exec", "shell", "file"); err != nil {
			return nil, false, err
		}
		item := taskrun.DynamicVar{}
		if exec, ok, err := d.getMap(spec, itemPath, "exec"); err != nil {
			return nil, false, err
		} else if ok {
			decoded, err := d.decodeExec(itemPath+".exec", exec)
			if err != nil {
				return nil, false, err
			}
			item.Exec = decoded
		}
		if shell, ok, err := d.getMap(spec, itemPath, "shell"); err != nil {
			return nil, false, err
		} else if ok {
			decoded, err := d.decodeShell(itemPath+".shell", shell)
			if err != nil {
				return nil, false, err
			}
			item.Shell = decoded
		}
		if file, ok, err := d.getMap(spec, itemPath, "file"); err != nil {
			return nil, false, err
		} else if ok {
			decoded, err := d.decodeFileRef(itemPath+".file", file)
			if err != nil {
				return nil, false, err
			}
			item.File = decoded
		}
		out[name] = item
		if dynamicKindCount(item) != 1 {
			return nil, false, d.fail(itemPath, "dynamic variable must declare exactly one of exec, shell or file")
		}
	}
	return out, true, nil
}

func (d *decoder) decodeFile(name string, raw any) (taskrun.ScriptFile, error) {
	path := "files." + name
	value, ok := raw.(map[string]any)
	if !ok {
		return taskrun.ScriptFile{}, d.fail(path, "script file must be an object, got %T", raw)
	}
	if err := d.checkKeys(value, path, "path", "args", "dir", "env", "interpreter"); err != nil {
		return taskrun.ScriptFile{}, err
	}
	file := taskrun.ScriptFile{Name: name}
	filePath, ok, err := d.getString(value, path, "path")
	if err != nil {
		return taskrun.ScriptFile{}, err
	}
	if !ok || filePath == "" {
		return taskrun.ScriptFile{}, d.fail(path, "path is required")
	}
	file.Path = filePath
	// A script path must stay inside the source base directory; escaping paths
	// would let configuration reach arbitrary files. The path is fixed to an
	// absolute value at load time so a later request directory cannot move it.
	resolved := filePath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(d.baseDir, resolved)
	}
	resolved = filepath.Clean(resolved)
	if !withinBase(d.baseDir, resolved) {
		return taskrun.ScriptFile{}, d.fail(path, "path %q escapes the source base directory %s", filePath, d.baseDir)
	}
	file.Path = resolved
	if args, ok, err := d.getStringList(value, path, "args"); err != nil {
		return taskrun.ScriptFile{}, err
	} else if ok {
		file.Args = args
	}
	if dir, ok, err := d.getString(value, path, "dir"); err != nil {
		return taskrun.ScriptFile{}, err
	} else if ok {
		file.Dir = dir
	}
	if env, ok, err := d.getStringMap(value, path, "env"); err != nil {
		return taskrun.ScriptFile{}, err
	} else if ok {
		file.Env = env
	}
	interpreter, ok, err := d.getMap(value, path, "interpreter")
	if err != nil {
		return taskrun.ScriptFile{}, err
	}
	if !ok {
		return taskrun.ScriptFile{}, d.fail(path, "interpreter is required")
	}
	if err := d.checkKeys(interpreter, path+".interpreter", "program", "prefix_args"); err != nil {
		return taskrun.ScriptFile{}, err
	}
	program, ok, err := d.getString(interpreter, path+".interpreter", "program")
	if err != nil {
		return taskrun.ScriptFile{}, err
	}
	if !ok || program == "" {
		return taskrun.ScriptFile{}, d.fail(path+".interpreter", "program is required, for example go with prefix_args [run]")
	}
	prefix, _, err := d.getStringList(interpreter, path+".interpreter", "prefix_args")
	if err != nil {
		return taskrun.ScriptFile{}, err
	}
	file.Interpreter = taskrun.Interpreter{Program: program, PrefixArgs: prefix}
	return file, nil
}

// dynamicKindCount counts how many actions a dynamic variable declares.
func dynamicKindCount(item taskrun.DynamicVar) int {
	count := 0
	for _, set := range []bool{item.Exec != nil, item.Shell != nil, item.File != nil} {
		if set {
			count++
		}
	}
	return count
}

// asInt accepts the numeric representations produced by the three decoders.
func asInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case uint64:
		return int(v), nil
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("not a number")
	}
}
