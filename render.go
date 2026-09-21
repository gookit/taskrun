package taskrun

import (
	"errors"

	"github.com/gookit/taskrun/internal/render"
)

// renderVars is the read-only view available to template interpolation and
// condition evaluation. The implementation lives in internal/render.
type renderVars = render.Vars

// conditionEvaluator is a compiled condition. An empty condition is always true.
type conditionEvaluator func() (bool, error)

// wrapRender maps a rendering error onto the package error kinds. A deferred
// value keeps the package sentinel so Inspect can report it.
func wrapRender(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, render.ErrDeferred) {
		return errDeferred
	}
	var rerr *render.Error
	if errors.As(err, &rerr) {
		kind := ErrKindInvalidDefinition
		if rerr.Kind == render.KindInvalidRequest {
			kind = ErrKindInvalidRequest
		}
		return &RunError{Kind: kind, Err: err}
	}
	return err
}

// renderTemplate interpolates namespaced templates. "$${" escapes a literal
// "${"; unknown references are errors.
func renderTemplate(source string, rv renderVars) (string, error) {
	out, err := render.Render(source, rv)
	if err != nil {
		return "", wrapRender(err)
	}
	return out, nil
}

// lookupEnv resolves an environment name honoring Windows case-insensitivity.
func lookupEnv(env map[string]string, name string) (string, bool) { return render.LookupEnv(env, name) }

// resolveVarLevel evaluates one level of static variables in dependency order.
func resolveVarLevel(level map[string]any, base renderVars) (map[string]any, error) {
	out, err := render.ResolveLevel(level, base)
	if err != nil {
		return nil, wrapRender(err)
	}
	return out, nil
}

// detectVarCycle rejects static variable cycles before any request runs.
func detectVarCycle(level map[string]any) error { return wrapRender(render.DetectCycle(level)) }

// compileCondition compiles an expr condition against the read-only view.
func compileCondition(source string, rv renderVars) (conditionEvaluator, error) {
	check, err := render.CompileCondition(source, rv)
	if err != nil {
		return nil, wrapRender(err)
	}
	return conditionEvaluator(check), nil
}
