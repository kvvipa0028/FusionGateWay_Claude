# 任务与执行存储合同 v1

数据库为独立私有目录中的 `fusion.db`，不访问 Magpie 账号库。只支持已验证的本机 macOS/arm64 场景；网络共享、NAS 和多控制进程不在准入范围。数据库及 sidecar 必须为本人私有普通文件，目录不能位于 Git 仓库内或通过软链接指定。`fusion.lock` 的 OS flock 在整个控制器生命周期持有；第二个控制器拒绝启动。锁文件保留，不能删除后创建另一个 inode 绕过现存锁。

## 持久化与事务

schema/migrations 当前为 `migrations/001.sql`：Task、StagePlanRevision、StageRun、RouteRevision、EvidenceRef、idempotency 和带 task 内序号的 Event。版本及迁移 SHA256 校验失败拒绝打开；现存未知数据库不能当成空数据库迁移。WAL、foreign_keys 和 synchronous=FULL 启用，单连接配合控制器锁与事务串行写入。

Create 的幂等键限定在 project 内，payload hash 包含项目、目标及完整快照。相同键和 payload 返回同一个 task；不同 payload 拒绝。数据库事务不会调用 Runtime，也不隐式执行任务。

计划只能插入新 revision；If-Match 必须等于当前 revision，新快照 revision 必须为旧版加一。普通修订保持 required_roles 不变，任何已有 stage_run 的角色（包含活动、未知及已结束 attempt）的绑定均不能改写；尚未开始角色可以修订。ValidateRevision 不写事件，RevisePlan 在同一提交事务重新核对，以拒绝预览后启动的竞态。旧快照、路线版本与证据引用不能覆写。快照 hash 只证明完整性，不能代替可信编译、鉴权和准入。RouteRevision 可以保存草稿元数据，不表示其已获执行准入。

EvidenceRef 绑定 task/run 与两个 SHA256；只作为不可变引用，不表示报告已解析或测试通过。EvidenceGate 在 WP-21 判定真实性与充分性。

## 启动、租约与终态

| 操作 | 前置与结果 |
|---|---|
| StartIntent | task=ready、exact plan revision、目标属于该 frozen binding；事务写 starting、attempt、generation、target 和 start_intent 事件后才允许外部启动 |
| ConfirmStarted | 相同 owner/generation、租约未过期、状态 starting；记录 native session，转 running |
| RenewLease | 相同 owner/generation 且租约仍有效；不能复活已过期租约 |
| CancelIntent | starting/running 转 cancelling，重复取消保持同一意图 |
| Finish | 仅当前有效租约；succeeded 只接受 running；终态不再接受旧事件 |
| 控制器重启 | 所有 starting/running/cancelling 转 unknown；generation 增加，owner 清空，task=needs_review；保留 intent/session/target 和 recovery_unknown 事件 |
| 观察到租约过期 | 原子持久化 unknown 和新 generation，记录 lease_expired_unknown，然后向旧 worker 返回 fenced；时钟回拨不能恢复旧 lease |

首版每个 task 仅允许一个活动 attempt，unknown 也占用该约束。外部进程与数据库不存在原子事务：记录 intent 后进程可能未启动，也可能已写文件但控制器没有收到确认。unknown 必须由 WP-11/WP-24 核对实际进程、会话和工作区后决定下一步，禁止盲重跑。

StageRun 可表达 succeeded、failed、cancelled、interrupted、advisory_only。单阶段 succeeded 只使 task 回到 ready，不能冒充任务验收通过；failed/cancelled/advisory_only 单独保存，interrupted/unknown 挂起核对。设计批准、硬证据与人工接受在后续工作流中分别记录。

task 内事件 seq 由同一事务分配，从 1 连续增长；失败事务不消费序号、attempt 或 generation。过期 owner/generation 和终态迟到事件拒绝。租约最长一次续期为一分钟，调度层负责心跳及总任务预算，不能用续租重置预算。

## 验证边界

已验证重复/并发提交、If-Match、快照历史、唯一活动 attempt、事件回放、事务故障回滚、取消终态、私有文件和控制器锁。使用隔离 Go 测试子进程建立启动确认与合成文件副作用后 SIGKILL；重开数据库保留副作用、原生 session 和未知状态，未新增执行 attempt。已验证租约过期后时钟回拨仍拒绝旧 worker。

这些证据不证明真实模型请求、Runtime sandbox、实际写进程停止或系统备份恢复；后续 Gate 必须另行验证。迁移或回滚需先停止派单、备份整个一致性数据库及 artifact，再恢复完整版本，不能只替换 binary。

## WP-10 schema 2 扩展

新增 `controller_policy`、`task_budgets`、`reservations`。001 checksum 保持不变，002 checksum 单独保存；从 schema 1 在事务内升级，未知版本/校验不符拒绝打开。

启动 intent、全局/物理额度池/项目写锁预留同事务提交。协议终态后预留继续 held；同任务下一阶段也须等待可信进程退出证明。恢复 unknown 继续占用，不退款、不自动释放或重放。调用/返工计数与事件原子持久化，共享任务上限。迁移前备份；向 schema 1 回退需恢复对应数据库备份，不能仅回滚 binary。
