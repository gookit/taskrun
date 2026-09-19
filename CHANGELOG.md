# Changelog

All notable changes to this module are documented here. The module follows
SemVer; versions before v1 are experimental and may change the API.

## [Unreleased]

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
- `cmd/kscript` CLI consumer and `examples/basic`, `examples/config`,
  `examples/host` runnable examples.
