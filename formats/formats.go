package formats

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	if !strings.EqualFold(ext, ".json") {
		return kscript.Definition{}, fmt.Errorf("format %q not implemented", ext)
	}
	var raw map[string]any
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return kscript.Definition{}, fmt.Errorf("load %s: %w", source, err)
	}
	return decode(raw, baseDir)
}

func decode(raw map[string]any, baseDir string) (kscript.Definition, error) {
	d := kscript.Definition{Version: 1, BaseDir: baseDir, Vars: map[string]any{}, Tasks: map[string]kscript.Task{}}
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
			if desc, ok := value["desc"].(string); ok {
				task.Desc = desc
			}
			if condition, ok := value["if"].(string); ok {
				task.If = condition
			}
			if run, ok := value["run"].(string); ok {
				task.Steps = []kscript.Step{{Exec: &kscript.ExecSpec{Program: run}}}
			}
		default:
			return d, fmt.Errorf("task %s must be string or object", name)
		}
		d.Tasks[name] = task
	}
	return d, nil
}
