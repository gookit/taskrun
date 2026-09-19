# Kite 迁移说明

独立库只负责任务定义、加载、条件、执行和结果。Kite 的 alias、extension、plugin、
系统命令兜底以及 `gvs`、`paths`、`kite` 应用变量仍由 Kite 适配层负责。

迁移步骤：

1. Kite 读取旧配置文件并保留原发现顺序（`DefineFiles` + `AutoTaskFiles`/`AutoMaxDepth`）。
2. 将旧 `Scripts` map 交给 `formats.LegacyDefinition` 转成 `kscript.Definition`；
   `ScriptDirs` 扫描结果通过 `LegacyOptions.Files` 传入，`type_shell` 通过
   `LegacyOptions.DefaultShell` 传入。
3. `AppendVarsFn` 产出的 `gvs`、`paths`、`kite` 等运行期变量放入 `Request.Vars`，
   与 `ctx.Vars` 合并；同时把这些名字列在 `LegacyOptions.RuntimeVars` 中，转换器才会把
   旧 `$name`/`${name}` 重写成 `${vars.name}`。
4. 继续由 `RunAny` 先处理 alias 和 extension，再调用独立 Runner。
5. 只有根任务返回 `kscript.ErrNotFound` 时才允许继续系统命令兜底；`ErrInvalidDefinition`
   和加载错误必须直接返回。
6. 迁移期间保留旧 Runner 回退点（配置开关或独立提交），完成 fixture 对照后再删除旧实现。

独立库不自动解释 Kite 私有的 `$@`、`$*`、`@task:` 或插件命名空间。转换器把这些语义转换成
明确的 Args、TaskCall 或 Handler，避免把 Kite 历史协议写入公共库。

## 转换映射

| 旧写法 | 转换结果 |
|---|---|
| `name: "go version"` / `[a, b]` / `run:` | 逐条命令；无 `type` 时按 exec 拆分成 argv |
| `type: sh` / `type_shell: sh` / `@sh:` | `ShellSpec{Name: sh}`（同一份源码仍由 Kite 决定平台可用性） |
| `@exec: go env GOOS` | `ExecSpec`（按命令行拆分） |
| 裸 `@cmd` | `ExecSpec` + `ignore_error: true`；旧 silent 归 Kite 输出策略 |
| `@task:name` / `task: name` | `TaskCall{Name: name, ForwardArgs: true}` |
| `deps` / `depends` | `Task.Deps` |
| `dir` / `workdir` | `Task.Dir`、`Step.Dir` |
| `timeout` / `cmd_timeout`、命令 `timeout` | `Task.Timeout`、`Step.Timeout`（时长字符串） |
| `env`、`env_path`/`env_paths` | `Task.Env`、`Step.Env`、`Task.EnvPaths` |
| `vars` 中以 `@sh:`/`@exec:` 开头的值 | `DynamicVars`；其他类型前缀按旧行为保留为字面量 |
| `vars` 其他值、命令 `vars` | `Task.Vars`、`Step.Vars` |
| 命令 `if`、任务 `if` | `If`（expr，裸变量名，不做模板转换） |
| `ignore_err` / `safe_run` | `Step.IgnoreError` |
| `__settings.vars`、`groups` + `default_group` | `Definition.Vars` |
| `__settings.env`、`env_path(s)` | `Definition.Env`、`Definition.EnvPaths` |
| `ScriptDirs` + `AllowedExt` + `ExtToBinMap` | `Definition.Files`；`ExtToBinMap` 值按命令行拆成 program + prefix_args |
| `alias`、`ext`、`scope`、`args`(usage)、`silent`、`output`、`fail_msg`、`for` | 不进入新模型，转换时产生 warning，由 Kite 保留 |

变量语法转换（仅对 `RuntimeVars` 中已知的名字）：

| 旧语法 | 新语法 |
|---|---|
| `$name`、`${name}` | `${vars.name}` |
| `$1`…`$N`、`${N}` | `${args.N}`（从 1 开始） |
| `$@` | `${vars.@}`（Kite 在 Request.Vars 注入空格连接的参数） |
| `$*` | `${vars.*}`（Kite 注入带引号的参数串） |
| `${vars.x}` | 原样保留 |
| `$HOME` 等未知名字、`$$`、Shell 内建展开 | 原样保留 |

`$@`/`$*`/`$1..$N` 的取值由 Kite 适配层在每次运行时按旧 `AppendArgsToVars` 语义写入
`Request.Vars`（键为 `@`、`*`、`1`…），因此任务文件不需要修改。

## 迁移状态（2026-09-19）

已完成：

- 新库侧 `formats.LegacyDefinition` 转换器（见上表）与 `formats.SplitCommandLine`。
- Kite 侧 `pkg/kscript/bridge` 适配包：把旧 Runner 已加载的脚本 map、`__settings`、
  `ScriptDirs`/`AllowedExt`/`ExtToBinMap`、运行期变量（`ctx.Vars`、`$@`/`$*`/`$1..N`、
  `time`/`workdir`/`dirname`/`cur_dir`、`AppendVarsFn` 的 `gvs`/`paths`/`kite`）转成
  `kscript.Definition` 与 `Request`，并提供 `TryRun`/`Run`（未匹配返回 found=false，
  便于继续系统命令兜底）。
