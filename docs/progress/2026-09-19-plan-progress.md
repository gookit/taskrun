# taskrun 实施计划进度记录

> 记录日期：2026-09-19
> 计划：`docs/plans/2026-09-15-kscript-library-plan.md`（Draft 0.2）
> 设计：`docs/design/2026-09-15-kscript-library-design.md`（Draft 0.2）

本文件只记录事实、命令与证据，不代替计划或设计批准。

## 仓库与基线

| 项目 | 值 |
|---|---|
| 目标 module | `github.com/gookit/taskrun` |
| Git root | `D:/work/inhere/my-tools-dev/gookit2/kscript`（模块 `github.com/gookit/taskrun`；目录改名被本工作区文件锁阻塞，未强改） |
| 本记录起点 HEAD | `fa877afa442859b40508c8c874ba6495e60e4148` |
| Go 版本 | go.mod `go 1.23`；本机 `go1.25.10`，另用 `GOTOOLCHAIN=go1.23.12` 验证编译 |
| 远端 | 未配置（无 push/tag，符合外部动作 Gate） |
| 主要依赖 | `expr-lang/expr`、`goccy/go-yaml`、`BurntSushi/toml` |

## 任务状态

| 任务 | 状态 | 证据 |
|---|---|---|
| T01 目标仓库与基线确认 | 部分 | 目录与 Git root 已存在并有提交历史；第二应用路径仍未确认；无 CI 运行记录（无远端） |
| T02 module 与公共模型 | 基本完成 | `taskrun.go`、`definition.go`、`request.go`、`result.go`、`engine.go`、`data.go`；`New` 冻结深拷贝、`BaseDir` 必须为绝对路径、负 timeout/空任务/多动作/未知 Shell 在 `New` 报错 |
| T03 loader、来源与发现 | 基本完成 | `formats/{load,decode,discover,doc}.go`；三格式等价、未知字段/重复 key/类型错误/越界路径报错、显式 `nearest`/`ancestors` 发现、多来源合并 |
| T04 变量、表达式与条件 | 完成 | `render.go`、`condition.go`；命名空间模板与未知引用报错、静态变量拓扑求值与环拒绝、动态变量按需求值一次、`Inspect` 标记 Deferred |
| T05 顺序图与运行状态 | 完成 | `graph.go`、`runner.go`；`New` 全图预检（环路径/深度）、运行期展开上限、每次调用独立 Vars/Env/Dir/deadline、`TaskResult` 记录跳过原因 |
| T06 进程/Shell/file/host 引擎 | 基本完成 | `process_engine.go`；显式 Shell 选择、argv 不二次分词、file 走注册表解释器、dry-run 零副作用、错误分类 |
| T07 取消、超时与清理 | 完成 | `process_{unix,windows}.go`；POSIX 进程组、Windows Job Object（受限宿主被拒时按 `TreeKillAuto` 退化为父进程链终止整棵树，`TreeKillRequired` 保留严格失败，见设计修订 0.4）、宽限期、`Canceled`/`TimedOut` 分类 |
| T08 Runner/Inspect/示例/README | 完成 | Runner/`Inspect`/README/中文 README、CLI consumer 与 `examples/{basic,config,host}` 均可运行 |
| T09 Kite 迁移 | 基本完成 | `formats/legacy.go` 转换器（含 ParseEnv 的 `${env.*}` 重写）+ `formats.SplitCommandLine`；kite-go 侧 `pkg/kscript/bridge` 适配包与 `script_engine` 开关（默认 `legacy`，转换失败自动回退）；`RunAny` 与 `kite run --type=script` 已接入；任务级、配置级双引擎对照均通过；仓库真实配置（`config/module/scripts.yml`，8 个任务）转换、校验与规划全部通过、0 warning；列表/搜索/`--show` 有意保留旧实现；仅剩“用真实配置实际执行任务”（有副作用，需你运行） |
| T10 第二应用与 Go 版本矩阵 | 部分 | 仓库已建（`gookit/taskrun`）并推送；CI 在真实 runner 上全绿（`go.yml`：ubuntu × Go 1.23/1.24/1.25/stable 四个作业，Revive 0 告警；`race-and-windows.yml`：ubuntu `-race` + windows 构建与测试）；第二真实应用仍未确认，`tmp/taskrun-consumer` 使用 `replace` |
| T11 文档、版本与发布准备 | 基本完成 | README/中文 README/kite-migration/CHANGELOG 完成；LICENSE 保留源码原始版权行；`docs/release/2026-09-19-v0.1.0-candidate-review.md` 形成发布候选（含依赖许可证、API 面、限制与发布清单）；缺外部动作授权（建远端/推送/tag/发布） |

