# 发布与推送清单（2026-09-21）

> 这些步骤需要外部动作授权，本文件只是把命令准备好，未执行任何一步。
> 模块：`github.com/gookit/taskrun`；本机工作副本：`D:/work/inhere/my-tools-dev/gookit2/kscript`。

## 1. 本地目录改名（可选，先做更省事）

本机工作副本目录名仍是 `kscript`（改目录时被本工作区某个进程的文件锁挡住）。两种做法：

```cmd
:: 关闭占用该目录的进程后（IDE、文件监视等）
move /y D:\work\inhere\my-tools-dev\gookit2\kscript D:\work\inhere\my-tools-dev\gookit2\taskrun
```

或者跳过改名，等仓库建好后直接以新名字克隆，再把本地这份删掉。

改名后需要同步两处 `replace` 目标（否则构建会失败）：

- `inhere-tools/kite-go/go.mod`：`replace github.com/gookit/taskrun => ../../gookit2/taskrun`
- `tmp/taskrun-consumer/go.mod`：`replace github.com/gookit/taskrun => D:/work/inhere/my-tools-dev/gookit2/taskrun`

## 2. 创建远端仓库并推送

```bash
cd <taskrun-module-root>
git remote add origin git@github.com:gookit/taskrun.git
git branch -M main          # 当前分支名按实际情况
git push -u origin main
```

## 3. 打 tag 发布 v0.1.0

```bash
git tag -a v0.1.0 -m "taskrun v0.1.0"
git push origin v0.1.0
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

- `go test -race ./...` 与 windows 构建/测试的 CI 日志（`.github/workflows/race-and-windows.yml`）。
- Go 1.23/1.24/1.25/stable 矩阵日志（`.github/workflows/go.yml`，组织模板）。
- 第二真实应用（T10）：项目路径、Go 版本、实际运行 Result。
- 在真实 Kite 配置上以 `script_engine: taskrun` 实际执行一次任务（转换、校验、规划已通过）。