- Kite 侧引擎开关：配置 `script_engine: legacy|kscript`，默认 `legacy`。
  `cmdbiz.RunScriptName`/`RunScriptOnly` 按开关分发；`RunAny` 与
  `kite run --type=script` 已接入。旧 Runner 保留为回退点，转换失败时桥接自动回退到
  旧实现（`WithLegacyFallback(true)`）并记录 warning。
- Kite 侧新增只读访问器 `SettingsData`、`ScriptFileMap`、`ExtToBin`，旧包其余行为不变。

依赖方式：当前 `kite-go/go.mod` 使用临时 `replace github.com/gookit/kscript =>
../../gookit2/kscript`（设计允许的本地迁移验证方式）。发布正式版本后应改为真实版本号；
该 replace 是本迁移的临时状态，不影响新库。

验证（2026-09-19）：

```bash
# 新库
cd gookit2/kscript && go build ./... && go vet ./... && go test -count=1 ./...

# Kite 侧
cd inhere-tools/kite-go && go build ./...
go test -count=1 ./pkg/kscript/... ./internal/biz/cmdbiz/
```

结果：全部通过。`pkg/quickjump`（既有断言差异）与 `pkg/simpleai`（既有 vet 报错，
`fmt.Println` 多余换行、非常量格式串）在本次改动前即失败，与迁移无关。

旧 fixture 双引擎对照：`pkg/kscript/bridge/compat_test.go`
用同一份旧配置（`__settings`、deps、任务变量、命令变量、`@task:` 引用、`$1`/`$2`、
Shell 展开的 env）分别在旧 Runner 与桥接引擎上运行，并把每条命令的可观察结果写入
`trace.txt` 后逐字节比较，结果一致；失败行为（命令非零退出）在两侧都返回错误。
对照过程中确认了三处旧实现的真实行为，已按“有意修复/需迁移说明”记录在下表。

已修复的既有缺陷：`ScriptTask.resolveIfExpr` 对空条件直接 `expr.Compile("")` 会 panic；
现在空条件返回 true。设计明确新实现不保留 panic，该测试此前一直 panic 失败。

## 尚未完成

- `script_engine: kscript` 尚未在真实 Kite 配置上端到端运行验证。
- 用户本机真实配置（`~/.kite` 全局脚本 + 项目自动发现文件）尚未用 `kscript` 引擎跑过。

## 有意保留：列表、搜索与 `--show`

`kite run -l`、`--search`、`--show` 仍由旧 Runner 解析（`RawScriptTasks`、
`GlobalScriptTasks`、`ProjectScriptTasks`、`TaskNameDescs`、`Search`、
`LoadScriptTaskInfo`、`LoadScriptFileInfo`）。理由：

1. 这些路径只读，不执行命令，也不是本次抽离的风险点；它们的输出格式属于 Kite 展示层。
2. 旧 Runner 仍加载同一份原始 `Scripts` map，因此引擎切换期间列表与实际可运行任务一致
   （两套引擎共享同一份配置来源，转换只在运行时发生）。
3. 迁移期保留单一解析实现，可以避免在回退点仍存在时维护两套展示模型。

回退点移除时（即旧 Runner 删除、`script_engine` 开关取消）需要一并把列表/搜索迁到新库，
所需数据为：任务名与 `Desc`、脚本文件注册表，以及全局/项目两套来源集合。
- 旧 fixture 的运行结果逐项对照已完成（`bridge/compat_test.go` 双引擎 trace 逐字节比较）；仍未做的是在真实 Kite 配置上以 `script_engine: kscript` 端到端运行。

## 行为差异（有意修复）

| 旧行为 | 新行为 |
|---|---|
| `resolveIfExpr` panic、打印并固定返回 true | 条件必须返回 bool；错误可分类，任务/步骤跳过可观察 |
| `${1}`、`${2}` 带花括号的数字形式不替换（只有 `$1` 生效） | `$N` 与 `${N}` 都替换为 `${args.N}` |
| 只有 `task: name`、没有 `run` 的命令 map 被静默忽略 | 该 map 会变成 task call（历史未生效字段转为生效，需要迁移说明） |
| `type: cmd`/`pwsh` 在 Windows 上仍按 `-c` 调用（实际不可用） | `cmd` 用 `/D /S /C`，`pwsh`/`powershell` 用 `-NoLogo -NoProfile -NonInteractive -Command` |
| `ScriptDirs` 脚本文件把 `BinName` 当作程序名、把文件当作第一个参数；Windows 上没有可用写法（`cmd` 不带 `/C` 会进入交互） | `Interpreter.Program` + `PrefixArgs` 结构化解释器（例如 `cmd` + `/C`），未知扩展或缺失解释器报错 |
| 加载完成标记早于加载成功、错误可被分发路径吞掉 | 定义先校验冻结、再发布；错误分类保留 |
| 递归任务共享并修改 `RunCtx`、包级 renderer | 每次调用独立 Vars/Env/Dir/deadline，无共享可变状态 |
| 只有顺序递归、无环检测 | `New` 预检完整环路径与深度，运行期限制展开数 |
| 所有 Shell 统一 `-c`、文件走 `go run` 字符串映射 | 显式 Shell 选择与结构化解释器；未知 Shell/解释器报错 |
| dry-run 仍会执行动态变量命令 | Inspect/DryRun 不执行任何动作，动态字段标记 Deferred |
| 旧命令默认吞掉错误 | `ignore_error` 只容忍非零退出码或 Handler 业务错误 |
