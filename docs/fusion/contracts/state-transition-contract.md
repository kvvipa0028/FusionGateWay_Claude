# 任务与执行存储合同 v1

数据库为独立私有目录中的 `fusion.db`，不访问 Magpie 账号库。只支持已验证的本机 macOS/arm64 场景；网络共享、NAS 和多控制进程不在准入范围。数据库及 sidecar 必须为本人私有普通文件，目录不能位于 Git 仓库内或通过软链接指定。`fusion.lock` 的 OS flock 在整个控制器生命周期持有；第二个控制器拒绝启动。锁文件保留，不能删除后创建另一个 inode 绕过现存锁。

## 持久化与事务

schema 当前为 5，依次使用 `migrations/001.sql`、`002.sql`、`003.sql`、`004.sql`、`005.sql`；初始表包含 Task、StagePlanRevision、StageRun、RouteRevision、EvidenceRef、idempotency 和带 task 内序号的 Event，扩展表见下文。版本及每份迁移 SHA256 校验失败拒绝打开；现存未知数据库不能当成空数据库迁移。WAL、foreign_keys 和 synchronous=FULL 启用，单连接配合控制器锁与事务串行写入。

Create 的幂等键限定在 project 内，payload hash 包含项目、目标、完整快照、预算及可选预设引用。相同键和 payload 返回同一个 task；不同 payload 拒绝。未用预设时新增字段省略，旧提交的 payload hash 保持兼容。数据库事务不会调用 Runtime，也不隐式执行任务。

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

## WP-15 schema 3 启动幂等扩展

新增不可修改的 `start_requests`，保存全局 opaque 幂等键、任务、请求 hash 和唯一 run 引用。请求身份包含 task、role、plan revision、expected task generation；不同请求使用相同键返回 conflict。具体 Target 必须来自服务端冻结绑定，不能来自 HTTP 自由输入。Owner、租期和准入观察由控制器提供，不改变原请求身份；重复启动也不能改写原 Target。

`StartReservedOnce` 强制提供 key 和非负 ExpectedGeneration，在同一事务校验任务 generation、生成 attempt/intent、预留容量、保存映射与事件。只在新提交时返回 `Created=true`；仅此值允许控制器尝试一次外部启动，不替代 Runtime、quota、权限及当前状态校验。重复请求在容量与预算检查之前读取已有 run，返回 `Created=false`，不刷新租约、不消费容量、不退还调用预算。

控制器重启后 `LookupStart` 只读已有状态，包括 unknown 和终态；禁止据此再次调用 Runtime。新 key 也必须使用当前 generation，避免完成后旧请求再起一个 attempt。原 `StartIntent` / `StartReserved` 不返回 Created，故拒绝携带幂等键；旧无 key 调用行为保留，后续产品控制接口必须使用新合同。StageRun succeeded 仍不代表工程验收；本扩展不授予返工或自动恢复权限。

003 在独立事务升级 schema 1/2，001/002 原 checksum 保持不变，003 单独校验。向 schema 2/1 回滚必须恢复相应的一致性数据库备份，旧 binary 会拒绝 schema 3。已验证并发重试、终态与 unknown 重读、stale generation、键冲突、映射写入故障的全事务回滚、schema 2 数据和历史事件保留；当前尚未注册产品启动 endpoint 或实际控制器。

## WP-15 schema 4 预设历史与来源

新增项目范围的不可变 preset_revisions、CAS latest preset_heads 与 task_preset_refs。Task 创建事务核对明确版本/hash 并原子保存引用，FK 保证同一项目且引用正确 hash。版本化 PUT 的幂等重读不追随 latest，不消费新 revision 或改变 created_at；新的 head 与版本同事务提交。详见 [preset-api.md](preset-api.md)。

004 checksum 独立保存，001–003 未改；已验证 schema 3 任务/提交和启动映射/预算/held 容量/事件与政策保留，以及 004 checksum 异常和未来 schema 拒绝。原 schema 1/2 回归的最终版本断言改为当前 4，原历史数据和 checksum 断言保留。回滚至 schema 3 或更早需恢复对应一致性数据库备份，旧 binary 不能打开 4。产品启动 endpoint 尚未注册；Controller 的实际 Native 合成回归已在 schema 4 独立通过，不构成三路线真实账号验收。

## WP-15 schema 5 默认配置层

新增独立的 default_layer_revisions 与 default_layer_heads，scope 为唯一 global 或确切 project。完整五角色 canonical 层形成不可变版本/hash，head 的 FK 指向历史；显式 inherit 以新版本保存，不删除 head。005 checksum 独立，001–004 保持原样。未保存表示无覆盖，不为旧任务制造模型默认值或来源引用。

创建新任务/修订计划通过 CreateCurrent/RevisePlanCurrent 在同一事务检查当前 global/project DefaultStamp；私有 stamp 不加入原 CreateRequest JSON，因此旧 payload hash 与幂等记录不改变。LookupCreation 只读精确 project/key/payload 的既有任务；已有 receipt 优先于新建的当前条件，不退款、不刷新租约、不执行 Runtime。冻结 Snapshot、预算、generation、run 与历史事件保持原合同。

schema 4 数据保留及 005 写入失败回滚、checksum 异常已验证；原 schema 1–3 测试的最终版本断言调整为 5，未来拒绝用 6，历史数据断言保留。回滚到 schema 4 或更早须恢复对应一致性备份。Controller 的固定 Native 合成回归在 schema 5 上通过；不代替真实账号验收。完整 API 合同见 [default-layer-api.md](default-layer-api.md)。
