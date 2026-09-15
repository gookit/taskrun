package formats

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"
	"github.com/gookit/kscript"
)

func LoadFile(path string) (kscript.Definition, error) {
	f, err := os.Open(path)
	if err != nil {
		return kscript.Definition{}, err
	}
	defer f.Close()
	return Load(filepath.Ext(path), f, path, filepath.Dir(path))
}

func Load(ext string, r io.Reader, source, baseDir string) (kscript.Definition, error) {
	var raw map[string]any
	var err error
	switch strings.ToLower(ext) {
	case ".json":
		err = json.NewDecoder(r).Decode(&raw)
	case ".yaml", ".yml":
		b, e := io.ReadAll(r)
		err = yaml.Unmarshal(b, &raw)
		_ = e
	case ".toml":
		b, e := io.ReadAll(r)
		err = toml.Unmarshal(b, &raw)
		_ = e
	default:
		return kscript.Definition{}, fmt.Errorf("unsupported format %q", ext)
	}
	if err != nil {
		return kscript.Definition{}, fmt.Errorf("load %s: %w", source, err)
	}
	return decode(raw, baseDir)
}

func decode(raw map[string]any, baseDir string) (kscript.Definition, error) {
	if raw == nil {
		return kscript.Definition{}, fmt.Errorf("definition must be an object")
	}
	d := kscript.Definition{Version: 1, BaseDir: baseDir, Vars: map[string]any{}, Tasks: map[string]kscript.Task{}}
	if v, ok := raw["version"].(float64); ok && int(v) != 1 {
		return d, fmt.Errorf("unsupported schema version %d", int(v))
	}
	if _, ok := raw["tasks"]; !ok {
		return d, fmt.Errorf("tasks is required")
	}
	for key := range raw { if key != "version" && key != "vars" && key != "tasks" && key != "files" { return d, fmt.Errorf("unknown definition field %q", key) } }
	if v, ok := raw["version"].(float64); ok {
		d.Version = int(v)
	}
	if v, ok := raw["vars"].(map[string]any); ok {
		d.Vars = v
	}
	tasks, ok := raw["tasks"].(map[string]any)
	if !ok {
		return d, nil
	}
	for name, value := range tasks {
		task := kscript.Task{Name: name}
		switch value := value.(type) {
		case string:
			task.Steps = []kscript.Step{{Exec: &kscript.ExecSpec{Program: value}}}
		case map[string]any:
			for key := range value { if key != "desc" && key != "if" && key != "run" && key != "deps" { return d, fmt.Errorf("task %s: unknown field %q", name, key) } }
			if desc, ok := value["desc"].(string); ok {
				task.Desc = desc
			}
			if condition, ok := value["if"].(string); ok {
				task.If = condition
			}
			if run, ok := value["run"].(string); ok {
				task.Steps = []kscript.Step{{Exec: &kscript.ExecSpec{Program: run}}}
			} else if _, present := value["run"]; present { return d, fmt.Errorf("task %s: run must be string", name) }
			}
		default:
			return d, fmt.Errorf("task %s must be string or object", name)
		}
		d.Tasks[name] = task
	}
	return d, nil
}
