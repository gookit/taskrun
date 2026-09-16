package formats

import (
	"fmt"
	"github.com/gookit/kscript"
)

// LegacyMap converts the old kscript map shape into a Definition.
// Kite-specific alias, extension and plugin routing remains outside this package.
func LegacyMap(scripts map[string]any, baseDir string) (kscript.Definition, error) {
	def := kscript.Definition{Version: 1, BaseDir: baseDir, Vars: map[string]any{}, Tasks: map[string]kscript.Task{}}
	for name, raw := range scripts {
		if name == "__settings" { continue }
		task := kscript.Task{Name: name}
		switch value := raw.(type) {
		case string:
			task.Steps = []kscript.Step{{Exec: &kscript.ExecSpec{Program: value}}}
		case []string:
			for _, command := range value { task.Steps = append(task.Steps, kscript.Step{Exec: &kscript.ExecSpec{Program: command}}) }
		case []any:
			for _, item := range value { command, ok := item.(string); if !ok { return def, fmt.Errorf("legacy task %s: command must be string", name) }; task.Steps = append(task.Steps, kscript.Step{Exec: &kscript.ExecSpec{Program: command}}) }
		case map[string]any:
			if desc, ok := value["desc"].(string); ok { task.Desc = desc }
			if condition, ok := value["if"].(string); ok { task.If = condition }
			if deps, ok := value["deps"].([]any); ok { for _, item := range deps { dep, ok := item.(string); if !ok { return def, fmt.Errorf("legacy task %s: dep must be string", name) }; task.Deps = append(task.Deps, dep) } }
			if command, ok := value["run"].(string); ok { task.Steps = []kscript.Step{{Exec: &kscript.ExecSpec{Program: command}}) }
		default:
			return def, fmt.Errorf("legacy task %s: unsupported value %T", name, raw)
		}
		def.Tasks[name] = task
	}
	return def, nil
}
