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

已修复的既有缺陷：`ScriptTask.resolveIfExpr` 对空条件直接 `expr.Compile("")` 会 panic；
现在空条件返回 true。设计明确新实现不保留 panic，该测试此前一直 panic 失败。

## 尚未完成

- Kite 侧列表/搜索/`--show` 路径仍使用旧 Runner 解析（`Search`、`LoadScriptTaskInfo`、
  `RawScriptTasks` 等），切换这些只读路径需要在新库上重建等价的展示模型。
- 旧 fixture 的运行结果逐项对照（同一配置分别用两个引擎运行并比较输出）尚未落地；
  当前只有转换等价性与单任务执行证据。
- `script_engine: kscript` 尚未在真实 Kite 配置上端到端运行验证。

## 行为差异（有意修复）

| 旧行为 | 新行为 |
|---|---|
| `resolveIfExpr` panic、打印并固定返回 true | 条件必须返回 bool；错误可分类，任务/步骤跳过可观察 |
| 加载完成标记早于加载成功、错误可被分发路径吞掉 | 定义先校验冻结、再发布；错误分类保留 |
| 递归任务共享并修改 `RunCtx`、包级 renderer | 每次调用独立 Vars/Env/Dir/deadline，无共享可变状态 |
| 只有顺序递归、无环检测 | `New` 预检完整环路径与深度，运行期限制展开数 |
| 所有 Shell 统一 `-c`、文件走 `go run` 字符串映射 | 显式 Shell 选择与结构化解释器；未知 Shell/解释器报错 |
| dry-run 仍会执行动态变量命令 | Inspect/DryRun 不执行任何动作，动态字段标记 Deferred |
| 旧命令默认吞掉错误 | `ignore_error` 只容忍非零退出码或 Handler 业务错误 |
