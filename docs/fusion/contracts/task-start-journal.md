# 原阶段启动请求的持久记录

组件 WP-15-START-JOURNAL-01。完成 Store 基础；后续 [API/Native 通路](task-start-api.md)及[原详情区恢复页面](task-start-ui.md)已接入。Store 基础组件当时未改原 Magpie UI、CSS 或 HTTP 合同。记录原请求身份，不授予 Native 启动、恢复、停止或验收权限。

## 原请求与可信来源

`PrepareStart(key, StartIdentity)` 从 Store 的当前 ready Task 和对应历史 StagePlan 构造 StartDraft：原 key、task_id/role/plan_revision/generation、原 Task 和完整冻结计划。新记录须精确匹配当前 Task 条件及计划中的角色，Restore 非 nil 拒绝；不接受调用方传来的 Task、Plan、账号或执行 target。准备不会创建 run、start_requests、reservation 或执行事件，不扣调用预算。

记录存于已有私有 0700 状态根的 0600 SQLite 文件。key 是请求幂等身份，不能用作管理认证；不保存账号凭据或 Native session。每项目最多一个 prepared/committed 记录，另一个项目可独立准备。相同 key/identity 的准备重试返回原不可变草稿及当前状态；全局默认、Task 后续状态或计划变更不能重新解释该请求。返回的计划为独立反序列化副本，调用方修改不影响存储。

| 状态 | 允许转移 | 意义 |
| --- | --- | --- |
| prepared | 原 StartReservedOnce 事务成功 → committed | 原请求已保存，尚未存在该 key 的 run 回执 |
| prepared | 明确 abandon 且没有 start_requests 映射 → abandoned | 此后原 key 启动和查询均冲突拒绝 |
| committed | 精确原 identity 与原 run_id 的 acknowledge → acknowledged | 已核对原 run 身份，不能推断执行成功或实际停止 |
| acknowledged / abandoned | 同身份重复读取/同终态解决 | 历史保留，项目可准备新的明确请求 |

`PendingStart(project)` 只读 prepared/committed；`LookupStartJournal(key, identity)` 可读取精确终态历史。完整 JSON、canonical hash、身份索引、Task 不变字段、历史计划 hash 及回执关联均核验。损坏冻结 JSON、关联 Task 缺失、prepared/abandoned 却已有 start_requests 映射均冲突拒绝；不会把已存在的损坏记录当作没有记录。

`ResolveStart(key, identity, action, runID)` 只允许 acknowledge/abandon。prepared 无法确认，已 committed 无法封存，替换 run ID 或身份拒绝。acknowledge 不改 Task/Run/预算/事件，不释放 held reservation，不产生 StopProof。unknown 执行仍为 unknown，需要原执行合同的人工核对。

## 与现有启动的事务关系

现有 `LookupStart` 在 key 有 journal 时先核对原身份及封存；没有 journal 的既有调用语义保持。StartReservedOnce 在同一事务创建 run、generation/attempt、start_requests、reservation 和执行事件，并由受约束的 SQL trigger 将匹配 prepared 记录更新为 committed/run_id。任一步失败整体回滚，原 prepared 记录保留；Created=false 的原 run 重读不授予再次调用 Runtime 的权限。

封存与 StartReservedOnce 共享 Store/SQLite 事务锁：先封存则迟到启动拒绝；先写入 run 则封存拒绝。数据库还保留不可变草稿、合法状态转移、精确回执关联和拒绝封存/变更 key 的约束。终态记录不能删除。没有 journal 的现有 API 启动、resume、旧 start_requests 和幂等回读不改；不从旧 run 或事件猜造已经丢失的历史 UI key。

启动 intent 只检查调用预算并占用 reservation；实际模型请求仍由原 ReserveCall 计数。journal 不改变这一计费边界，也不把恢复读取当作新的模型请求。

## schema 8 迁移与回退

008 新增 start_journals、每项目 pending 唯一索引及约束/事务耦合 trigger。001–007 的文件和 checksum、旧 payload hash、任务提交记录与旧 run 不改。现有 schema 1–7 自动逐步迁移；008/schema/checksum 写入失败时该步骤整体回滚，schema 7 数据保持，可修复阻塞后重试；checksum 漂移及未来 schema 9 及以上拒绝。

升级前停止自己拥有的宿主，等待 owned Runtime 实际退出并核对 reservation；保存关闭后的一致性私有状态备份，包括 fusion.db、私有来源和同属该宿主的状态。备份保持原访问权限，不提交 Git。先在备份副本/隔离 HOME 中验证新二进制可以打开，核对原任务、冻结计划、已用预算和历史回执，再用于原状态根。

schema≤7 的旧二进制不能打开 schema8。回退必须停止新宿主并恢复对应一致性备份；不能改 user_version、删表或继续用旧 binary 写入新库。journal ack 不等于实际停止，不能凭它执行在线数据库回退。

## 验证与后续

12 个新增顶层测试覆盖准备无执行、持久重开、独立副本、原回执原子提交/失败整项回滚、原 unknown run 重读、ack 无执行副作用、封存重开和迟到启动、同/跨项目 pending、改身份拒绝、12轮封存/启动竞争、冻结记录/关联损坏、schema7数据保留、008失败回滚重试与漂移拒绝。未来版本测试随实际支持版本更新至9，旧数据和错误断言保持。

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/start-journal-race.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

需要 Go1.26.3、Xcode SDK、Python3 和已准备的离线依赖缓存。runner 使用临时 HOME/XDG 与环境白名单，fixture 为私有合成数据，未读取日常账号。最终日志/具体 PASS/SKIP 数量与构建 hash 见 [START-JOURNAL-01](../work-items/WP-15/START-JOURNAL-01/summary.md)。明确 opt-in 的真实 Native/账号测试与 helper skip 单独列出，不能视为已通过。

受 Management/可信项目来源保护的 [API 与精确 Native 桥](task-start-api.md)已完成准备/回读/解决原记录，后续[原详情区页面](task-start-ui.md)已消费：先持久准备成功才发送原 start；重新打开窗口仅回读，不自动启动；原 body/key/Task If-Match 继续保持。独立 run 证明与原请求确认未完成时保留身份并禁用新请求，不能用当前配置重编译或新 key 追求成功。浏览器消费链已验证，实际 Native 窗口操作仍未验证；WP-15/WP-16 及整体目标保持 in_progress，Jev off，最终 T01–T60 not_run。

后续 `ReadStartJournal(key)` 为可信消费者返回完整校验的原记录；调用方仍须核对当前项目登记、原 Task 和条件，key 不替代鉴权。
