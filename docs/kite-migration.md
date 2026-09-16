# Kite 迁移说明

独立库只负责任务定义、加载、条件、执行和结果。Kite 的 alias、extension、plugin、系统命令兜底以及 `gvs`、`paths`、`kite` 应用变量仍由 Kite 适配层负责。

迁移步骤：

1. Kite 读取旧配置文件并保留原发现顺序。
2. 将旧 `Scripts` map 通过 `formats.LegacyMap` 转成 `kscript.Definition`。
3. 将 `AppendVarsFn` 结果放入 `Request.Vars` 或宿主专用 Handler 输入。
4. 继续由 `RunAny` 先处理 alias 和 extension，再调用独立 Runner。
5. 只有根任务返回 `kscript.ErrNotFound` 时才允许继续系统命令兜底；加载和执行错误必须直接返回。
6. 迁移期间保留旧 Runner 回退点，完成 fixture 对照后再删除旧实现。

独立库不自动解释 Kite 私有的 `$@`、`$*`、`@task:` 或插件命名空间。Kite converter 必须把这些语义转换成明确的 Args、TaskCall 或 Handler，避免把 Kite 历史协议写入公共库。
