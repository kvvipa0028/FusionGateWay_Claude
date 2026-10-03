# 整项任务取消的存储合同

本项提供内部 `Store.CancelTask(id, TaskVersion, owner)`，补足尚未启动、已暂停及有执行记录的整项任务取消。公开的阶段 `/runs/{run_id}/cancel` 不等于整项任务取消。Controller、HTTP `/tasks/{task_id}/cancel` 和产品 UI 将在后续接线；当前存储方法不注册接口、不调用 Native。

完整 TaskVersion 的 plan revision、generation、state 在同一写事务内核对。失配 ErrConflict，非法/溢出 ErrInvalid，未知执行或缺少停止证据 ErrCancelReconcile；活动 run 继续使用原 owner/generation/lease fencing，过期仍持久化 unknown/needs_review。owner 由可信 Controller 提供，不允许公开请求指定。

| 当前状态与证据 | 行为 |
|---|---|
| ready / paused / failed / advisory_only，全部历史执行有终态、released 预留及 stop proof，或无执行 | Task 改为 cancelled，generation +1，写 task_cancelled；不启动 Runtime |
| cancelled，全部历史执行已停止释放 | 同当前版本重复取消只读 Changed false，不增 generation 或事件 |
| starting / running / cancelling 的同 generation、当前 owner 执行 | 同事务写 run cancelling / cancel_intent 和 Task cancelling / task_cancel_requested；保留 generation，等待原 Controller 取消并实际退出 |
| 当前协议已终态，但预留仍 held | Task 改为 cancelling，返回待等待的 Run；协议终态不能代替进程停止 |
| pausing | 整项取消取代任务暂停意图，改为 cancelling；已提交 run cancel_intent 不重复写入 |
| cancelling，执行仍待停止 | 当前版本重复取消返回原 Run，Changed false；不释放预留 |
| needs_review、unknown、generation 不一致或缺少历史 stop proof | 拒绝推断停止或接管；保留已有待核对状态与预留 |
| completed 等其他状态 | ErrConflict；不改变任务验收结论 |

空闲取消递增 generation，阻止旧启动请求。运行中保留 generation，使原 owner 能 Finish、Wait 和提交真实 StopProof；run 非 running 后 ReserveCall 拒绝新调用。普通 Pause/Continue 无法将 cancelling/cancelled 变为 ready，原启动幂等映射仍只能只读重读，不可重新执行。

## 停止收尾与恢复

CancelTask 的 receipt 包含 Task、可选待处理 StageRun、Changed。StageRun 含私有 owner/session，不能直接序列化为公开 JSON。receipt 是取消意图，不是 Handle.Cancel/Wait 的证据，也不是文件副作用已回滚的证明。

Task cancelling 下的 Finish succeeded/failed/cancelled/advisory_only 均保留 cancelling。interrupted 立即变 needs_review，并保留 held；不能用丢失 Handle 的协议结果声称任务已停止。

可信 ReleaseReserved 在同一事务中释放预留、写 reservation_released、检查全任务历史执行，并收尾 Task：全部历史 run 都为终态且都有 released 预留及 stop_proof_hash 时改为 cancelled / task_cancelled；否则为 needs_review / task_cancel_requires_review。当前 proof 不能证明另一执行已停止。协议终态、当前 run 的释放、整项任务的停止证明是不同证据；现有直接阶段取消语义保持兼容。

收尾失败回滚预留和 Task/事件；相同 proof 重试只读，不重复写事件。不退还已用调用/返工次数，不改快照、Target、required_roles、预算、预设来源和历史记录。Cancelled 表示停止后续派单，不表示代码变更已回滚或任务通过验收。

重启时空闲 cancelled 保留；活动 cancelling 执行经原 recover 变 unknown/needs_review，预留和预算保持，不重放。已协议终态但 held 的执行仍不能自动认定已停止。检查点、工作区副作用核对和 Native session 恢复属于后续工程闭环。

## 验证边界

schema 仍为 5，001–005 migration、现有提交/启动 payload 与幂等 hash 未改变。Task wire state 本来就是字符串，不新增请求字段或外部停止证明标志。

10 个新增 Store race 测试覆盖空闲/paused、运行/starting、协议终态 held、暂停转取消、旧版本/owner/lease、消耗预算和快照、启动竞态、取消意图及可信释放收尾故障回滚、重复 proof、重启、legacy 无证明、当前 proof 不能代替历史执行、非法条件与 generation 溢出。

全量 Fusion race **341 PASS / 9 SKIP / 0 FAIL**；CLI/GUI build 与全仓 tagged vet exit 0。固定 Claude Code 2.1.287 两个既有 Controller Native 成功/暂停场景通过，真实进程 Wait/StopProof/release，使用合成上游；它们未调用新 CancelTask，不能当成整项任务取消的 Native 验收。本项实际模型调用和真实额度查询为 0，Jev off。

证据见 [CANCEL-STORE-01](../work-items/WP-15/CANCEL-STORE-01/summary.md)。重跑测试请按 [task-control-api.md 的隔离验证步骤](task-control-api.md)准备私有 HOME/XDG 和固定 Native；Store targeted 将包参数改为 `./internal/fusion/store`，测试选择器改为 `^TestTaskCancel`，仍须使用 `fusion,nogui` tags 与 Go 1.26.3。产品注册、整项 Controller/HTTP 取消接线及最终验收继续实施。
