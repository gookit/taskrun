# kscript

`github.com/gookit/kscript` 是可嵌入 Go 应用的任务和脚本执行库，支持 Go 1.23+、JSON/YAML/TOML、任务依赖、条件判断、外部命令、Shell、脚本文件、宿主 Handler、dry-run、超时和有界输出。

## CLI

```bash
go run ./cmd/kscript -config ./examples/basic.json -task hello
go run ./cmd/kscript -config ./examples/basic.json -task hello -dry-run
```

## 配置

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

Go 应用可以直接创建 Runner：

```go
runner, err := kscript.New(definition)
result, err := runner.Run(ctx, kscript.Request{Task: "hello"})
```

`Inspect` 和 `DryRun` 只生成计划，不执行动作。`exec` 保留 argv 参数边界；`shell` 必须显式指定并受平台 Shell 语义影响。`context.Context` 支持取消，Task 和 Step timeout 限制执行时间。

库不会初始化 CLI、修改宿主进程 cwd 或设置宿主进程环境。Kite alias、extension 和旧任务格式转换由 Kite 适配层负责。
