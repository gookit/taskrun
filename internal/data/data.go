// Package data holds the copy, merge and validation helpers shared by the
// public package and its internal implementations. Everything here works on
// plain Go values: scalars, slices and string keyed maps.
package data

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

// MaxDepth bounds nesting while validating caller supplied data.
const MaxDepth = 32

// IsWindows reports the host platform. Windows compares environment names
// case-insensitively.
const IsWindows = runtime.GOOS == "windows"

// Validate accepts only copyable data values: scalars, slices of copyable data
// and maps with string keys. Functions, channels, pointers, structs with side
// effects and cyclic data are rejected. The returned error text names the
// offending path; the caller decides how to classify it.
func Validate(value any, path string) error {
	return validateDepth(value, path, 0)
}

func validateDepth(value any, path string, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("%s is nested too deeply", path)
	}
	switch v := value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return nil
	case []any:
		for i, item := range v {
			if err := validateDepth(item, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
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
				return fmt.Errorf("%s contains an empty key", path)
			}
			if err := validateDepth(v[key], path+"."+key, depth+1); err != nil {
				return err
			}
		}
		return nil
	case map[string]string:
		for key := range v {
			if key == "" {
				return fmt.Errorf("%s contains an empty key", path)
			}
		}
		return nil
	default:
		return fmt.Errorf("%s has unsupported type %s: only scalars, slices and string-keyed maps are allowed",
			path, reflect.TypeOf(value))
	}
}

// Clone deep copies a copyable data value.
func Clone(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = Clone(item)
		}
		return out
	case []string:
		return append([]string(nil), v...)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = Clone(item)
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

// CloneMap deep copies a map of data values.
func CloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = Clone(value)
	}
	return out
}

// CloneStringMap copies a string map.
func CloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// CloneStrings copies a string slice.
func CloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// Merge returns a new map with later maps overriding earlier ones.
func Merge(maps ...map[string]any) map[string]any {
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

// EnvKey normalizes an environment variable name for lookup.
func EnvKey(name string) string {
	if IsWindows {
		return strings.ToUpper(name)
	}
	return name
}

// EnvList flattens an environment map into the KEY=VALUE slice os/exec expects,
// sorted for deterministic results.
func EnvList(env map[string]string) []string {
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

// SortedKeys returns the keys of a string map in ascending order.
func SortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SortedDataKeys returns the keys of a data map in ascending order.
func SortedDataKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// SortStrings sorts a string slice in place.
func SortStrings(values []string) { sort.Strings(values) }
