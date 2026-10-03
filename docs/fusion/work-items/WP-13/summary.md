# WP-13 进度

状态：in_progress。已完成 [PROTOCOL-01](PROTOCOL-01/summary.md)：固定 Native headless fixture、辅助调用/工具声明调查、有界协议观察器及回归。

交付中的 grok-adapter 尚未完成：生产启动与 stdout/stderr/产物连接、私有认证、生产完整模型调用许可与额度/计费、准确 Resume、取消后不继续写以及真实路线准入仍待验证。当前 `Session.Resume` 明确 unsupported，未注册任何 Grok Worker。

Parent 关联 T01/T02/T14/T15/T25/T31/T32/T35/T44/T56/T59 只新增了组件级证据，60 类最终 Gate 全部仍 not_run。Jev off，现有 GLM/Codex 行为和产品执行开关保持原状。

合同：[headless 观察](../../contracts/grok-headless-protocol.md)，[锁定能力](grok-lock-capability.md)。

已完成 [CALLS-01](CALLS-01/summary.md)：每 HTTP 的冻结绑定/ModelAudience/Scheduler 预算、严格文本 SSE 验证和 Native 标题/重试合成诊断。真实 Store/Scheduler 持久化记账通过，但没有生产 NativeForwarder、真实额度准入或受管 Worker。合同：[grok-call-gate.md](../../contracts/grok-call-gate.md)。
