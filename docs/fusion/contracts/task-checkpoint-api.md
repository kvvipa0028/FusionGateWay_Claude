# 可信 checkpoint 管理入口

[CHECKPOINT-API-01](../work-items/WP-15/CHECKPOINT-API-01/summary.md) 将既有私有归档生产端接入 Controller/Management Handler。真实执行 bootstrap/Worker/GUI 尚未注册；独立草稿 listener 复用 Handler，但无 Controller 时仍拒绝。Jev off。

`POST /control/v1/tasks/{task_id}/runs/{run_id}/checkpoint` 要求同一 Management 身份、单个 strong Task If-Match、正文严格 `{}`。不接受调用者提供的 ref、Native session、Root、argv、Target、输出、凭据或 StopProof。成功返回 200：

```json
{
  "checkpoint": {"id": "64个小写hex字符", "digest": "64个小写hex字符"},
  "run": {"id": "原run的安全RunView"}
}
```

上例是字段示意，实际 run 包含既有 RunView 字段，hex 必须是生产端返回的准确值。Location 指向原 run 的管理读取路径，X-Fusion-Task-ETag 表示相关 Task 当前条件；不设置回复资源的 ETag，不输出 Native UUID、owner、lease、原始文本、私有路径/key 或 process proof。正常重复请求返回相同 reference，没有新 Native/模型调用/预算/event；仍需当前条件和管理身份。

Controller.Checkpoint/CheckpointAuthorized 仅对当前 ready Task、相同 generation 的 owned run 生效。job 必须真正 Wait 完成、Store succeeded，StoppedVerified 与 Released 均可信通过；当前完整 run 与 release 时保存的私有终态快照一致，无 held reservation。归档 callback 固定为启动时 Backend.Checkpoint，不重新 Resolve，不让后续配置替换旧 Adapter。失败、取消、未知、未结束、无停止证明、释放失败、另一个 Controller 或未绑定生产端都拒绝。ref 的格式核验不是 seal；具体 BindGrokCheckpoint 调用同一 Adapter.Checkpoint，在私有 Archives 内继续核验原 proof/14 文件/Read/seal/Source，详见 [grok-checkpoint.md](grok-checkpoint.md)。

Task 全 PlanRevision/Generation/State 在生产前后重查；失配或当前不是 ready 为 412，缺条件为 428，非法条件/body 为 400，stage/query credential、Origin 和 Management 沿用既有 middleware。管理身份在归档前后及 API 回复前重查；撤销后不返回 reference。Backend 原始错误和无效引用均转为固定 reconciliation 错误，不回显秘密。Controller关闭/未注册为503，所有权/成功退出/停止无法核验为409，未绑定归档能力为503。

生产操作计入已有 16 个 Controller work slots 和 wait group；使用请求 Context，并链接 Controller lifetime。HTTP 断线可中止归档请求，不取消已停止的旧 Native；Close 会取消归档 Context并等待操作退出。可信 Backend忽略取消时，Close 超时保持未完成，不宣称已停。除了取消 Context，归档前后的关闭标志也独立重查，覆盖 Close 已置位、AfterFunc尚未送达的间隔。

归档属于私有文件系统副作用，管理撤销或条件变化可能发生在私有发布之后。失败回复不披露引用、不改变 Task/预算/event，也不谎称已经回滚私有归档。重复私有归档仍按原 seal/稳定 reference 核验。此接口不增加 DB schema/table 或可由客户端提交的归档路径；已有 Archives 自身持久化原文/seal。调用方保存返回 reference 后，可用于 [明确恢复接口](task-resume-api.md)。进程重启后没有原 owned Handle，不通过这个 producer重造停止证明；已有持久 reference 的 Info/Restore消费者保持独立核验，不自动接管 unknown 或扫描日常认证。

实际 Grok Controller 经此入口取得 reference 后恢复准确 UUID，重复生产不增加HTTP；包含旧批准Read历史、取消和请求断线。Management API真实loopback测试使用合成Backend；Native/Controller证明与HTTP鉴权证明分别记录，尚未证明真实账号经产品HTTP执行。真实模型/Quota均0，合成inspection/上游不提升账号、计费、quota/pool、整个Source稳定性或当前项目权限服务的准入 flags。
