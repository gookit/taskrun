// Package taskrun is an embeddable task and script runner.
//
// A Definition is validated and frozen by New, so later mutation of the caller's
// maps cannot change a run. Each Run gets isolated variables, environment and
// working directory; actions run through explicit shells or host handlers, and
// the processes an action owns are terminated when its context is canceled or
// its deadline expires.
package taskrun

import (
	"os"
	"strings"
	"time"

	"github.com/gookit/taskrun/internal/data"
)

// Default limits applied by New unless an Option overrides them.
const (
	// DefaultMaxCallDepth bounds the static task call chain.
	DefaultMaxCallDepth = 64
	// DefaultMaxExpansions bounds the number of task calls in one run.
	DefaultMaxExpansions = 10000
	// DefaultKillGrace is how long a canceled process tree may exit on its own
	// before it is killed.
	DefaultKillGrace = 200 * time.Millisecond
	// DefaultDynamicOutputLimit bounds the captured output of a dynamic
	// variable command.
	DefaultDynamicOutputLimit = 1 << 20
)

// Option configures a Runner. Options are applied before the definition is
// validated, so an invalid engine, handler name or limit fails New.
type Option func(*runnerConfig) error

type runnerConfig struct {
	engine        Engine
	handlers      map[string]Handler
	observer      Observer
	baseEnv       map[string]string
	baseEnvSet    bool
	maxCallDepth  int
	maxExpansions int
	killGrace     time.Duration
	dynamicLimit  int64
}

// WithEngine replaces the external action backend. The backend must honor
// cancellation and return a ProcessError or a context cause so failures can be
// classified.
func WithEngine(engine Engine) Option {
	return func(c *runnerConfig) error {
		if engine == nil {
			return errf("WithEngine: engine is nil")
		}
		c.engine = engine
		return nil
	}
}

// WithHandler registers a host action. Handlers are immutable after New: a
// definition that names an unregistered handler is rejected.
func WithHandler(name string, handler Handler) Option {
	return func(c *runnerConfig) error {
		if name == "" {
			return errf("WithHandler: name is required")
		}
		if handler == nil {
			return errf("WithHandler: handler for %q is nil", name)
		}
		if c.handlers == nil {
			c.handlers = map[string]Handler{}
		}
		c.handlers[name] = handler
		return nil
	}
}

// WithObserver registers an event observer. Observers are optional, cannot
// change scheduling decisions and do not return errors; callbacks of one run are
// delivered in order, while different runs may call the observer concurrently.
func WithObserver(observer Observer) Option {
	return func(c *runnerConfig) error {
		if observer == nil {
			return errf("WithObserver: observer is nil")
		}
		c.observer = observer
		return nil
	}
}

// WithBaseEnv sets the environment baseline snapshot. The default is
// os.Environ copied when New runs; a run never re-reads the process
// environment and never modifies it.
func WithBaseEnv(env map[string]string) Option {
	return func(c *runnerConfig) error {
		for key := range env {
			if key == "" {
				return errf("WithBaseEnv: empty variable name")
			}
		}
		c.baseEnv = data.CloneStringMap(env)
		c.baseEnvSet = true
		return nil
	}
}

// WithMaxCallDepth bounds the static task call chain. Zero or negative disables
// the static check.
func WithMaxCallDepth(depth int) Option {
	return func(c *runnerConfig) error {
		c.maxCallDepth = depth
		return nil
	}
}

// WithMaxExpansions bounds the number of task calls in one run.
func WithMaxExpansions(limit int) Option {
	return func(c *runnerConfig) error {
		if limit <= 0 {
			return errf("WithMaxExpansions: limit must be positive")
		}
		c.maxExpansions = limit
		return nil
	}
}

// WithKillGrace sets how long a canceled process tree may exit on its own
// before the engine kills it.
func WithKillGrace(grace time.Duration) Option {
	return func(c *runnerConfig) error {
		if grace < 0 {
			return errf("WithKillGrace: grace must not be negative")
		}
		c.killGrace = grace
		return nil
	}
}

// WithDynamicOutputLimit bounds the captured output of a dynamic variable
// command. Output above the bound fails the run instead of rendering a
// truncated value.
func WithDynamicOutputLimit(limit int64) Option {
	return func(c *runnerConfig) error {
		if limit <= 0 {
			return errf("WithDynamicOutputLimit: limit must be positive")
		}
		c.dynamicLimit = limit
		return nil
	}
}

// Runner holds a validated definition snapshot and an immutable backend
// configuration. One Runner may serve concurrent Runs.
type Runner struct {
	def Definition
	cfg runnerConfig
}

// New validates a definition and publishes a private snapshot. Load, New,
// Lookup, List and Inspect never execute commands; only Run produces side
// effects through explicit actions and dynamic variables.
func New(def Definition, opts ...Option) (*Runner, error) {
	cfg := runnerConfig{
		handlers:      map[string]Handler{},
		maxCallDepth:  DefaultMaxCallDepth,
		maxExpansions: DefaultMaxExpansions,
		killGrace:     DefaultKillGrace,
		dynamicLimit:  DefaultDynamicOutputLimit,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, &RunError{Kind: ErrKindInvalidDefinition, Err: err}
		}
	}
	if cfg.engine == nil {
		cfg.engine = ProcessEngine{grace: cfg.killGrace}
	}
	prepared, err := prepareDefinition(def, cfg)
	if err != nil {
		return nil, err
	}
	if !cfg.baseEnvSet {
		cfg.baseEnv = envFromList(os.Environ())
	}
	handlers := make(map[string]Handler, len(cfg.handlers))
	for name, handler := range cfg.handlers {
		handlers[name] = handler
	}
	cfg.handlers = handlers
	return &Runner{def: prepared, cfg: cfg}, nil
}

// Lookup returns an exact-name copy of a task. The returned value shares no
// mutable state with the Runner.
func (r *Runner) Lookup(name string) (Task, error) {
	task, ok := r.def.Tasks[name]
	if !ok {
		// A missing dependency is a definition error; only a root lookup by
		// name reports ErrNotFound.
		return Task{}, &RunError{Kind: ErrKindNotFound, Task: name, Err: ErrNotFound}
	}
	return cloneTask(task), nil
}

// List returns every task sorted by name. Each entry is a copy.
func (r *Runner) List() []Task {
	names := make([]string, 0, len(r.def.Tasks))
	for name := range r.def.Tasks {
		names = append(names, name)
	}
	data.SortStrings(names)
	out := make([]Task, 0, len(names))
	for _, name := range names {
		out = append(out, cloneTask(r.def.Tasks[name]))
	}
	return out
}

// Source returns the definition provenance recorded by the loader, if any.
func (r *Runner) Source() []Source { return append([]Source(nil), r.def.Sources...) }

func envFromList(list []string) map[string]string {
	out := make(map[string]string, len(list))
	for _, item := range list {
		index := strings.Index(item, "=")
		if index <= 0 {
			continue
		}
		out[item[:index]] = item[index+1:]
	}
	return out
}
