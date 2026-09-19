package kscript

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }

// errDeferred marks a value that only exists at run time. Inspect reports it as
// deferred instead of executing anything.
var errDeferred = errors.New("kscript: value requires execution")

// maxDataDepth bounds nesting while validating caller supplied data.
const maxDataDepth = 32

// validateData accepts only copyable data values: scalars, slices of copyable
// data and maps with string keys. Functions, channels, pointers, structs with
// side effects and cyclic data are rejected.
func validateData(value any, path string) error {
	return validateDataDepth(value, path, 0)
}

func validateDataDepth(value any, path string, depth int) error {
	if depth > maxDataDepth {
		return &RunError{Kind: ErrKindInvalidRequest, Err: errf("%s is nested too deeply", path)}
	}
	switch v := value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return nil
	case []any:
		for i, item := range v {
			if err := validateDataDepth(item, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
		return nil
	case []string:
		return nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "" {
				return &RunError{Kind: ErrKindInvalidRequest, Err: errf("%s contains an empty key", path)}
			}
			if err := validateDataDepth(v[key], path+"."+key, depth+1); err != nil {
				return err
			}
		}
		return nil
	case map[string]string:
		for key := range v {
			if key == "" {
				return &RunError{Kind: ErrKindInvalidRequest, Err: errf("%s contains an empty key", path)}
			}
		}
		return nil
	default:
		return &RunError{Kind: ErrKindInvalidRequest, Err: errf("%s has unsupported type %s: only scalars, slices and string-keyed maps are allowed", path, reflect.TypeOf(value))}
	}
}

// cloneData deep copies a copyable data value.
func cloneData(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneData(item)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneData(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	default:
		return value
	}
}

func cloneDataMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneData(value)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// mergeDataMaps returns a new map with later maps overriding earlier ones.
func mergeDataMaps(maps ...map[string]any) map[string]any {
	total := 0
	for _, m := range maps {
		total += len(m)
	}
	if total == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, total)
	for _, m := range maps {
		for key, value := range m {
			out[key] = value
		}
	}
	return out
}

// envKey normalizes an environment variable name for lookup. Windows treats
// environment names case-insensitively.
func envKey(name string) string {
	if isWindows {
		return strings.ToUpper(name)
	}
	return name
}

// envList flattens an environment map into the KEY=VALUE slice os/exec expects,
// sorted for deterministic results.
func envList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortStrings(values []string) { sort.Strings(values) }

func sortedDataKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
