# WP-15-REVISION-01：未开始阶段计划修订

状态：本子工作项 `done`，WP-15 继续 `in_progress`。组件接口尚未注册到生产 listener/GUI；旧入口继续关闭。本项未发出真实模型请求。

## 接口与使用

管理 Bearer 鉴权沿用 Server.Handler；不能使用 stage secret、query token、cookie 或角色 header。

| 方法与路径 | 输入与结果 |
|---|---|
| GET `/control/v1/tasks/{id}/plan` | 返回当前不可变 Snapshot；ETag 为带双引号的正整数 revision |
| POST `/control/v1/tasks/{id}/plan/preview` | If-Match 取读取到的 ETag，body 仅为 `{"task":{"roles":{...},"groups":{...}}}`；返回新 revision/hash、receipt、配置版本及原预算上限；无持久效果 |
| PUT `/control/v1/tasks/{id}/plan` | If-Match 为同一原 revision，body 为 `{"preview_id":"...","plan_hash":"..."}`；提交原服务端快照，返回该 Snapshot 和新 ETag |

If-Match 缺失返回 428/plan_precondition_required；重复、弱 tag、通配符、未加引号、零/负数/前导零/溢出返回 400。版本或已开始阶段冲突返回 409/plan_revision_or_stage_conflict。不得后写覆盖。用途/任务/base/hash/配置/有效期不匹配的 receipt 返回 409/preview_expired_or_changed。

receipt 为进程内随机 256-bit 值、五分钟有效期，与创建 receipt 共用最多 1024 个容量；重启需重新预览。已提交且仍保留的 receipt 重试返回该次提交的原快照，不因后续修订返回新 hash，也不把任务倒回旧版本；最新状态通过 GET 获取。创建与修订 receipt 不互通。

## 冻结和竞态

CompileRevision 只编译明确选择的角色，以当前受信任项目/全局配置与注册路线处理 locked/auto/inherit。其他 frozen binding 深拷贝原值，默认或旧路线变化不会重新解释。保留历史的已退役路线元数据不授予执行权限；后续实际启动仍必须走当前准入检查。

禁止空修改、未知或非 required_roles 的角色、增删必需阶段，以及 HTTP 导入预算、凭据、全局配置和自制 Snapshot。原角色集合保持不变。Store 的普通修订冻结任何已有 stage_run 的角色，包含已完成/失败/取消/中断/未知状态；不能把这些绑定用新值改写。运行中阶段安全暂停后重新绑定并启动新 attempt 的功能，仍需后续独立控制/恢复合同，本接口不提供。

ValidateRevision 在预览时只读核对；RevisePlan 在提交事务再次核对，关闭“预览后阶段已启动”的窗口。快照插入、当前 revision 与 plan_revised 事件共同提交，任何失败全部回滚。

未来角色修订保持当前 run 的 PlanRevision/target、attempt/generation、lease/session、预留和预算计数。当前凭据继续绑定原 run revision，不能自行改成任务的新 plan revision。接口不启动、暂停、取消 Worker，也不释放预留或退还调用/返工计数。

## 验证与边界

缺失 CompileRevision/ValidateRevision 先编译 RED；新增 HTTP 路由先 404 RED，随后实现 GREEN。4 个 stageplan、4 个 store 和 8 个 API 新 top-level tests 验证：明确角色/继承/深拷贝、退役历史元数据、已完成角色禁止改写、预览无持久效果、必需角色不可替换、事件故障事务回滚、活动 attempt 与已消耗预算保持、当前能力凭据仍有效、预览后启动竞态、两个页面仅一方提交、receipt 重放/跨任务/用途/配置/hash、强 If-Match、非法输入和重启/过期/鉴权。

完整 Fusion/nogui tagged race：189 个 top-level PASS，2 个父进程 helper SKIP（既有 OS 子进程测试分别执行 helper）；CLI/GUI 构建与全仓 Fusion/nogui vet exit 0，Go 1.26.3。本项 HTTP 验证通过 Handler 测试；完整套件还包含先前 SSE 的真实 loopback HTTP 测试，不把它当作本项生产部署证明。

尚未完成：实际 UI/OpenAPI 接线、生产 listener/调度接线、安全暂停后换模型与新 attempt、暂停/取消/继续、预设/额度接口，以及三条真实路线和最终工程闭环。T34/T35/T36 等最终验收仍为 not_run，不因组件检查成功而改为通过。
