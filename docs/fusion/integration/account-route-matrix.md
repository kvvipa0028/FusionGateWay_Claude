# 三条候选路线及准入边界

2026-10-03 本机核对。本人授权所有者为 local_user；真实账号、workspace 和地区在隔离官方登录后核验，不读取日常认证。目录中的候选仅用于展示和后续准入，不可直接执行。

| 候选 | 宿主与本机版本 | 认证 / 冻结费用类型 | 地区 | 生成 / 额度查询 |
|---|---|---|---|---|
| codex-chatgpt | Codex app-server / 0.160.0 | 本人官方 OAuth / subscription | 尚未核验 | 均 unverified；私有登录待完成 |
| grok-subscription | Grok Build CLI / 1.0.48 | 本人官方登录 / subscription | 尚未核验 | 均 unverified；私有登录待完成 |
| glm-cn-claude | Claude Code / 2.1.287 | 本人 API key / coding_plan | CN，用户确认 | 连接诊断通过；两项准入仍 unverified |

OpenAI 官方区分 ChatGPT 订阅登录与按量 API key，CLI 支持浏览器登录；本项目不自动改用后者。[官方认证说明](https://learn.chatgpt.com/docs/auth)

Codex app-server 提供初始化、登录、执行事件及额度读取接口；目录和执行、额度应分别核验。[官方 App Server 文档](https://learn.chatgpt.com/docs/app-server)

Grok 官方 CLI 提供 login 和 ACP stdio；安装及有目录不代表本人权益已核验，X/订阅身份以官方登录读回为准。[官方 CLI Reference](https://docs.x.ai/build/cli/reference)

GLM 官方 FAQ 指定 Claude Code 的 CN Anthropic Base URL，且将 Coding Plan 限于指定工具；费用明细中的抵扣资源包可用于核验实际账单路径。本次 key 及工具由本人指定，不能仅凭成功响应标记计费已核验。[官方 Coding Plan FAQ](https://docs.bigmodel.cn/cn/coding-plan/faq)

subscription、coding_plan、普通 API、credits、余额与 MCP 分开。当前未批准普通 API、充值或额度重置，也没有公共模型代理用途。GLM 的模型/MCP 是否共享池按官方数据记录，不能按本地 token 累计猜测；不自动使用搜索/MCP 工具。

原版 Magpie 内置与社区插件不是本项目官方 Runtime 路线。Grok 已有 mover，插件可接管旧请求、认证与用量；仅验证旧内置不能证明插件请求可控。Fusion 当前关闭这些旧执行入口。GLM 的 planquota 有 CN/Global 查询来源映射；后续独立只读宿主已用登记 key 完成一次真实查询，但 Complete=false、Pool.Verified=false，仍未授予生成准入，见 [GLM 额度宿主证据](../work-items/WP-15/GLM-QUOTA-HOST-01/summary.md)。对应源码：internal/provider/migrate.go、migrate_side.go、planquota.go；执行边界见 ingress-auth-map。

本机三个命令的版本和 executable SHA256 已记录在 capability-matrix.json；哈希与版本不证明发布者签名、严格锁定、sandbox 或账单。Codex 临时 HOME 的 PATH helper 警告已保留，版本命令退出码仍为 0。没有安装、更新 CLI 或启动生成会话。

2026-10-05 已补充三条固定 Runtime 的真实官方发布内容及 Apple Developer ID 核验，见 [发布者核验](runtime-publisher-verification.md)。原 2026-10-03 inventory 的未核验字段是历史记录；新的报告没有读取认证，也不证明账号、费用、额度或工程 Smoke 已完成。[当前真实接入矩阵](live-matrix.md)。

Registry 默认不准入任何路线；升级需可信 Adapter 验证报告及完整 route hash，绑定模型、身份、工作区、费用、effort、Runtime、transport 和所有调用控制。未知地区必须登记新 exact revision，不能修改既有候选。撤销的 revision 不重新准入；Resolve 每次验证证据当前有效性。生成证据不授予额度查询权。