## 验证命令与结果（2026-09-19）

```bash
go build ./...                 # 通过
go vet ./...                   # 通过
go test -count=1 ./...         # 通过（taskrun 与 formats 两个包）
gofmt -l .                     # 无输出
GOOS=linux  go build ./...     # 通过（POSIX 分支）
GOOS=darwin go build ./...     # 通过
GOTOOLCHAIN=go1.23.12 go build ./...   # 通过（Go 1.23 语言/API 兼容）
GOTOOLCHAIN=go1.23.12 go test -count=1 ./...   # 通过（Go 1.23 也跑测试，不只是编译）
go run ./cmd/taskrun -config ./examples/basic.json -task hello          # 通过，输出 go version
go run ./cmd/taskrun -config ./examples/basic.json -task check -dry-run # 通过，输出计划
go list -deps ./...            # 只有标准库与 expr/go-yaml/toml，无 inhere/kite-go、gcli、cliui、gookit/slog

# 工作区外的独立 module（仅导入主包，replace 到本地目录）
cd ../../tmp/taskrun-consumer && go test -count=1 ./...   # 通过；go list -deps 无 kite-go

# 工作区外的独立 module（模拟“已发布版本”，不依赖 replace）
# 做法：用 git archive 从当前提交生成模块 zip，摆成 file:// 模块代理，再让一个
# 全新 module 通过版本号引入；这样验证的是“发布后 go get 能否用”，而不是本地目录。
#   <proxy>/github.com/gookit/taskrun/@v/v0.1.0-rc-local.{info,mod,zip}
#   GOPROXY=file:///<proxy>,https://goproxy.cn,direct GOSUMDB=off GOFLAGS=-mod=mod
#   go mod init acceptance && go mod edit -require=github.com/gookit/taskrun@v0.1.0-rc-local
#   go mod tidy && go list -m github.com/gookit/taskrun && go build ./... && go run .
# 结果（2026-09-21）：模块 zip 被接受并解析为 v0.1.0-rc-local；go list -deps 无 kite-go；
# 程序输出 “status=succeeded tasks=2 steps=4”，并断言了冻结定义、跳过步骤、ErrNotFound、
# ErrExit 与 handler 参数。唯一未做的只是真实 tag 之后用真实代理复核一次。

# Kite 侧（迁移状态）
cd ../../inhere-tools/kite-go && go build ./...
go test -count=1 ./pkg/kscript/... ./internal/biz/cmdbiz/  # 通过，含双引擎与配置级对照
```

本机无法执行的验证：

- `go test -race ./...`：本机为 Windows 且无 C 工具链（`CGO_ENABLED=0`，无 gcc），无法本地运行；
  已由 `.github/workflows/race-and-windows.yml` 的 ubuntu job 在真实 runner 上执行并通过。替代措施：
  `TestConcurrentRunsAreIsolated` 用 16 个并发 Run × 4 轮、每轮校验输出等于本请求的变量值，
  可在没有 race 检测时发现跨请求串值。
- CI：`.github/workflows/go.yml`（组织模板，ubuntu × Go 1.23/1.24/1.25/stable）与
  `.github/workflows/race-and-windows.yml`（新增：ubuntu `-race`、windows build+vet+test）。

CI 实测结论（仓库 `gookit/taskrun`，2026-09-21，提交 `2e1e69b`）：

| 运行 | 结论 |
|---|---|
| `action-tests`（`go.yml`，Go 1.23/1.24/1.25/stable 四个作业） | 通过（run 35581088175） |
| `action-tests` 的 Revive 步骤 | 0 条告警（此前 7 条，见下） |
| `race-and-windows` → Race detector（ubuntu，`go test -race`） | 通过（run 35581088159，42s） |
| `race-and-windows` → Windows（build + vet + test） | 通过（run 35581088159，1m4s） |
| 更早的 Windows job（run 35578758700） | 失败，根因见下；修复后连续两次通过 |

两个工作流都带 `paths: go.mod / **.go / **.yml` 过滤，因此纯文档提交不会触发 CI。

Windows 失败的根因（用 `gh run view --job <id> --log-failed` 读到）：

```
taskrun: start: process tree cleanup is unavailable:
  AssignProcessToJobObject for pid 7328 failed: Access is denied.
--- FAIL: TestConcurrentRunsAreIsolated
```

GitHub Actions 的 Windows runner 已经把它自己的 Job Object 套在步骤进程上，嵌套加入被拒绝。
原实现按设计“能力不可用即失败”，结果在该环境下列库完全跑不起来。处置（同样达到设计意图：
不允许只杀父进程）：

