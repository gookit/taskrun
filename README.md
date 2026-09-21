# taskrun

`github.com/gookit/taskrun` is an embeddable Go task and script runner. A Go 1.23+
application can load a task definition, inspect the plan, run tasks and script
files, and get an isolated, cancelable, classified result without initializing a
CLI framework or any global state.

## Quick start

```bash
go run ./cmd/taskrun -config ./examples/basic.json -task hello
go run ./cmd/taskrun -config ./examples/basic.json -task check -dry-run
```

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/gookit/taskrun"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	runner, err := taskrun.New(taskrun.Definition{
		Version: 1,
		BaseDir: dir,
		Tasks: map[string]taskrun.Task{
			"check": {
				Steps: []taskrun.Step{
					{Exec: &taskrun.ExecSpec{Program: "go", Args: []string{"version"}}},
				},
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	result, err := runner.Run(context.Background(), taskrun.Request{
		Task: "check",
		IO:   taskrun.IO{Stdout: os.Stdout, Stderr: os.Stderr},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("status=%s", result.Status)
}
```

`BaseDir` must be absolute. `New` copies and freezes the definition, so later
mutations by the caller have no effect, and one `Runner` may serve concurrent
runs. `log.Fatal` above is consumer behavior: the library never exits the
process.

## Definition schema (version 1)

```yaml
version: 1
vars:
  target: ./...
env:
  KS_ROOT: "."
env_paths: ["/opt/ks-bin"]
tasks:
  check:
    desc: check the project
    deps: [test]
    timeout: 2s
    steps:
      - exec:
          program: go
          args: [vet, "${vars.target}"]
  test:
    steps:
      - shell:
          name: sh
          script: 'printf "%s\n" "$KS_LABEL"'
        env:
          KS_LABEL: "${vars.target}"
  generate:
    steps:
      - file:
          name: generator
          args: [--check]
  notify:
    dynamic_vars:
      revision:
        exec: {program: git, args: [rev-parse, --short, HEAD]}
    steps:
      - host:
          name: app.notify
          args: ["${vars.revision}"]
files:
  generator:
    path: scripts/generate.go
    interpreter:
      program: go
      prefix_args: [run]
```

YAML, JSON and TOML decode into the same model. Unknown fields, duplicate keys,
type errors, path escapes, mixed step actions, missing references and negative
timeouts are rejected with the source file and field path, before anything runs.

`timeout` is a duration string (`500ms`, `2s`). Task fields: `desc`, `if`,
`platform`, `deps`, `dir`, `timeout`, `clean_env`, `env`, `env_paths`, `vars`,
`dynamic_vars`, `steps`. Step fields: `name`, `if`, `platform`, `dir`,
`timeout`, `ignore_error`, `env`, `env_paths`, `vars`, `dynamic_vars` and
exactly one of `exec`, `shell`, `file`, `task`, `host`.

## Actions

| Action | Meaning |
|---|---|
| `exec` | executable plus argv; no shell parsing, no implicit word splitting |
| `shell` | explicit shell (`sh`, `bash`, `zsh`, `cmd`, `pwsh`, `powershell`) with its own argument contract |
| `file` | named entry from `files`, run as `interpreter.program` + `prefix_args` + absolute script path |
| `task` | serial call of another task; `args` replaces the inherited arguments, `forward_args` appends the request arguments |
| `host` | handler registered with `WithHandler`, immutable after `New` |

## Variables, environment and directories

Lowest to highest precedence:

| Setting | Order |
|---|---|
| Vars | definition defaults → task → step → `Request.Vars` |
| Env | `WithBaseEnv` snapshot → definition → task → step → `Request.Env` |
| PATH | `EnvPaths` step → task → definition, prepended to the effective PATH |
| Dir | `Request.Dir` or `BaseDir`, then task `dir`, then step `dir`; absolute paths stay absolute |

`clean_env: true` on a task drops only the base environment snapshot; explicit
`env` values remain. The library never calls `os.Chdir`, `os.Setenv`, never
re-reads the process environment during a run and never writes results to disk.

Templates are single-pass and namespaced: `${vars.name}`, `${env.NAME}`,
`${args.N}` (1-based), `${host.name}`, `${run.task}`, `${run.dir}`,
`${run.call}`, `${run.os}`, `${run.arch}`. Unknown references are errors and
`$${` escapes a literal `${`. Dynamic variables declared in `dynamic_vars` are
resolved at most once per task call or step, before first use, through the same
engine, context and output limit as other actions.

Conditions use [expr](https://github.com/expr-lang/expr) and must evaluate to a
bool. They may read `vars`, `env`, `args`, `host` and `run`, plus bare variable
names such as `enabled`. Task conditions are evaluated before dependencies, step
conditions after the step's dynamic variables and before the action. Inspect
never runs a dynamic variable command: it reports those fields as deferred.

## Execution, cancellation and output

- Dependencies and task calls run serially, once per occurrence; there is no
  implicit de-duplication and no implicit retry.
- Static cycles and over-deep call chains are rejected by `New` with the full
  path, before any action runs. A run also enforces a call expansion budget.
- The effective deadline is the earliest of the parent context and the task or
  step timeout. Cancellation stops scheduling new steps; `Result.Status` becomes
  `canceled` or `timed_out` and the returned `RunError` preserves
  `context.Canceled` / `context.DeadlineExceeded`.
- The default engine owns a process tree: POSIX children run in their own
  process group, Windows children in a Job Object. A canceled tree is asked to
  stop, then killed after the grace period. If the platform capability is
  unavailable, the action fails before starting instead of killing only the
  parent.
- `IO.CaptureLimit` bounds the collected bytes per stream. With a writer set,
  output is both forwarded and captured; after the bound, collection stops and
  output is marked truncated but keeps draining. A writer error terminates the
  action and is reported as an IO error. `CaptureLimit` 0 streams without
  collecting; negative values are rejected.
- `ignore_error` tolerates only a started process with a non-zero exit code or a
  handler business error. It never tolerates a start failure, cancellation,
  timeout, output-limit, IO or configuration error.

## Statuses and errors

`Result.Status` is one of `succeeded`, `succeeded_with_warnings`, `failed`,
`canceled`, `timed_out`, `skipped` or `dry_run`; it never contradicts the
returned error. `Result.Tasks` records every task call, including skipped calls
and their reason, and `Result.Steps` records kind, start state, exit code,
output, truncation and error per step.

Failures return `*taskrun.RunError` with `Kind`, task, call id, step and source,
and support `errors.Is`/`errors.As` against `ErrNotFound`,
`ErrInvalidRequest`, `ErrInvalidDefinition`, `ErrDependencyCycle`,
`ErrExpansionLimit`, `ErrStart`, `ErrExit`, `ErrHandler`, `ErrOutputLimit`,
`ErrIO`, `context.Canceled` and `context.DeadlineExceeded`. `ErrNotFound` means
only that the requested root task name is unknown; a missing dependency is an
invalid definition, and a load failure is reported by the loader.

`Inspect(ctx, req)` and `Request{DryRun: true}` validate arguments and expand the
call graph without running any action, handler or dynamic variable command.

## Loading and discovery

```go
def, err := formats.LoadFile("tasks.yaml")           // baseDir becomes absolute
files, err := formats.Discover(formats.DiscoverOptions{
	Mode:     formats.Ancestors,                     // or formats.Nearest
	Names:    []string{"tasks", ".kite.task"},
	StartDir: dir,
	StopDir:  root,
	MaxDepth: 8,
})
def, err = formats.LoadFiles(files, false)           // false: conflicts are errors
```

Discovery is always explicit: names, start directory, stop directory and depth
come from the caller, and nothing above the described levels is read. `nearest`
stops at the first level with a match; `ancestors` collects one file per level
from the root down. Merging errors on duplicate task or script file names unless
`override` is enabled, in which case the later definition replaces the whole
task; `vars` and `env` are overridden by key and every source is recorded in
`Definition.Sources`.

`formats.LegacyMap` converts the historical Kite task map shape for the Kite
adapter; see `docs/kite-migration.md`. Command aliases, extensions, plugins and
system command fallback stay in Kite.

## Platforms and versions

Go 1.23 or newer, MIT licensed. Supported runtime platforms are Windows, Linux
and macOS; shell selection is explicit and shell sources are not portable across
platforms by assumption. The module depends on `expr-lang/expr`,
`goccy/go-yaml` and `BurntSushi/toml` only.

## Development

```bash
make check        # gofmt check, build, vet and tests
make test-go123   # run the tests with the declared minimum Go version
make test-race    # needs cgo and a C compiler
make cross        # linux and darwin builds
make cli          # run the example CLI against examples/basic.json
make examples     # run the Go examples
```
