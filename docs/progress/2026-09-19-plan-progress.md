# kscript 实施计划进度记录

> 记录日期：2026-09-19
> 计划：`docs/plans/2026-09-15-kscript-library-plan.md`（Draft 0.2）
> 设计：`docs/design/2026-09-15-kscript-library-design.md`（Draft 0.2）

本文件只记录事实、命令与证据，不代替计划或设计批准。

## 仓库与基线

| 项目 | 值 |
|---|---|
| 目标 module | `github.com/gookit/kscript` |
| Git root | `D:/work/inhere/my-tools-dev/gookit2/kscript` |
| 本记录起点 HEAD | `fa877afa442859b40508c8c874ba6495e60e4148` |
| Go 版本 | go.mod `go 1.23`；本机 `go1.25.10`，另用 `GOTOOLCHAIN=go1.23.12` 验证编译 |
| 远端 | 未配置（无 push/tag，符合外部动作 Gate） |
| 主要依赖 | `expr-lang/expr`、`goccy/go-yaml`、`BurntSushi/toml` |

## 任务状态

| 任务 | 状态 | 证据 |
|---|---|---|
| T01 目标仓库与基线确认 | 部分 | 目录与 Git root 已存在并有提交历史；第二应用路径仍未确认；无 CI 运行记录（无远端） |
| T02 module 与公共模型 | 基本完成 | `kscript.go`、`definition.go`、`request.go`、`result.go`、`engine.go`、`data.go`；`New` 冻结深拷贝、`BaseDir` 必须为绝对路径、负 timeout/空任务/多动作/未知 Shell 在 `New` 报错 |
| T03 loader、来源与发现 | 基本完成 | `formats/{load,decode,discover,doc}.go`；三格式等价、未知字段/重复 key/类型错误/越界路径报错、显式 `nearest`/`ancestors` 发现、多来源合并 |
| T04 变量、表达式与条件 | 完成 | `render.go`、`condition.go`；命名空间模板与未知引用报错、静态变量拓扑求值与环拒绝、动态变量按需求值一次、`Inspect` 标记 Deferred |
| T05 顺序图与运行状态 | 完成 | `graph.go`、`runner.go`；`New` 全图预检（环路径/深度）、运行期展开上限、每次调用独立 Vars/Env/Dir/deadline、`TaskResult` 记录跳过原因 |
| T06 进程/Shell/file/host 引擎 | 基本完成 | `process_engine.go`；显式 Shell 选择、argv 不二次分词、file 走注册表解释器、dry-run 零副作用、错误分类 |
| T07 取消、超时与清理 | 完成 | `process_{unix,windows}.go`；POSIX 进程组、Windows Job Object、宽限期、能力不可用即失败、`Canceled`/`TimedOut` 分类 |
| T08 Runner/Inspect/示例/README | 完成 | Runner/`Inspect`/README/中文 README、CLI consumer 与 `examples/{basic,config,host}` 均可运行 |
| T09 Kite 迁移 | 基本完成 | `formats/legacy.go` 转换器（含 ParseEnv 的 `${env.*}` 重写）+ `formats.SplitCommandLine`；kite-go 侧 `pkg/kscript/bridge` 适配包与 `script_engine` 开关（默认 `legacy`，转换失败自动回退）；`RunAny` 与 `kite run --type=script` 已接入；任务级与配置级双引擎对照均通过；列表/搜索/`--show` 有意保留旧实现（理由见 kite-migration.md）；卡在：用户本机真实配置端到端 |
| T10 第二应用与 Go 版本矩阵 | 未完成 | CI matrix 定义存在但未运行；第二真实应用未确认，`tmp/kscript-consumer` 使用 `replace`，不构成可复用验收 |
| T11 文档、版本与发布准备 | 基本完成 | README/中文 README/kite-migration/CHANGELOG 完成；LICENSE 保留源码原始版权行；`docs/release/2026-09-19-v0.1.0-candidate-review.md` 形成发布候选（含依赖许可证、API 面、限制与发布清单）；缺外部动作授权（建远端/推送/tag/发布） |

## 验证命令与结果（2026-09-19）

```bash
go build ./...                 # 通过
go vet ./...                   # 通过
go test -count=1 ./...         # 通过（kscript 与 formats 两个包）
gofmt -l .                     # 无输出
GOOS=linux  go build ./...     # 通过（POSIX 分支）
GOOS=darwin go build ./...     # 通过
GOTOOLCHAIN=go1.23.12 go build ./...   # 通过（Go 1.23 语言/API 兼容）
go run ./cmd/kscript -config ./examples/basic.json -task hello          # 通过，输出 go version
go run ./cmd/kscript -config ./examples/basic.json -task check -dry-run # 通过，输出计划
go list -deps ./...            # 只有标准库与 expr/go-yaml/toml，无 inhere/kite-go、gcli、cliui、gookit/slog

# 工作区外的独立 module（仅导入主包）
cd ../../tmp/kscript-consumer && go test -count=1 ./...   # 通过；go list -deps 无 kite-go

# Kite 侧（迁移状态）
cd ../../inhere-tools/kite-go && go build ./...
go test -count=1 ./pkg/kscript/... ./internal/biz/cmdbiz/  # 通过，含双引擎与配置级对照
```

