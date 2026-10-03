# 任务暂停/继续的存储合同

本项为内部 Store 基础，尚未增加 Controller 方法、HTTP pause/continue endpoint 或产品 UI。现有 Handler OpenAPI 路径保持原样。调用方必须是已鉴权的可信控制器；owner 来自该控制器，不能由 HTTP body 或 Worker header 提供。

`TaskVersion` 包含 plan revision、generation 与 state，全部在写事务内与当前 Task 比较。单独计划 revision 不能代替 Task 条件。版本不符返回 ErrConflict；非法或溢出条件返回 ErrInvalid；未知/缺少执行停止证明返回 ErrPauseReconcile。PauseTask 的 TaskControlReceipt 返回 Task、可选待处理 Run 和 Changed；这些内部类型不能直接序列化成公开响应，Run 含私有 owner/session 等，HTTP 层仍须使用受限 view。

| 当前条件 | PauseTask / ContinueTask 行为 |
|---|---|
| ready，所有历史执行已可信释放，或尚无执行 | PauseTask 将 Task 改为 paused，generation +1，写 task_paused；不启动/取消 Runtime |
| running，存在同 generation 的受当前 owner 管理执行 | PauseTask 同事务写 cancelling 与 cancel_intent，并将 Task 置为 pausing / task_pause_requested；generation 保持，等待原 owner 实际停止 |
| ready，但协议已终态的当前执行仍 held | PauseTask 置 pausing，返回待等待的终态 Run，不能据协议结果声称进程已停止 |
| pausing，当前执行仍待停/释放 | 同版本 PauseTask 只读已有意图，不重复写事件；ContinueTask 返回 ErrPauseReconcile |
| paused，全部历史执行已可信停止/释放 | ContinueTask 改为 ready，generation +1，写 task_continued；不调用 Start/Resume、不重置预算 |
| 当前 paused 再 pause，或当前 ready 再 continue | 只读返回 Changed false；使用最新完整 TaskVersion |
| needs_review、unknown、过期租约、其他 owner | 不接管、不重跑、不释放预留；过期租约仍按原 fencing 合同持久化 unknown |
| completed、failed、cancelled、advisory_only 等非可继续任务状态 | 不通过普通控制动作变回 ready |

空闲 pause/continue 的 generation 增量关闭 ABA：即使状态回到 ready，暂停之前的 StartIdentity.ExpectedGeneration 也不能创建新 intent。运行期间不增加 generation，避免阻止原 owner 完成取消与实际退出；同一 generation 的 state 改变仍反映到 Task strong ETag。旧启动映射的只读重读合同不变，不能从旧 receipt 再次执行。新意图依旧需要当前状态、精确 generation、冻结 Target 与实际 route/额度/权限准入。

## 从暂停中结束

PauseTask 本身只提交停止意图，不调用 Handle.Cancel/Wait。任务必须等待 Controller 执行真实停止并通过 Adapter/Scheduler 的独立 StopProof 验证，之后才由 ReleaseReserved 处理预留。

Finish 只有协议结果，不提供进程停止证明。pausing 下的 succeeded/failed/cancelled/advisory_only 结果保留 pausing，直到真实 release；interrupted 立即转 needs_review，并保留 held，避免丢失 Handle 时暗示可继续。RenewLease 仍允许原 owner 给 cancelling 执行续租以完成停止，新 ReserveCall 因 run 非 running 被拒绝。

ReleaseReserved 中预留释放、reservation_released、Task 收尾及其事件同事务：当前 run succeeded 且该任务所有历史 run 均为终态、有 released 预留及 stop_proof_hash 时，Task 转 paused；其他结果或早先执行缺少停止证明时转 needs_review / task_pause_requires_review。当前执行的 proof 不能证明另一执行已停止。故障写入回滚全部状态/事件；相同 proof 重试不重复收尾。

取消、中断或失败之后的“继续”不能作为盲重跑许可。恢复 Native session、核验工作区副作用/检查点、改绑已执行角色、新 attempt 与有限返工仍需 WP-24 及相应 Runtime 能力；当前 Native Resume 的 unsupported 保持原样。已成功阶段被暂停后解除暂停，也只恢复后续派单资格，没有重放该阶段。服务重启时：空闲 paused 保留；活动 pausing 执行按原 recover 变 unknown/needs_review，held 和消耗预算保持，不自动完成暂停。

## 兼容性与验证

未增加 schema 或 CreateRequest/StartIdentity 字段，schema 仍为 5，001–005 migration 原样保留。旧提交/启动幂等 hash、Plan/Snapshot、required_roles、Target、预算、预设来源和历史事件不改写。普通未来角色计划修订的规则保持原合同；本项不能改写已有 run 的角色绑定。

11 个新增 Store race 测试覆盖空闲/运行/终态 held、完整版本、旧请求、并发 pause 与启动的唯一赢家、外来 owner/过期、预算与快照、事件失败回滚、release 收尾原子性/重复 proof、重启、legacy 无停止证明、generation 溢出以及当前 proof 无法代替历史证明。最后一项曾实际 RED：当前成功 run 的 release 错将仍有未证明旧执行的 Task 标为 paused；补入全任务 quiescence 检查后 GREEN。

最终全量 Fusion race 319 PASS / 8 SKIP / 0 FAIL；CLI/GUI build 与全仓 tagged vet exit 0。既有固定 Claude Code 2.1.287 的 Controller 合成一次 HTTP/Permit、实际 wait/StopProof/release 回归通过；该 Native 回归没有触发新 PauseTask，不是 Native pause/resume 的验收。本项真实模型与额度查询为 0，Jev off。

证据见 [PAUSE-STORE-01](../work-items/WP-15/PAUSE-STORE-01/summary.md)。后续必须把 Store 意图接到持有 Handle 的 Controller、鉴权/完整 Task If-Match 的 HTTP 控制、OpenAPI、真实合成取消验证和 GUI 后，才可声明完整暂停/继续接口完成。
