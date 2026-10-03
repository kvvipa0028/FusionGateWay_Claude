# WP-09

已实现 Magpie Quota JSON、Codex 原生 rate-limit JSON、GLM 原生 limits 的纯数据适配与有界刷新 Broker。原始已知窗口、source/observed/received time、身份 generation、物理池与单位分别保存；空字段、零额度、缓存、unknown/stale/auth_required/unsupported 与余额/credits/MCP 分开。共享池别名不相加，迟到身份响应不进入缓存，错误不回显凭据。

14 个 quota 测试、79 个 Fusion 顶层 race 测试通过，父进程 helper 跳过 1 项。quota vet 和 Draft202012 schema/六个 Go 导出快照校验通过。RED→GREEN 日志与 schema 样本随包保存。

本包无真实认证、网络查询或凭据写入。真实 Native Reader 与额度查询准入仍待 WP-12–WP-14；最终验收仍为 not_run。