未完成的验证：

- `go test -race ./...`：本机为 Windows 且无 C 工具链（`CGO_ENABLED=0`，无 gcc），无法运行；
  需要在 Linux runner 或安装 gcc 后执行。
- CI matrix（Go 1.23.x/1.25.x、ubuntu/windows）尚未在真实 runner 上执行（无远端）。
- 第二真实应用接入与结果记录（T10）。

## 设计验收矩阵覆盖情况

| ID | 场景 | 现状 |
|---|---|---|
| A01 | 外部 module 只导入主包执行任务 | 本地通过：`tmp/kscript-consumer` 编译、测试、`go list -deps` 无 kite-go；发布版本后的正式验收待外部动作 |
| A02 | 三格式等价与非法输入定位 | 覆盖（`formats` 测试） |
| A03 | 缺失 deps、混合环、展开超限 | 覆盖（`TestCycleIsRejectedAtNewWithPath`、`TestExpansionLimitAtRuntime`） |
| A04 | 菱形依赖与重复调用 | 覆盖（`TestDiamondDependencyRunsTwice`） |
| A05 | 条件 true/false/空/类型错误/平台 | 覆盖 |
| A06 | 参数空格、引号、空串、Windows 路径 | 覆盖（`TestExecKeepsArgumentBoundaries` 逐字节校验 argv） |
| A07 | go run / sh / cmd / pwsh 解释器 | 基本覆盖（`file` 动作走 `go run`、`sh`/`bash`/`cmd` 与 prefix_args 有测试；pwsh 依赖 runner 是否安装） |
| A08 | vars/env 优先级、CleanEnv、PATH、动态变量 | 覆盖 |
| A09 | dry-run 零副作用与 Deferred | 覆盖 |
| A10 | 并发 Run 隔离 | 覆盖（并发用例通过；race 检测受限） |
| A11 | 超时/取消清理进程树 | 覆盖（子进程树标记文件用例） |
| A12 | 大输出、截断、writer 失败 | 覆盖 |
| A13 | 非零退出、ignore_error、取消、未知根任务 | 覆盖 |
| A14 | Kite 旧配置 fixture 行为对照 | 覆盖（`bridge/compat_test.go` 任务级 trace 逐字节一致 + 失败行为一致；`internal/biz/cmdbiz/scriptengine_e2e_test.go` 用真实形态配置文件 `DefineFiles` + `ScriptDirs` + `__settings` 双引擎对照一致；用户本机真实配置端到端待补） |
| A15 | 第二真实应用 | 未完成 |

## 与计划的偏差（需评审确认）

| 偏差 | 说明 |
|---|---|
| 文件布局 | 计划列出 `internal/graph`、`internal/render`、`internal/process`；这些实现依赖主包类型，放在主包同名文件中（`graph.go`、`render.go`、`condition.go`、`process_*.go`），公共 API 语义不变 |
| `Timeout` 类型 | Plan/T02 未固定单位；实现改为 `time.Duration`，配置使用时长字符串。原 int64 纳秒语义会使 `timeout: 5` 变成 5ns |
| `Inspect` 增加 ctx | 设计草案签名为 `Inspect(req)`；实现为 `Inspect(ctx, req)`，以便取消与一致性，语义未变 |
| `BaseDir` 必须绝对 | 设计明确要求；`formats.LoadFile` 自动转换为绝对路径，调用方需注意 |
| 第二应用 | T01 Gate 未确认路径，仍为开放项 |
| Kite 依赖方式 | `kite-go/go.mod` 使用临时 `replace => ../../gookit2/kscript`（设计允许的本地迁移验证）；发布版本后需改为真实版本号 |
| Kite 引擎开关 | 新增 `script_engine: legacy|kscript`（默认 `legacy`），作为回退点；旧 Runner 未被删除 |

## 下一步

1. T09 剩余：在用户本机真实 Kite 配置上以 `script_engine: kscript` 跑一次端到端（列表/搜索保留旧实现为有意决策）。
2. T10：确认第二真实应用并接入；在真实 runner 上执行 Go 1.23/1.25 与 race 矩阵。
3. T11 剩余（均需外部动作授权）：创建远端仓库、推送、打 `v0.1.0` tag、发布；
   之后把 `kite-go/go.mod` 的临时 replace 换成真实版本号并做工作区外验收。
