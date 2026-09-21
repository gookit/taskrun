// Package render interpolates the library's namespaced templates and evaluates
// expr conditions against a read-only view. It never exposes host objects with
// methods and never runs anything: a value that only exists at run time is
// reported as deferred.
package render

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/gookit/taskrun/internal/data"
)

// Kind classifies a rendering failure so the caller can map it onto its own
// error kinds.
type Kind int

const (
	// KindInvalidDefinition reports a definition that cannot be rendered.
	KindInvalidDefinition Kind = iota
	// KindInvalidRequest reports a request that cannot be satisfied.
	KindInvalidRequest
)

// Error is a rendering failure. Msg is the complete user facing message.
type Error struct {
	Kind Kind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// ErrDeferred marks a value that only exists at run time. Inspect reports it as
// deferred instead of executing anything.
var ErrDeferred = errors.New("taskrun: value requires execution")

// Vars is the read-only view available to template interpolation and condition
// evaluation.
type Vars struct {
	Vars map[string]any
	Env  map[string]string
	Args []string
	Host map[string]any
	Run  map[string]any
	// Dynamic resolves a declared dynamic variable on demand. Inspect leaves it
	// nil so that no command runs; a lookup then reports ErrDeferred.
	Dynamic func(name string) (any, bool, error)
	// DeferredNames lists the dynamic variable names declared for this level.
	DeferredNames []string
}

var templatePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\.([^{}]*)\}`)

// invalid builds a definition error.
func invalid(format string, args ...any) error {
	return &Error{Kind: KindInvalidDefinition, Msg: fmt.Sprintf(format, args...)}
}

// Render performs a single-pass interpolation of explicitly namespaced
// templates. "$${" escapes a literal "${". Unknown references are errors.
func Render(source string, rv Vars) (string, error) {
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

func lookupTemplate(namespace, name string, rv Vars) (string, error) {
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
				merged := data.Merge(rv.Vars, map[string]any{top: value})
				if resolved, ok := lookupPath(merged, name); ok {
					return renderScalar("vars."+name, resolved)
				}
			}
		}
		return "", invalid("unknown variable %q", name)
	case "env":
		if value, ok := LookupEnv(rv.Env, name); ok {
			return value, nil
		}
		return "", invalid("unknown environment reference ${env.%s}", name)
	case "args":
		index := 0
		if _, err := fmt.Sscanf(name, "%d", &index); err != nil || index < 1 || fmt.Sprint(index) != strings.TrimSpace(name) {
			return "", invalid("${args.%s} is not a 1-based argument index", name)
		}
		if index > len(rv.Args) {
			return "", &Error{Kind: KindInvalidRequest,
				Msg: fmt.Sprintf("${args.%d} refers to a missing argument; %d given", index, len(rv.Args))}
		}
		return rv.Args[index-1], nil
	case "host":
		if value, ok := lookupPath(rv.Host, name); ok {
			return renderScalar("host."+name, value)
		}
		return "", invalid("unknown host reference ${host.%s}", name)
	case "run":
		if value, ok := lookupPath(rv.Run, name); ok {
			return renderScalar("run."+name, value)
		}
		return "", invalid("unknown run reference ${run.%s}", name)
	default:
		return "", invalid("unknown template namespace ${%s.%s}; use vars, env, args, host or run", namespace, name)
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

func renderScalar(label string, value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", invalid("%s is null and cannot be rendered", label)
	case string:
		return v, nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v), nil
	default:
		return "", invalid("%s has non-scalar type %T and cannot be interpolated", label, value)
	}
}

// LookupEnv resolves an environment name honoring Windows case-insensitivity.
func LookupEnv(env map[string]string, name string) (string, bool) {
	if value, ok := env[name]; ok {
		return value, true
	}
	if !data.IsWindows {
		return "", false
	}
	want := data.EnvKey(name)
	for key, value := range env {
		if data.EnvKey(key) == want {
			return value, true
		}
	}
	return "", false
}

// Refs returns the variable names interpolated through ${vars.*}.
func Refs(value string) []string {
	matches := templatePattern.FindAllStringSubmatch(value, -1)
	var out []string
	for _, match := range matches {
		if match[1] == "vars" {
			out = append(out, match[2])
		}
	}
	return out
}

// DetectCycle rejects static variable cycles. Only same-level references can
// form a cycle, so this check is decidable at load time, before any request runs.
func DetectCycle(level map[string]any) error {
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
			return invalid("variable cycle: %s", strings.Join(append(path, key), " -> "))
		}
		state[key] = visiting
		if text, ok := level[key].(string); ok {
			for _, ref := range Refs(text) {
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
	for _, key := range data.SortedDataKeys(level) {
		if err := visit(key, nil); err != nil {
			return err
		}
	}
	return nil
}

// ResolveLevel evaluates one level of static variables in dependency order.
// Unresolved references and cycles are definition errors.
func ResolveLevel(level map[string]any, base Vars) (map[string]any, error) {
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
			return invalid("variable cycle: %s", strings.Join(append(path, key), " -> "))
		}
		state[key] = visiting
		value := level[key]
		if text, ok := value.(string); ok && strings.Contains(text, "${") {
			view := base
			view.Vars = data.Merge(base.Vars, out)
			for _, ref := range Refs(text) {
				if _, sameLevel := level[ref]; !sameLevel {
					continue
				}
				if err := resolve(ref, append(path, key)); err != nil {
					return err
				}
			}
			view.Vars = data.Merge(base.Vars, out)
			rendered, err := Render(text, view)
			if err != nil {
				return err
			}
			out[key] = rendered
		} else {
			out[key] = data.Clone(value)
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

// identifierCache memoizes the word-boundary pattern of a name. It is read and
// written by concurrent runs, so it is a sync.Map.
var identifierCache sync.Map

// ReferencesAny reports whether source mentions any of the identifiers as a
// standalone word. It decides whether a condition needs a dynamic variable,
// which Inspect must not evaluate.
func ReferencesAny(source string, names []string) bool {
	for _, name := range names {
		if name == "" {
			continue
		}
		var re *regexp.Regexp
		if cached, ok := identifierCache.Load(name); ok {
			re = cached.(*regexp.Regexp)
		} else {
			re = regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_]|$)`)
			identifierCache.Store(name, re)
		}
		if re.MatchString(source) {
			return true
		}
	}
	return false
}
