# 当前基线的复用点与后续改动边界

源码基线：`1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`。以下入口已通过当前源码与 codebase-memory 索引核对，新增模块名称来自实施计划。

| 能力/问题 | 当前代码入口 | 后续工作包与处理方式 |
|---|---|---|
| 普通 manual 模型组 | `internal/provider/group.go` 的 `Group.Picked()`、`routes()`；`internal/gateway/gateway.go` 的 `serve()` | WP-04–WP-07：保留普通组行为，为任务冻结具体绑定并增加独立 strict 出口。选择失效时不能调用 `Picked()` 回退 |
| 配置与缓存目录 | `internal/appdir/appdir.go` 的 `Config()`、`Cache()` | WP-02：建立独立产品目录、端口、服务与更新身份；本轮只隔离验证环境，未改应用默认值 |
| 网关调用身份 | `internal/gateway/lan.go` 的 `identifyCaller()`；`internal/access/access.go` 的 `Authenticate()` | WP-06：阶段身份由服务端解析，新增受控入口不能依赖 loopback 或客户端阶段 header 获得权限 |
| 迁移后的订阅 | `internal/provider/migrate*.go` 的 mover、`Moved()`、`KeepRetiringMoved()`；`internal/plugin/` | WP-08、WP-12–WP-14、WP-17：核查实际内置/插件/官方 Runtime 路径，记录版本；只测试旧内置实现不能证明插件请求可控 |
| Jev 分类调用 | `internal/gateway/classify.go`、`internal/gateway/decide.go` 的 `askJev()` | WP-28–WP-29：可选旁路，保留用户 locked；本轮未调用 Jev |
| 官方 Runtime 与诊断 | Codex `app-server`、Grok CLI、Claude Code 作为仓库外实际执行程序；`scripts/fusion/glm-claude-probe.py` 已完成 Claude Code/GLM 连接诊断 | WP-08、WP-11–WP-14：Adapter 仍需实现并验证；诊断不经过旧 Grok 订阅内置/社区插件，也不代表其认证成功 |
| 阶段配置/任务/证据 | 计划新增 `internal/fusion/`，目前不存在 | WP-04 以后按合同实施；不要把原版路由日志当作完整任务或验收记录 |

本轮未改动上述 Go 实现，也没有启用真实账号。原版能够编译及相关测试通过，只证明当前导入基线；不证明 Fusion 严格锁定、账号准入或任务闭环已完成。

以上“本轮”指原始源码准备。2026-10-03 的源码复核见 [source-comparison.json](../work-items/WP-01/source-comparison.json)；GLM 官方 Claude Code 连接诊断单独记录，不追溯改变原始基线结论。