- 新增 `TreeKillMode`：默认 `TreeKillAuto` 在拥有机制不可用时退化为按活动父进程链终止
  （`taskkill /T /F /PID`），仍然清理子孙进程；`TreeKillRequired` 保留严格失败语义。
- 创建 Job Object 失败与加入失败都走同一分层策略，且只在 `TreeKillAuto` 下回退。
- 进程树测试拆成两个子用例（拥有机制 / 父进程链回退），两条路径都真实执行并通过；
  另加 `TestTreeKillRequiredFailsWithoutTheOwningMechanism` 覆盖严格模式。
- Linux 侧不受影响：进程组始终可用（race job 已连续两次通过）。
- 该行为属于语义变更，已按规范记入设计修订（`docs/design/2026-09-15-kscript-library-design.md`
  修订 0.4 与决策 D11），不是静默改行为。

## Revive 告警清理（2026-09-21，提交 `2e1e69b`）

首次在真实 runner 上跑 `go.yml` 时，Revive 步骤报出 7 条告警。逐条处理，不做忽略：

| 位置 | 问题 | 处置 |
|---|---|---|
| `runner.go` | 局部变量名 `call_` 带下划线 | 改名为 `hostCall`（外层已有 `call *callState`，不能直接叫 `call`） |
| `runner.go` | `fallbackCause` 声明了 `fallback` 却从不使用 | 删除该函数：它是空操作，且分支里的另一条路径只会把同一个错误再赋一次，属于误导性代码 |
| `process_unix.go` | `attach` 的 `cmd` 参数未使用 | 与 Windows 实现一致，去掉参数名（签名受 `treeControl` 接口约束） |
| `taskrun.go` | 缺少包注释 | 补 `Package taskrun` 注释 |
| `formats/legacy.go` | `legacyScriptFile` 的 `warn`、`translateLegacyTemplate` 的 `warn`/`where` 未使用 | 删除这些参数；随后 `legacyDynamicSpec` 的 `warn` 也失去用途，一并删除（级联） |

`warn` 回调本身仍在真正使用它的地方保留（`__settings` 忽略、未归属字段、平台覆盖等）。
本地用与 CI 同款默认规则运行 `revive ./...`：0 条告警；`gofmt`、`go vet`、`go test`、
Go 1.23 工具链与 linux/darwin 交叉编译全部通过。

## 设计验收矩阵覆盖情况

| ID | 场景 | 现状 |
|---|---|---|
| A01 | 外部 module 只导入主包执行任务 | 覆盖：`tmp/taskrun-consumer`（replace 到本地目录）编译/测试通过；另有本地 file 模块代理模拟“已发布版本”的验收，全新 module 通过版本号引入并真实运行通过，`go list -deps` 无 kite-go；真实 tag 后用真实代理复核待外部动作 |
| A02 | 三格式等价与非法输入定位 | 覆盖（`formats` 测试） |
| A03 | 缺失 deps、混合环、展开超限 | 覆盖（`TestCycleIsRejectedAtNewWithPath`、`TestExpansionLimitAtRuntime`） |
| A04 | 菱形依赖与重复调用 | 覆盖（`TestDiamondDependencyRunsTwice`） |
| A05 | 条件 true/false/空/类型错误/平台 | 覆盖 |
| A06 | 参数空格、引号、空串、Windows 路径 | 覆盖（`TestExecKeepsArgumentBoundaries` 逐字节校验 argv） |
| A07 | go run / sh / cmd / pwsh 解释器 | 覆盖：`TestShellArgumentContract` 固定各 shell 的调用契约（含 `zsh`），`TestShellSelectionIsExplicit` 在装有解释器的主机上真实执行 `sh`/`bash`/`pwsh`/`powershell`/`cmd`（本机 2026-09-21 实测五个全部通过），`file` 动作走 `go run` 与 `prefix_args` 另有测试 |
| A08 | vars/env 优先级、CleanEnv、PATH、动态变量 | 覆盖 |
| A09 | dry-run 零副作用与 Deferred | 覆盖 |
| A10 | 并发 Run 隔离 | 覆盖（并发用例通过；race 检测由 CI 的 ubuntu job 执行） |
| A11 | 超时/取消清理进程树 | 覆盖（子进程树标记文件用例） |
| A12 | 大输出、截断、writer 失败 | 覆盖 |
| A13 | 非零退出、ignore_error、取消、未知根任务 | 覆盖 |
| A14 | Kite 旧配置 fixture 行为对照 | 覆盖（`bridge/compat_test.go` 任务级 trace 逐字节一致 + 失败行为一致；`internal/biz/cmdbiz/scriptengine_e2e_test.go` 用真实形态配置文件 `DefineFiles` + `ScriptDirs` + `__settings` 双引擎对照一致；用户本机真实配置端到端待补） |
| A15 | 第二真实应用 | 未完成（待确认项目路径与 Go 版本，见 T10） |

