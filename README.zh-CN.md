# taskrun

[![Go mod version](https://img.shields.io/github/go-mod/go-version/gookit/taskrun?style=flat-square)](https://github.com/gookit/taskrun)
[![Actions Status](https://github.com/gookit/taskrun/workflows/action-tests/badge.svg)](https://github.com/gookit/taskrun/actions)
[![GoDoc](https://pkg.go.dev/badge/github.com/gookit/taskrun.svg)](https://pkg.go.dev/github.com/gookit/taskrun?tab=overview)
[![GitHub tag (latest SemVer)](https://img.shields.io/github/tag/gookit/taskrun)](https://github.com/gookit/taskrun)

`github.com/gookit/taskrun` 是可嵌入 Go 应用的任务与脚本执行库。Go 1.23+ 应用无需初始化
CLI 框架或全局状态，即可加载任务定义、查看执行计划、运行任务与脚本文件，并获得隔离、
可取消、可分类的结构化结果。

> **[English](README.md)**

## 快速开始

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

`BaseDir` 必须是绝对路径。`New` 会复制并冻结定义，之后调用方修改自己的定义不再影响
Runner；同一个 Runner 可并发服务多个 Run。示例中的 `log.Fatal` 是消费者行为，库本身
不会终止进程。

## 定义 schema（version 1）

```yaml
version: 1
vars:
  target: ./...
env:
  KS_ROOT: "."
env_paths: ["/opt/ks-bin"]
tasks:
  check:
    desc: 检查项目
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

YAML、JSON、TOML 解码为同一模型。未知字段、重复 key、类型错误、路径越界、同一步多个
动作、缺失引用和负 timeout 都会在动作执行前报错，并给出源文件与字段路径。

`timeout` 为时长字符串（`500ms`、`2s`）。Task 字段：`desc`、`if`、`platform`、
`deps`、`dir`、`timeout`、`clean_env`、`env`、`env_paths`、`vars`、`dynamic_vars`、
`steps`。Step 字段：`name`、`if`、`platform`、`dir`、`timeout`、`ignore_error`、
`env`、`env_paths`、`vars`、`dynamic_vars`，并在 `exec`、`shell`、`file`、`task`、
`host` 中且仅在其中选择一个。

## 动作模型

| 动作 | 语义 |
|---|---|
| `exec` | 可执行程序 + argv，不做 Shell 解析，不隐式分词 |
| `shell` | 显式指定 `sh`、`bash`、`zsh`、`cmd`、`pwsh`、`powershell`，各自使用自己的参数契约 |
| `file` | 引用 `files` 中的具名脚本，按 `interpreter.program` + `prefix_args` + 绝对脚本路径执行 |
| `task` | 顺序调用另一个任务；`args` 替换继承参数，`forward_args` 追加本次请求参数 |
| `host` | 调用 `WithHandler` 注册的宿主函数，`New` 之后不可更改 |

## 变量、环境与目录

优先级由低到高：

| 项目 | 顺序 |
|---|---|
| Vars | Definition 默认值 → Task → Step → `Request.Vars` |
| Env | `WithBaseEnv` 快照 → Definition → Task → Step → `Request.Env` |
| PATH | `EnvPaths` 按 Step → Task → Definition 前置到有效 PATH |
| 目录 | `Request.Dir` 或 `BaseDir`，再 Task `dir`，再 Step `dir`；绝对路径保持绝对 |

Task 的 `clean_env: true` 只去掉基线环境快照，显式 `env` 仍然保留。库不会调用
`os.Chdir`、`os.Setenv`，运行期不重新读取进程环境，也不把结果写入磁盘。

模板为单次渲染并带命名空间：`${vars.name}`、`${env.NAME}`、`${args.N}`（从 1 开始）、
`${host.name}`、`${run.task}`、`${run.dir}`、`${run.call}`、`${run.os}`、
`${run.arch}`。未知引用报错，`$${` 表示字面量 `${`。`dynamic_vars` 声明的动态变量在
每次任务调用或 Step 内最多求值一次，走相同 Engine、context 与输出上限。

条件使用 [expr](https://github.com/expr-lang/expr)，必须返回 bool，可读取 `vars`、
`env`、`args`、`host`、`run` 以及裸变量名（如 `enabled`）。Task 条件在 deps 之前求值，
Step 条件在该步动态变量之后、动作之前求值。Inspect 不执行动态变量命令，相关字段标记为
Deferred。

## 执行、取消与输出

- deps 与 task call 串行执行，按出现次数执行，不隐式去重，不隐式重试。
- 静态环和过深调用链由 `New` 在首个动作前拒绝，并给出完整环路径；运行期还有调用展开上限。
- 有效截止时间取父 context 与 Task/Step timeout 中的最早值。取消会停止调度新步骤，
  `Result.Status` 为 `canceled` 或 `timed_out`，返回的 `RunError` 保留
  `context.Canceled` / `context.DeadlineExceeded`。
- 默认引擎拥有整棵进程树：POSIX 子进程独立进程组，Windows 子进程加入 Job Object。
  取消时先请求退出，超过宽限期后强制终止。若拥有机制无法建立（例如 CI runner 已有的受限
  Job Object 拒绝嵌套加入），引擎退化为按活动父进程链终止整棵树，仍然会清理子孙进程而不是
  只杀父进程。需要严格保证时可用 `WithEngine(ProcessEngine{TreeKill: TreeKillRequired})`
  让动作直接失败而不再回退。
- `IO.CaptureLimit` 限制每路流收集的字节数。设置 writer 时会同时转发与收集；超过上限后
  停止收集并标记截断，但继续排空，避免子进程阻塞。writer 返回错误会终止该动作并作为 IO
  错误上报。`CaptureLimit` 为 0 表示只转发不收集，负值非法。
- `ignore_error` 只容忍已启动进程的非零退出码或 Handler 业务错误，不容忍启动失败、取消、
  超时、输出上限、IO 或配置错误。

## 观察执行

```go
runner, err := taskrun.New(def, taskrun.WithObserver(func(event taskrun.Event) {
	log.Printf("%s task=%s step=%s depth=%d status=%s reason=%s err=%v",
		event.Kind, event.Task, event.Step, event.Depth, event.Status, event.Reason, event.Err)
}))
```

事件覆盖运行（`run_started`、`run_finished`）、任务调用（`task_started`、
`task_skipped`、`task_finished`）与步骤（`step_started`、`step_skipped`、
`step_finished`），并携带 call id、动作类型、有效目录、退出码与分类后的错误。只有真正
执行的任务才会报 `task_started`，被跳过的任务只报带原因的 `task_skipped`。观察器不能改变
调度、也没有返回值；同一次运行的回调按顺序到达，并发运行可能并发调用观察器，宿主需自行
同步并快速返回。Inspect 与 DryRun 不产生事件，事件也不携带子进程输出，需要输出请读
`Result`。

## 状态与错误

`Result.Status` 取值为 `succeeded`、`succeeded_with_warnings`、`failed`、`canceled`、
`timed_out`、`skipped`、`dry_run`，与返回的 error 不会互相矛盾。`Result.Tasks` 记录每次
任务调用（含跳过原因），`Result.Steps` 记录动作类型、启动状态、退出码、输出、截断与错误。

失败返回 `*taskrun.RunError`，包含 `Kind`、task、call id、step、source，并支持
`errors.Is`/`errors.As` 匹配 `ErrNotFound`、`ErrInvalidRequest`、`ErrInvalidDefinition`、
`ErrDependencyCycle`、`ErrExpansionLimit`、`ErrStart`、`ErrExit`、`ErrHandler`、
`ErrOutputLimit`、`ErrIO`、`context.Canceled`、`context.DeadlineExceeded`。
`ErrNotFound` 只表示根任务名不存在；依赖缺失属于 InvalidDefinition，加载失败由 loader 报出。

`Inspect(ctx, req)` 与 `Request{DryRun: true}` 会校验参数并展开调用图，但不执行任何动作、
Handler 或动态变量命令。

## 加载与发现

```go
def, err := formats.LoadFile("tasks.yaml")           // baseDir 变为绝对路径
files, err := formats.Discover(formats.DiscoverOptions{
	Mode:     formats.Ancestors,                     // 或 formats.Nearest
	Names:    []string{"tasks", ".kite.task"},
	StartDir: dir,
	StopDir:  root,
	MaxDepth: 8,
})
def, err = formats.LoadFiles(files, false)           // false 表示冲突即报错
```

发现始终显式：名称、起始目录、停止目录和层数由调用方给出，不会读取描述范围之外的内容。
`nearest` 遇到第一层匹配即停止；`ancestors` 每层最多一个文件，按根到近层收集。合并时同名
任务或脚本默认报错，启用 `override` 后后者整条替换；`vars`、`env` 按键覆盖，来源记录在
`Definition.Sources`。

`formats.LegacyMap` 负责把 Kite 历史任务结构转换为新模型，细节见
`docs/kite-migration.md`。命令 alias、extension、plugin 与系统命令兜底仍归 Kite 所有。

## 平台与版本

Go 1.23 及以上，MIT 许可。运行平台支持 Windows、Linux、macOS；Shell 必须显式指定，库不
假设同一份 Shell 源码跨平台等价。依赖仅 `expr-lang/expr`、`goccy/go-yaml`、
`BurntSushi/toml`。

## 开发命令

```bash
make check        # gofmt 检查、build、vet、测试
make test-go123   # 用声明的最低 Go 版本跑测试
make test-race    # 需要 cgo 与 C 编译器
make cross        # linux 与 darwin 构建
make cli          # 用 examples/basic.json 跑示例 CLI
make examples     # 运行 Go 示例
```
