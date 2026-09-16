# kscript

`github.com/gookit/kscript` is an embeddable Go task and script runner. It supports Go 1.23+, JSON/YAML/TOML definitions, task dependencies, conditions, external commands, shells, script files, host handlers, dry-run plans, timeouts, and bounded output capture.

## CLI

```bash
go run ./cmd/kscript -config ./examples/basic.json -task hello
go run ./cmd/kscript -config ./examples/basic.json -task hello -dry-run
```

## Definition

```json
{
  "version": 1,
  "vars": {"message": "hello"},
  "tasks": {
    "hello": {
      "if": "enabled",
      "run": "printf ${vars.message}"
    }
  }
}
```

Create a runner from Go:

```go
runner, err := kscript.New(definition)
result, err := runner.Run(ctx, kscript.Request{Task: "hello"})
```

`Inspect` and `DryRun` produce a plan without executing actions. `exec` keeps argv boundaries; `shell` is explicit and platform dependent. `context.Context` controls cancellation, while task and step timeouts bound execution.

The package does not initialize a CLI, change the host process working directory, or set the host process environment. Kite-specific aliases, extensions, and legacy task conversion belong in the Kite adapter.
