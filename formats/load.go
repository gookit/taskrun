package formats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"
	"github.com/gookit/kscript"
)

// Format names accepted by Load.
const (
	FormatJSON = "json"
	FormatYAML = "yaml"
	FormatTOML = "toml"
)

// Load decodes one document. baseDir must be an absolute directory: relative
// script paths resolve against it and the value is recorded as the source base
// directory.
func Load(ext string, reader io.Reader, source, baseDir string) (kscript.Definition, error) {
	format, err := formatOf(ext)
	if err != nil {
		return kscript.Definition{}, err
	}
	if !filepath.IsAbs(baseDir) {
		return kscript.Definition{}, fmt.Errorf("load %s: baseDir %q must be an absolute path", source, baseDir)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return kscript.Definition{}, fmt.Errorf("load %s: %w", source, err)
	}
	raw, err := decodeDocument(format, data)
	if err != nil {
		return kscript.Definition{}, fmt.Errorf("load %s: %w", source, err)
	}
	def, err := decodeDefinition(raw, source, baseDir, format)
	if err != nil {
		return kscript.Definition{}, err
	}
	return def, nil
}

// LoadFile decodes a file, using its extension to select the format. The
// directory of path becomes the absolute source base directory.
func LoadFile(path string) (kscript.Definition, error) {
	baseDir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return kscript.Definition{}, fmt.Errorf("load %s: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return kscript.Definition{}, fmt.Errorf("load %s: %w", path, err)
	}
	defer file.Close()
	return Load(filepath.Ext(path), file, path, baseDir)
}

// LoadFiles decodes and merges several sources in order. Task name conflicts
// are an error unless override is true, in which case the later definition
// replaces the whole task. Variables and environment defaults are always
// overridden by key.
func LoadFiles(paths []string, override bool) (kscript.Definition, error) {
	defs := make([]kscript.Definition, 0, len(paths))
	for _, path := range paths {
		def, err := LoadFile(path)
		if err != nil {
			return kscript.Definition{}, err
		}
		defs = append(defs, def)
	}
	return Merge(defs, override)
}

// Merge combines decoded definitions in input order.
func Merge(defs []kscript.Definition, override bool) (kscript.Definition, error) {
	if len(defs) == 0 {
		return kscript.Definition{}, fmt.Errorf("merge: no definitions")
	}
	out := kscript.Definition{
		Version: 1,
		BaseDir: defs[0].BaseDir,
		Vars:    map[string]any{},
		Env:     map[string]string{},
		Tasks:   map[string]kscript.Task{},
		Files:   map[string]kscript.ScriptFile{},
	}
	origins := map[string]string{}
	for _, def := range defs {
		out.Sources = append(out.Sources, def.Sources...)
		for key, value := range def.Vars {
			out.Vars[key] = value
		}
		for key, value := range def.Env {
			out.Env[key] = value
		}
		for _, path := range def.EnvPaths {
			if !containsString(out.EnvPaths, path) {
				out.EnvPaths = append(out.EnvPaths, path)
			}
		}
		for name, task := range def.Tasks {
			if previous, exists := origins[name]; exists && !override {
				return kscript.Definition{}, fmt.Errorf("merge: task %q is defined by both %s and %s; enable override to replace it",
					name, previous, sourceLabel(def))
			}
			origins[name] = sourceLabel(def)
			out.Tasks[name] = task
		}
		for name, file := range def.Files {
			if previous, exists := origins["file:"+name]; exists && !override {
				return kscript.Definition{}, fmt.Errorf("merge: script file %q is defined by both %s and %s; enable override to replace it",
					name, previous, sourceLabel(def))
			}
			origins["file:"+name] = sourceLabel(def)
			out.Files[name] = file
		}
	}
	return out, nil
}

func sourceLabel(def kscript.Definition) string {
	if len(def.Sources) == 0 {
		return "<unknown>"
	}
	return def.Sources[0].Name
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func formatOf(ext string) (string, error) {
	switch strings.ToLower(ext) {
	case ".json", "json":
		return FormatJSON, nil
	case ".yaml", ".yml", "yaml", "yml":
		return FormatYAML, nil
	case ".toml", "toml":
		return FormatTOML, nil
	default:
		return "", fmt.Errorf("unsupported format %q: use .json, .yaml, .yml or .toml", ext)
	}
}

func decodeDocument(format string, data []byte) (map[string]any, error) {
	var raw map[string]any
	switch format {
	case FormatJSON:
		if err := checkJSONDuplicateKeys(data); err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
	case FormatYAML:
		// Duplicate mapping keys are a syntax error unless explicitly allowed.
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
	case FormatTOML:
		if err := toml.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
	}
	if raw == nil {
		return nil, fmt.Errorf("document must be an object")
	}
	return normalizeMap(raw), nil
}

// normalizeMap converts YAML/TOML specific map and slice types into the shapes
// the decoder expects, so all three formats produce an identical model.
func normalizeMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = normalizeValue(value)
	}
	return out
}

func normalizeValue(value any) any {
	switch v := value.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case map[string]any:
		return normalizeMap(v)
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = normalizeValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	default:
		if normalized, ok := normalizeNumber(value); ok {
			return normalized
		}
		// TOML and YAML may hand back concrete slice and map types; normalize
		// them so all three formats share one model.
		return normalizeReflect(value)
	}
}

// normalizeNumber maps every integer kind to int64 and every float kind to
// float64 so the three decoders produce identical models.
func normalizeNumber(value any) (any, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return nil, false
	}
}

func normalizeReflect(value any) any {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return value
		}
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = normalizeValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return value
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = normalizeValue(iter.Value().Interface())
		}
		return out
	default:
		return value
	}
}

// checkJSONDuplicateKeys rejects documents with duplicate object keys, which
// encoding/json would silently collapse.
func checkJSONDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key must be a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate key %q", key)
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

// sortedKeys returns object keys in a stable order for deterministic messages.
func sortedKeys(in map[string]any) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// withinBase reports whether path stays inside base.
func withinBase(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
