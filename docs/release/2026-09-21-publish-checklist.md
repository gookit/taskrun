# 发布与推送清单（2026-09-21）

> 这些步骤需要外部动作授权。已获授权并完成的部分在下方标注为「已完成」；未完成的步骤把命令准备好。
> 模块：`github.com/gookit/taskrun`；本机工作副本：`D:/work/inhere/my-tools-dev/gookit2/kscript`。

## 状态（2026-09-21）

| 步骤 | 状态 |
|---|---|
| 建远端 + 首次推送 | 已完成（`origin` = `https://github.com/gookit/taskrun.git`，`main` 已推送） |
| 组织 CI 与新增工作流 | 已完成：`go.yml`（组织模板，仅修了 `matrix.os` 笔误）、`codeql.yml`、`release.yml`，另加 `race-and-windows.yml`（ubuntu `-race` + windows 构建/测试） |
| Windows CI 失败与修复 | 已完成：commit `688c5b5`（分层清理，见设计修订 0.4 / D11） |
| Revive 告警 | 已完成：7 条告警全部处理，commit `2e1e69b`；CI Revive 步骤现为 0 条 |
| 推送 | 已完成：`main` 已到 `94d185d`。推送过程中 `github.com:443` 出现过一段时间不可达（`api.github.com` 一直可用），恢复后推送成功 |
| CI 结论 | 已完成：`action-tests`（Go 1.23/1.24/1.25/stable 四个作业）通过；`race-and-windows` 的 Windows 与 Race detector 两个作业通过 |
| tag `v0.1.0` | 未执行（需授权；第 3 步） |

推送后可这样确认 Windows job：

```bash
gh run list --workflow=race-and-windows.yml --limit 3
gh run watch <run-id>            # 或: gh run view --job <job-id> --log-failed
```

## 1. 本地目录改名（可选，已降级为收尾项）

本机工作副本目录名仍是 `kscript`（改目录时被本工作区某个进程的文件锁挡住）。两种做法：

```cmd
:: 关闭占用该目录的进程后（IDE、文件监视等）
move /y D:\work\inhere\my-tools-dev\gookit2\kscript D:\work\inhere\my-tools-dev\gookit2\taskrun
```

已结论：可以跳过或放到发布之后。模块路径已经是 `github.com/gookit/taskrun`，本地目录名
只影响两处临时 `replace`；按第 4、5 步换成真实版本后，`replace` 会整行删除，目录名就与
构建无关了。现在强行改名会打断使用者已经打开的路径，收益低，故保持现状。

改名后需要同步两处 `replace` 目标（否则构建会失败）：

- `inhere-tools/kite-go/go.mod`：`replace github.com/gookit/taskrun => ../../gookit2/taskrun`
- `tmp/taskrun-consumer/go.mod`：`replace github.com/gookit/taskrun => D:/work/inhere/my-tools-dev/gookit2/taskrun`

## 2. 创建远端仓库并推送（已完成）

```bash
cd <taskrun-module-root>
git remote add origin git@github.com:gookit/taskrun.git
git branch -M main          # 当前分支名按实际情况
git push -u origin main
```

## 3. 打 tag 发布 v0.1.0

打 tag 之前先收尾 CHANGELOG 的版本标题（当前是 `## [Unreleased]`）：

```bash
# 把 CHANGELOG.md 里的
#   ## [Unreleased]
# 改成
#   ## [v0.1.0] - 2026-09-21
git commit -am "docs: mark v0.1.0 in the changelog"
```

发布说明由组织模板的 `Tag-release`（`release.yml`）自动生成：它在 tag 推送后拉取
`chlog`，按 `.github/changelog.yml` 的规则**从 commit 标题**分组（`fix:` → Fixed、
`feat:`/`new:` → Feature、`up:`/`update:` → Update、`refactor:`/`break:` → Refactor，
其余进 Other），并过滤长度不足的标题。本仓库历史里 `style:`、`docs:`、`chore:` 标题会落到
Other 分组，属正常；人读的完整清单以 `CHANGELOG.md` 为准。

```bash
git tag -a v0.1.0 -m "taskrun v0.1.0"
git push origin v0.1.0
```

推送 tag 后确认发布动作：

```bash
gh run list --workflow=release.yml --limit 3
gh release view v0.1.0
```

发布前建议先在 Linux 或装了 C 工具链的机器上跑一次 race：

```bash
make check
make test-race
```

Windows 无 C 工具链时 `make test-race` 会失败，属预期：CI 的
`.github/workflows/race-and-windows.yml` 已在 ubuntu 与 windows runner 上覆盖。

## 4. 把 kite-go 的临时 replace 换成真实版本

```bash
cd inhere-tools/kite-go
go mod edit -dropreplace=github.com/gookit/taskrun
go mod edit -require=github.com/gookit/taskrun@v0.1.0
go mod tidy
go build ./...
go test -count=1 ./pkg/kscript/... ./internal/biz/cmdbiz/
```

## 5. 工作区外正式验收（A01/A02）

```bash
cd tmp/taskrun-consumer
go mod edit -dropreplace=github.com/gookit/taskrun
go mod edit -require=github.com/gookit/taskrun@v0.1.0
go mod tidy
go test -count=1 ./...
go list -deps ./... | grep -c inhere/kite-go   # 期望 0
```

## 6. 还没有取得的证据（发布后补齐）

- 仍未取得的证据：windows job 的重跑结果（需先推送 `688c5b5`）；`go.yml` 组织模板矩阵日志的第一个全绿运行。
  ubuntu `-race` 已两次通过。
- Go 1.23/1.24/1.25/stable 矩阵日志（`.github/workflows/go.yml`，组织模板）。
- 第二真实应用（T10）：项目路径、Go 版本、实际运行 Result。
- 在真实 Kite 配置上以 `script_engine: taskrun` 实际执行一次任务（转换、校验、规划已通过）。
