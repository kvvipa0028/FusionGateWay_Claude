# WP-15-BUDGET-01：任务提交与共享预算事务

状态：本子工作项 `done`，父工作包 WP-15 继续 `in_progress`。

预览新增 `budget`，只接受 max_calls/max_reworks，不接受 used counters。项目可通过受信任配置指定默认上限；未指定时预览明确展示 50 次模型调用、最多 1 次返工。任务可以在预览时调整为 1–1000 次调用、0–1 次返工。这里是上限，不会在提交时发起调用或预扣供应商余额。

Submit 从服务端 receipt 取原预算，与 task、plan snapshot、project-scoped idempotency 和 created event 一次事务写入。任一写入失败全部回滚。预算也是幂等 payload 的组成部分；同 key 更改预算返回 conflict，成功提交的重试不会重新初始化预算，也不会重置已经消耗的调用/返工次数。

新增管理读取 `GET /control/v1/tasks/{id}/budget`，返回 max_calls/max_reworks/used_calls/used_reworks；沿用强鉴权、禁止 URL key 和任意 workspace path 的边界。阶段切换、API 重启和新预览不会增加共享预算；实际扣数仍由既有 Scheduler/Dispatcher 在每次准许模型调用前完成。

Store.CreateRequest 的 Budget 是可选追加字段、nil 时 JSON 省略，保持旧内部创建记录的幂等 payload。生产任务提交组件始终传入有效预算。既有无预算记录不被悄悄升级，Scheduler 仍按 budget_missing 阻止执行；没有 schema 迁移或覆写旧快照。旧 PREVIEW-01 packet 是其历史提交的证据快照，本记录覆盖新增合同。

## 验证

新合同缺失先 RED；补上字段后再次 RED，真实数据库故障证明 budget insert 失败时旧实现仍提交 task，非法预算被接受、预览遗漏上限。实现事务和验证后通过。

故障注入在 SQLite budget insert 处 abort，随后核对 tasks/plan_revisions/idempotency/events/task_budgets 均为零，再移除故障后用同 key 正常提交。消费测试通过实际 Store.StartReserved/ConfirmStarted/ReserveCall/ReserveRework 接口累计 3 次调用、1 次返工，提交重试保持计数与事件数不变；这是存储/调度合同测试，没有真实模型请求。

17 个 API tests 与 3 个新增 submission tests 通过；完整 Fusion tagged race 162 个 top-level tests 通过、2 个父进程 helper skip。Fusion CLI/GUI 编译及全仓 Fusion/nogui vet exit 0。全部使用隔离 HOME/XDG 和合成身份。预算设置不代替真实额度、计费或 Native 全部调用出口证明。

生产 Handler 注册、If-Match 计划修订、阶段暂停/取消/继续、预设/额度、SSE 与 UI 仍待后续 WP-15/WP-16 实施，父包未完成。
