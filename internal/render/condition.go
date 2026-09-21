package render

import (
	"strings"

	"github.com/expr-lang/expr"

	"github.com/gookit/taskrun/internal/data"
)

// CompileCondition compiles an expr condition against the read-only view. A
// condition may read vars, env, args, host and run metadata; it cannot call host
// methods. A condition that needs a declared dynamic variable resolves it
// through Vars.Dynamic, which Inspect leaves nil so the condition is reported as
// deferred instead of running a command.
func CompileCondition(source string, rv Vars) (func() (bool, error), error) {
	if strings.TrimSpace(source) == "" {
		return func() (bool, error) { return true, nil }, nil
	}
	env := map[string]any{}
	varsCopy := data.CloneMap(rv.Vars)
	if ReferencesAny(source, rv.DeferredNames) {
		if rv.Dynamic == nil {
			return nil, ErrDeferred
		}
		for _, name := range rv.DeferredNames {
			value, found, err := rv.Dynamic(name)
			if err != nil {
				return nil, err
			}
			if found {
				varsCopy[name] = value
			}
		}
	}
	env["vars"] = varsCopy
	env["env"] = rv.Env
	env["args"] = rv.Args
	env["host"] = rv.Host
	env["run"] = rv.Run
	// Bare variable names stay available for simple conditions such as
	// "enabled", while the namespaces above remain reserved.
	for key, value := range varsCopy {
		if _, reserved := env[key]; reserved {
			continue
		}
		env[key] = value
	}
	program, err := expr.Compile(source, expr.Env(env))
	if err != nil {
		return nil, invalid("invalid condition %q: %v", source, err)
	}
	return func() (bool, error) {
		value, err := expr.Run(program, env)
		if err != nil {
			return false, invalid("condition %q failed: %v", source, err)
		}
		result, ok := value.(bool)
		if !ok {
			return false, invalid("condition %q must return bool, got %T", source, value)
		}
		return result, nil
	}, nil
}
