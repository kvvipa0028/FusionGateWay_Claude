# WP-15-PAUSE-API-01 Controller 与 HTTP 暂停/继续

状态：`done`，父 WP-15：`in_progress`。BASE：`2d4ba1ae3900790a18c111623c51ace20babcc81`。

Controller 新增管理重查的 Pause/Continue：完整 TaskVersion 的 Store 意图先提交，仅取消本控制器同 task/run/generation 的 owned job；HTTP 断开不会撤销该意图。Continue 只解除可靠 paused 的派单冻结，不调用 Native Start/Resume、不重置预算、不接管 unknown。取消/中断副作用仍 needs_review，普通继续拒绝盲重跑。

新增 POST /control/v1/tasks/{task_id}/pause 和 /continue，强 Task If-Match、body 仅 {}、Management/Origin/query/字段/header/当前授权检查。响应包含当前 Task、可选受限 RunView 和 changed；失败停止保留已提交的安全 receipt/409，固定错误不回显私有 Native 数据。202 是事务接受 pausing 的状态，Task/header 是同一次当前读取，即使处理期间实际完成仍不丢失原意图。

实际 RED 发现 Backend.Start 返回 Handle 前 lifetime 已被 Pause 取消但后到 Handle 没有收到 Cancel。Start 现对已取消 lifetime 的精确后到 Handle 交付 Cancel，避免 backend 忽略启动 Context 时遗失停止；6 个 Controller、6 个 API 测试通过，现有默认 fixture 断言保持。no_stop 仅是新增 API fixture 模式。

新增固定 Claude Code 2.1.287 inflight 暂停：一次合成 HTTP/持久 Permit，Controller Pause→owned Native Cancel→真实 Wait/StopProof→同 Adapter Release→needs_review。Continue 和旧 start 都不重跑，不退款/重复请求。连同既有成功 Native 回归显式 **2 PASS**。真实模型和额度调用为 0，fixture route/账号/计费/额度不提升为真实准入，Jev off。

最终 full Fusion tagged race **331 PASS / 9 SKIP / 0 FAIL**，CLI/GUI build 和 full tagged vet exit 0。新增测试 13 个（Controller6/API6/Native1）；九项 skip 是六个 Native opt-in（其中两个本轮独立验证）、一个真实额度 opt-in 与两个 helper。官方 OpenAPI 校验通过 **23 paths / 27 operations / 43 schemas / 44 Handler samples / 6 negative schema cases**；样本包含不确定暂停意图的409安全 receipt、实际本机HTTP与SSE。

schema 5 /001–005 migration 与旧 CreateRequest/StartIdentity hash 不变。证据源码/packet hash、原 RED 和 GREEN 日志保留。私有 key 不进入源码/样本/日志。运行与可复制复核见 [task-control-api.md](../../../contracts/task-control-api.md)。

产品 bootstrap/listener/GUI、实际账户路线准入、Native session/检查点恢复和有限返工、剩余工作包与最终 Gate 未完成；本项不宣称 WP-15 或整个产品完成。
