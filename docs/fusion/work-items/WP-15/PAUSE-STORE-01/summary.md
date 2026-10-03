# WP-15-PAUSE-STORE-01 暂停/继续存储基础

状态：`done`，父 WP-15：`in_progress`。BASE：`894f121c22c9ec56816de0aefb3fcb8a2f4184db`。

实现 PauseTask / ContinueTask 的完整 TaskVersion 事务条件，空闲控制递增 generation 防止状态 ABA 后接受旧启动请求。活动暂停保持原 generation，同事务提交 cancelling/cancel_intent 与 pausing，拒绝新模型调用；Stop/Wait 仍由原 owner 执行。该 Store receipt 不授权 Native Start/Resume。

Finish 不能证明进程停止，pausing 等待可信 ReleaseReserved。成功完成且全任务所有历史执行均有终态和 released stop proof 才变 paused；取消/失败/中断、unknown 或任何缺少证明的历史执行需要 needs_review。普通 continue 只解除可靠暂停，不重跑任务、不重置预算、不接管会话。schema 与旧 JSON/idempotency hash 未改变，仍为 5，001–005 保持原样。

新增 11 个 Store race 测试通过，包含并发 pause/start 唯一赢家、旧版本/owner/lease、预算/快照、停止收尾事务故障、重试、重启、无 proof 的 legacy 和 overflow。新增真实 RED 复现“当前停止证明误替代旧执行证明”，补全 quiescence 查询后 GREEN。最初缺方法 RED 中的测试 helper 错名已换回源码的 ConfigureBudget，未放宽生产保护或测试断言。

全量 Fusion race **319 PASS / 8 SKIP / 0 FAIL**；CLI/GUI build 和全仓 Fusion tagged vet exit 0。既有固定 Claude Code 2.1.287 Controller 合成回归验证一次 HTTP/Permit、实际 wait/StopProof/release；该回归未调用新 PauseTask，不宣称 Native 暂停已验证。真实模型/额度查询为 0，私有 key 未进入源码/证据，Jev off。

合同见 [task-pause-store.md](../../../contracts/task-pause-store.md)。Controller/HTTP pause/continue、OpenAPI 和实际暂停停止回归是下一依赖工作；产品 listener/GUI、Native session 恢复与剩余工作包/最终 Gate 继续实施。本子项完成不等于完整暂停/继续或 WP-15 已完成。
