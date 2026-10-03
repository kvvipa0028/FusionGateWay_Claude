# WP-12 进度与验证证据

状态：`in_progress`。本次完成子工作项 `WP-12-PROTOCOL-01`：锁定 Codex 0.160.0 的协议客户端与私有 stdio 传输。**完整官方 Runtime Adapter、真实登录与严格执行准入尚未完成。**

## 已交付

- `internal/fusion/runtime/codex/`：initialize/initialized、account/read、thread/start/resume、turn/start/interrupt 和原生事件状态机。
- 冻结模型、effort、账号/workspace/credential identity、generation、项目 cwd 和独立 Codex home；每次 RPC 前后核对当前身份。
- initialize 显式选择 gateway OAuth，禁止恢复自动登录；不调用登录或任意管理方法。私有登录和续期尚待受信任管理层实现。
- 主模型、子 Agent、自动压缩、MCP、动态工具等控制无法证明时返回 `unverified`；未知 item 与只读阶段的 fileChange 进入 `execution_uncertain`。
- Native 启动确认丢失后禁止重放。只有匹配 thread/turn 的原生 completed/failed/interrupted 才产生协议终态；文本、进程退出、取消 RPC 确认都不能代替它。
- stdio 请求 ID 配对、重复/混合字段拒绝、有界帧/累计输出/通知队列、超时关闭、权限请求拒绝。真实发现并修复最终响应与 EOF 竞争导致确认丢失的问题。
- `testdata/native-0.160.0/`：完整 synthetic DTO vectors 与 13 个本地 Native JSON Schema 的指纹；未导入真实账号或凭据。

## 实际验证边界

详见 `test-results.json`、原始 RED/GREEN 日志与构建报告。Native schema 校验通过仅证明 fixtures 与锁定版本的结构匹配。

`native-handshake.json` 记录一次无账号、无网络、无 fork 的真实 Native 启动检查：CLI 在任何握手响应前以 exit 1 退出，stderr 为 `Error: Failed to synchronize managed preferences`。已 wait/reap；模型与登录请求数均为 0。该结果是已记录的启动阻断，不是握手成功或生产停止证明。

冻结版本的 [官方 managed preferences 实现](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/config/src/loader/macos.rs) 使用 CoreFoundation 同步 `com.openai.codex` 的受管配置。当前缺少可安全承载该访问且保持凭据隔离的启动合同；本次没有放宽 WP-11 的生产沙箱。

## 未完成

- 将协议会话接入持久化 Store、stage grant、Supervisor 私有交互管道与原生进程终止证明。
- 用户自己的独立 ChatGPT 登录 home、账号/workspace 身份验证、同账号凭据续期与真实恢复验证。
- 所有模型子调用/摘要的冻结目标、预算/额度控制与实际计费路线证明。
- 原生成功/失败/中断、实际网络与子进程的受控验证，以及 WP-17/Gate A 的三路线实测。

T01/T02/T14/T15/T25/T31/T32/T35/T44/T53/T59 的最终端到端状态继续为 `not_run`。已验证协议不代表真实路线已准入；其余独立工作继续实施。
