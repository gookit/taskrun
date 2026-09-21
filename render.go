package taskrun

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// renderVars is the read-only view available to template interpolation and
// condition evaluation. It never exposes host objects with methods.
type renderVars struct {
	Vars map[string]any
	Env  map[string]string
	Args []string
	Host map[string]any
	Run  map[string]any
	// Dynamic resolves a declared dynamic variable on demand. Inspect leaves it
	// nil so that no command runs; a lookup then reports errDeferred.
	Dynamic func(name string) (any, bool, error)
	// DeferredNames lists the dynamic variable names declared for this level.
	DeferredNames []string
}

var templatePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\.([^{}]*)\}`)

// renderTemplate performs a single-pass interpolation of explicitly namespaced
// templates. "$${" escapes a literal "${". Unknown references are errors.
func renderTemplate(source string, rv renderVars) (string, error) {
	if !strings.Contains(source, "${") {
		return source, nil
	}
	var b strings.Builder
	b.Grow(len(source))
	for i := 0; i < len(source); {
		if source[i] == '$' {
			if strings.HasPrefix(source[i:], "$${") {
				b.WriteString("${")
				i += 3
				continue
			}
			if loc := templatePattern.FindStringSubmatchIndex(source[i:]); loc != nil && loc[0] == 0 {
				namespace := source[i+loc[2] : i+loc[3]]
				name := source[i+loc[4] : i+loc[5]]
				value, err := lookupTemplate(namespace, name, rv)
				if err != nil {
					return "", err
				}
				b.WriteString(value)
				i += loc[1]
				continue
			}
		}
		b.WriteByte(source[i])
		i++
	}
	return b.String(), nil
}

func lookupTemplate(namespace, name string, rv renderVars) (string, error) {
	switch namespace {
	case "vars":
		if value, ok := lookupPath(rv.Vars, name); ok {
			return renderScalar("vars."+name, value)
		}
		if rv.Dynamic != nil {
			// Only a top-level name can be produced by a dynamic variable.
			top := name
			if index := strings.IndexByte(top, '.'); index >= 0 {
				top = top[:index]
			}
			value, found, err := rv.Dynamic(top)
			if err != nil {
				return "", err
			}
			if found {
				merged := mergeDataMaps(rv.Vars, map[string]any{top: value})
				if resolved, ok := lookupPath(merged, name); ok {
					return renderScalar("vars."+name, resolved)
				}
			}
		}
		return "", deferredOrInvalid("variable", name)
	case "env":
		if value, ok := lookupEnv(rv.Env, name); ok {
			return value, nil
		}
		return "", invalidDef("unknown environment reference ${env.%s}", name)
	case "args":
		index := 0
		if _, err := fmt.Sscanf(name, "%d", &index); err != nil || index < 1 || fmt.Sprint(index) != strings.TrimSpace(name) {
			return "", invalidDef("${args.%s} is not a 1-based argument index", name)
		}
		if index > len(rv.Args) {
			return "", &RunError{Kind: ErrKindInvalidRequest, Err: errf("${args.%d} refers to a missing argument; %d given", index, len(rv.Args))}
		}
		return rv.Args[index-1], nil
	case "host":
		if value, ok := lookupPath(rv.Host, name); ok {
			return renderScalar("host."+name, value)
		}
		return "", invalidDef("unknown host reference ${host.%s}", name)
	case "run":
		if value, ok := lookupPath(rv.Run, name); ok {
			return renderScalar("run."+name, value)
		}
		return "", invalidDef("unknown run reference ${run.%s}", name)
	default:
		return "", invalidDef("unknown template namespace ${%s.%s}; use vars, env, args, host or run", namespace, name)
	}
}

// lookupPath resolves a possibly dotted name such as "time.datetime" through
// nested string-keyed maps. It never calls host methods.
func lookupPath(root map[string]any, name string) (any, bool) {
	if root == nil {
		return nil, false
	}
	if value, ok := root[name]; ok {
		return value, true
	}
	if !strings.Contains(name, ".") {
		return nil, false
	}
	segments := strings.Split(name, ".")
	var current any = root
	for _, segment := range segments {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := asMap[segment]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func deferredOrInvalid(kind, name string) error {
	return invalidDef("unknown %s %q", kind, name)
}

func renderScalar(label string, value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", invalidDef("%s is null and cannot be rendered", label)
	case string:
		return v, nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v), nil
	default:
		return "", invalidDef("%s has non-scalar type %T and cannot be interpolated", label, value)
	}
}

// lookupEnv resolves an environment name honoring Windows case-insensitivity.
func lookupEnv(env map[string]string, name string) (string, bool) {
	if value, ok := env[name]; ok {
		return value, true
	}
	if !isWindows {
		return "", false
	}
	want := envKey(name)
	for key, value := range env {
		if envKey(key) == want {
			return value, true
		}
	}
	return "", false
}

// varRefs returns the variable names interpolated through ${vars.*}.
func varRefs(value string) []string {
	matches := templatePattern.FindAllStringSubmatch(value, -1)
	var out []string
	for _, match := range matches {
		if match[1] == "vars" {
			out = append(out, match[2])
		}
	}
	return out
}

// detectVarCycle rejects static variable cycles. Only same-level references can
// form a cycle, so this check is decidable at New, before any request runs.
func detectVarCycle(level map[string]any) error {
	if len(level) == 0 {
		return nil
	}
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(level))
	var visit func(key string, path []string) error
	visit = func(key string, path []string) error {
		switch state[key] {
		case done:
			return nil
		case visiting:
			return invalidDef("variable cycle: %s", strings.Join(append(path, key), " -> "))
		}
		state[key] = visiting
		if text, ok := level[key].(string); ok {
			for _, ref := range varRefs(text) {
				if _, sameLevel := level[ref]; !sameLevel {
					continue
				}
				if err := visit(ref, append(path, key)); err != nil {
					return err
				}
			}
		}
		state[key] = done
		return nil
	}
	for _, key := range sortedDataKeys(level) {
		if err := visit(key, nil); err != nil {
			return err
		}
	}
	return nil
}

// resolveVarLevel evaluates one level of static variables in dependency order.
// Unresolved references and cycles are definition errors.
func resolveVarLevel(level map[string]any, base renderVars) (map[string]any, error) {
	if len(level) == 0 {
		return map[string]any{}, nil
	}
	keys := make([]string, 0, len(level))
	for key := range level {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(level))
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(level))
	var resolve func(key string, path []string) error
	resolve = func(key string, path []string) error {
		switch state[key] {
		case done:
			return nil
		case visiting:
			return invalidDef("variable cycle: %s", strings.Join(append(path, key), " -> "))
		}
		state[key] = visiting
		value := level[key]
		if text, ok := value.(string); ok && strings.Contains(text, "${") {
			view := base
			view.Vars = mergeDataMaps(base.Vars, out)
			for _, ref := range varRefs(text) {
				if _, sameLevel := level[ref]; !sameLevel {
					continue
				}
				if err := resolve(ref, append(path, key)); err != nil {
					return err
				}
			}
			view.Vars = mergeDataMaps(base.Vars, out)
			rendered, err := renderTemplate(text, view)
			if err != nil {
				return err
			}
			out[key] = rendered
		} else {
			out[key] = cloneData(value)
		}
		state[key] = done
		return nil
	}
	for _, key := range keys {
		if err := resolve(key, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}

var identifierCache = map[string]*regexp.Regexp{}

// referencesAny reports whether source mentions any of the identifiers as a
// standalone word. It is used to decide whether a condition needs a dynamic
// variable, which Inspect must not evaluate.
func referencesAny(source string, names []string) bool {
	for _, name := range names {
		if name == "" {
			continue
		}
		re, ok := identifierCache[name]
		if !ok {
			re = regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_]|$)`)
			identifierCache[name] = re
		}
		if re.MatchString(source) {
			return true
		}
	}
	return false
}
