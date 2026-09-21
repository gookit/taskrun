# Changelog

All notable changes to this module are documented here. The module follows
SemVer; versions before v1 are experimental and may change the API.

## [v0.2.0] - 2026-09-21

### Fixed

- The cache behind condition reference detection is concurrency safe. It was a
  plain map read and written by every run that evaluates a condition, so two
  concurrent runs could crash the process with `concurrent map read and map
  write`.

### Changed

- Implementation moved behind `internal/` so the root package is only the public
  API and its orchestration: `internal/data` (copy, merge and validation
  helpers), `internal/graph` (cycle and depth checks), `internal/render`
  (templates and conditions) and `internal/process` (platform process tree
  control). The exported API is unchanged.

## [v0.1.0] - 2026-09-21

### Fixed

- Each process tree control is released exactly once. The deferred release was
  bound to the owning control before the fallback replaced it, so on Windows the
  job handle was closed twice while the fallback control was never released;
  closing the same handle value twice can close an unrelated handle once the
  value is reused.

### Changed

- Windows tree cleanup degrades instead of failing when the owning mechanism is
  unavailable: if `AssignProcessToJobObject` is denied (a restricted job already
  owns the process, as on GitHub Actions runners), the engine terminates the tree
  through live parent process ids, so descendants are still killed. Set
  `ProcessEngine{TreeKill: TreeKillRequired}` to fail the action instead.
- Module renamed from `github.com/gookit/kscript` to `github.com/gookit/taskrun`
  (package `taskrun`) before the first release. The `k` prefix was a Kite
  leftover, and the old name collided with the legacy `pkg/kscript` package in
  kite-go, which forced an import alias in the migration bridge.

### Added

- `Definition`, `Task`, `Step`, `Request`, `IO`, `Result`, `Plan`, `Engine` and
  `Handler` model with a frozen definition snapshot per `Runner`.
- `New` pre-flight validation: missing references, dependency and task-call
  cycles with the full path, over-deep call chains, wrong step action counts,
  unknown shells, unregistered handlers, unknown script files and negative
  timeouts.
- Serial dependency and task-call execution with isolated per-call variables,
  environment, directory and deadline; argument inheritance, explicit
  replacement and `forward_args`.
- Namespaced templates (`${vars.*}`, `${env.*}`, `${args.N}`, `${host.*}`,
  `${run.*}`) with errors on unknown references and `${` escaping, plus
  topological static variable evaluation and cycle rejection.
- `expr` conditions on tasks and steps with `vars`, `env`, `args`, `host` and
  `run` namespaces; `Inspect` reports runtime-dependent fields as deferred
  instead of executing them.
- Dynamic variables declared per task or step, evaluated at most once per call,
  with a bounded capture limit.
- Default process engine with explicit shell selection (`sh`, `bash`, `zsh`,
  `cmd`, `pwsh`, `powershell`), argv boundaries, path resolution against the
  effective environment, bounded capture with forwarding, and process-tree
  cleanup through POSIX process groups or Windows Job Objects.
- `RunError` classification with `errors.Is`/`errors.As` support and preserved
  `context.Canceled` / `context.DeadlineExceeded` causes.
- `formats` package: YAML/JSON/TOML decoding of schema version 1, strict
  unknown-field and duplicate-key rejection, source and field-path errors,
  explicit `nearest`/`ancestors` discovery, multi-source merging and the legacy
  Kite task map converter.
- `WithObserver` and `Event`: ordered run, task and step events (started,
  skipped, finished) with call id, depth, action kind, effective directory, exit
  code and the classified error. Observers cannot change scheduling, and Inspect
  or DryRun emit nothing.
- `cmd/taskrun` CLI consumer and `examples/basic`, `examples/config`,
  `examples/host` runnable examples.
