# 明确 checkpoint 恢复 API

[RESUME-API-01](../work-items/WP-15/RESUME-API-01/summary.md) 已实现 Management 保护的内部 Handler 和可信 Controller 接线。独立草稿 listener 沿用现有 Handler；真实执行 Controller/Worker/GUI 未在产品 bootstrap 注册，因此不能把此合同当作真实账号路线已可用。Jev off。

`POST /control/v1/tasks/{task_id}/resume` 要求单个 strong Task If-Match（原 ready 状态）、单个 Idempotency-Key、同一 Management 身份，正文只接受：

```json
{
  "role": "design",
  "restore": {
    "origin_run_id": "run-example",
    "checkpoint_id": "64个小写hex字符",
    "checkpoint_digest": "64个小写hex字符"
  }
}
```

hex 占位文字仅解释格式；实际 ID/digest 必须来自可信已完成归档。reference 不授予权限，不携带 Native UUID、argv、Root、凭据、Target、StopProof 或历史工具授权。缺失、null、错误 digest、额外字段、重复 body/header、错管理身份和失效条件均按现有严格输入/鉴权合同拒绝；stage/query credential 和不可信 Origin 沿用原 middleware 拒绝。

新请求只能针对当前 ready Task。原 run 必须属于同 task/role/revision、exact frozen Target，已 succeeded、已确认 Native identity、终态 owner 空且可信释放。Controller 先验证 scope 与旧 reservation，可信 Backend.CheckRestore 再核验密封归档和当前 Source；管理身份在慢预检查与 Inspection 前后重查。Store 在同一启动事务重核原 run/Target/released proof、当前 generation/计划/状态及预算容量。未通过不得提交 intent。当前恢复仅接受 readonly Spec，写恢复明确拒绝；auto 选择也不能换掉归档目标。

`StartIdentity.restore` 纳入既有 start_requests payload hash。nil 使用 omitempty，保持所有普通启动旧 JSON/hash/schema；没有新数据库迁移。相同 key 的原 role/If-Match/restore 重读直接返回同一 persisted run，不进入 Resolve、归档、Inspection 或 Native。改变 origin/checkpoint/digest，或将相同 key 改为普通 Start，均冲突。普通 Start DTO 仍只接受 role，Controller.Start 也拒绝带 restore 的内部调用。请求准备时复制 identity，Inspection/Resolver 不能改写恢复引用。

只有 Created=true 可以进入明确 Backend.Restore；当前具体绑定为 BindGrokCheckpoint→Adapter.ResumeCheckpoint。真实启动时再核验私有 seal、新 prepared run/current scope，创建新 run/generation/attempt/Root/grant/端口/预算，准确恢复原 Native UUID。通用 Adapter/Session.Resume 缺少准确参数，仍 unsupported；不提升通用 Probe.Resume 或真实准入 flags。恢复前的持久 request hash 和私有 prepared mapping 均不是实际启动或停止证明。

回复沿用安全 ExecutionReply/RunView：新 intent 为 202，相同请求读取为 200，Location 指向新 run，X-Fusion-Task-ETag 指向 Task 当前条件；不输出 owner/lease/Native session/原始文本/私有路径/错误。未知启动或无 Handle 返回 409 且保留已提交 run/created、needs_review/held；重复请求只读原映射。Store 重开后的 unknown/interrupted 也不重放、不接管、不自动释放。不带新 key/当前条件不能新开恢复。

HTTP Context 只用于提交前预检查，提交后由 Controller owned lifetime 控制。明确取消/Close、实际 Wait、同 Adapter 的 Supervisor StopProof 和 Release 沿用原链。请求断线不会取消恢复；终态取消重读不构造停止证据。普通 Continue 只解除可靠暂停的派单冻结，不隐式调用这个接口。当前只允许已成功释放的归档来源；needs_review/未知/已取消来源的副作用核对仍属于后续工程闭环。

验证包括 10 个 Host 顶层/30 子测试、实际 loopback HTTP；固定 Grok Controller 3 个新增恢复场景（成功、恢复中取消、请求断线）共 6 次 Native 启动，与原 Grok 7 场景及 Claude 3 场景一起回归。合成 upstream/inspection/Quota 为零真实调用，不能代替真实 subscription、账号/计费/Quota/pool、整个 Source 的稳定性或当前项目授权服务。当前 [OpenAPI](../openapi-fusion.yaml) 为 25 paths/29 operations，52 个实际 Handler 样本、8 个 schema 越权反例通过离线标准校验。

