package taskrun

import (
	"strings"

	"github.com/expr-lang/expr"
)

// conditionEvaluator is a compiled condition. An empty condition is always true.
type conditionEvaluator func() (bool, error)

// compileCondition compiles an expr condition against the read-only view. A
// condition may read vars, env, args, host and run metadata; it cannot call
// host methods. Conditions that need a declared dynamic variable resolve it
// through rv.Dynamic, which Inspect leaves nil so the condition is reported as
// deferred instead of running a command.
func compileCondition(source string, rv renderVars) (conditionEvaluator, error) {
	if strings.TrimSpace(source) == "" {
		return func() (bool, error) { return true, nil }, nil
	}
	env := map[string]any{}
	varsCopy := cloneDataMap(rv.Vars)
	if referencesAny(source, rv.DeferredNames) {
		if rv.Dynamic == nil {
			return nil, errDeferred
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
		return nil, invalidDef("invalid condition %q: %v", source, err)
	}
	return func() (bool, error) {
		value, err := expr.Run(program, env)
		if err != nil {
			return false, invalidDef("condition %q failed: %v", source, err)
		}
		result, ok := value.(bool)
		if !ok {
			return false, invalidDef("condition %q must return bool, got %T", source, value)
		}
		return result, nil
	}, nil
}

// condEnvKeys lists the reserved condition namespaces.
func condEnvKeys() []string { return []string{"vars", "env", "args", "host", "run"} }