## 与计划的偏差（需评审确认）

| 偏差 | 说明 |
|---|---|
| 文件布局 | 计划列出 `internal/graph`、`internal/render`、`internal/process`；这些实现依赖主包类型，放在主包同名文件中（`graph.go`、`render.go`、`condition.go`、`process_*.go`），公共 API 语义不变 |
| `Timeout` 类型 | Plan/T02 未固定单位；实现改为 `time.Duration`，配置使用时长字符串。原 int64 纳秒语义会使 `timeout: 5` 变成 5ns |
| `Inspect` 增加 ctx | 设计草案签名为 `Inspect(req)`；实现为 `Inspect(ctx, req)`，以便取消与一致性，语义未变 |
| `BaseDir` 必须绝对 | 设计明确要求；`formats.LoadFile` 自动转换为绝对路径，调用方需注意 |
| 第二应用 | T01 Gate 未确认路径，仍为开放项 |
| Kite 依赖方式 | `kite-go/go.mod` 使用临时 `replace github.com/gookit/taskrun => ../../gookit2/kscript`（设计允许的本地迁移验证）；发布版本后需改为真实版本号 |
| Kite 引擎开关 | 新增 `script_engine: legacy|taskrun`（默认 `legacy`），作为回退点；旧 Runner 未被删除 |

## 下一步

1. 外部动作（命令已备好，见 `docs/release/2026-09-21-publish-checklist.md`）：
   本机目录改名、创建 `gookit/taskrun` 远端并推送、打 `v0.1.0` tag、把 kite-go 的临时 replace
   换成真实版本、工作区外 consumer 正式验收。
2. T09 剩余：在真实 Kite 配置上以 `script_engine: taskrun` 实际执行一次任务（只读的转换/校验/规划已完成，
   执行会产生副作用，需由你在自己的机器上触发）。
3. T10：确认第二真实应用并接入；在真实 runner 上执行 Go 1.23/1.25 与 race 矩阵。
3. T11 剩余（均需外部动作授权）：创建远端仓库、推送、打 `v0.1.0` tag、发布；
   之后把 `kite-go/go.mod` 的临时 replace 换成真实版本号并做工作区外验收。

## 本地开发命令

设计里声明的公共边界现已全部落地：`New`/`Option`（`WithEngine`、`WithHandler`、
`WithBaseEnv`、`WithObserver`、深度/展开/宽限/输出上限）、`Lookup`/`List`/`Source`、
`Inspect`/`Run`、`Engine`/`Handler`/`Observer`、`Result`/`RunError`。其中
`WithObserver` 与 `Event` 于 2026-09-21 补齐：运行/任务/步骤的 started、skipped、
finished 事件，带 call id、深度、动作类型、有效目录、退出码与分类错误；跳过的任务只报
`task_skipped`（附原因），Inspect/DryRun 不产生事件。

测试中的平台跳过已改为按解释器/能力探测：本机（Windows + Git bash + PowerShell 7）
只有 `TestHelperProcess`（子进程夹具）与 `TestGracefulExitIsPreferred`（POSIX 信号语义）
会跳过，其余进程、Shell、并发、超时用例都真实执行。

模块内提供 `Makefile`（已在 Windows + GNU make 上实测）：

```bash
make check        # gofmt 检查 + build + vet + test
make test-go123   # 用 go1.23.12 工具链跑测试
make test-race    # 需要 CGO 与 C 编译器（本机没有，故未运行）
make cross        # linux / darwin 构建
make cli          # 跑示例 CLI
make examples     # 跑 Go 示例
```

## 模块改名（2026-09-21）

模块由 `github.com/gookit/kscript` 改名为 `github.com/gookit/taskrun`（主包 `taskrun`）。
原因：`k` 是 Kite 遗留前缀，且与 kite-go 旧包 `pkg/kscript` 同名会导致 import 别名。
改名范围：模块路径、包名、`taskrun.go`/`cmd/taskrun`、错误前缀、README/CHANGELOG/全部文档、
kite-go 侧的 require+replace 与桥接导入（别名 `kscript2` 已删除）、引擎开关值
`script_engine: taskrun`、工作区外 consumer。
设计文档升到 Draft 0.3（含 D01 更新），计划升到 0.3。
本机目录名仍为 `gookit2/kscript`（被工作区文件锁阻塞，未强改）。
