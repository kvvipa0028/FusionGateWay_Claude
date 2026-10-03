# WP-15-CANCEL-STORE-01 整项任务取消存储基础

状态：`done`，父 WP-15：`in_progress`。BASE：`856cca102a6c8590fca1af7454ad8bfcf2950236`。

补足尚未启动和已暂停的整项任务取消：CancelTask 以完整 TaskVersion 原子条件提交。空闲且全历史执行已可信停止时 cancelled / generation +1；活动执行同事务提交 run cancel_intent 与 Task cancelling，保留 generation，等待原 owner 真正停止。暂停可升级为整项取消；重复当前意图只读，不重复事件、不重置预算和快照。

Finish 保留 cancelling；协议终态不是进程停止证明。可信 ReleaseReserved 在同一事务中释放预留、写事件、检查全任务历史 quiescence 并转 cancelled；未证明的旧执行/unknown/interrupted 仍需 needs_review。未知不能接管或普通继续；取消不表示文件副作用已回滚。保留现有直接阶段取消兼容性，schema 5/001–005、旧提交和启动 hash 不变。

新增 **10 个 Store race 测试通过**：空闲/paused、running/starting、终态 held、暂停升级取消、旧版本/owner/lease、调用消耗与快照、启动竞态、意图与收尾事务故障回滚、重复 proof、重启 unknown、legacy 和当前 proof 无法代替历史证明、非法条件/overflow。最初缺方法编译 RED 保留；实现后全部 GREEN。

全量 Fusion race **341 PASS / 9 SKIP / 0 FAIL**；CLI/GUI build 及全仓 tagged vet exit 0。固定 Claude Code 2.1.287 两个既有 Controller Native 成功/暂停回归通过，合成上游每例 1 HTTP/Permit，真实 wait/StopProof/release；未调用新 CancelTask，不宣称整项任务取消的 Native 验收。真实模型与额度查询为 0，私有 key 未进入源码或证据，Jev off。

合同见 [task-cancel-store.md](../../../contracts/task-cancel-store.md)。Controller/鉴权 HTTP task cancel、OpenAPI、实际整项 Native 取消是下一依赖工作；产品 bootstrap/listener/GUI、真实准入和完整 30 工作包/60 类最终验收仍待完成。本子项完成不等于完整任务取消或 WP-15 完成。
