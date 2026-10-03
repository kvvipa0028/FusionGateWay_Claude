# 阶段执行 API 合同

当前接口是带 Management middleware 的内部 Handler，尚未注册产品 listener/GUI。可信 bootstrap 只能一次 SetController，并且 Controller 必须使用相同 Store；请求不能更换控制器或提供 Runtime/workspace/token 参数。管理身份在预检查和 Inspection 前后按原 issuer 重查。无/错误管理凭据为 401，stage `fgs_` 凭据为 403，沿用 WP-06；不因接入新 API 改变鉴权合同。

| 方法/路径 | 请求与返回 |
|---|---|
| GET `/agent/v1/tasks/{task_id}` | 原 Task JSON，新增 strong ETag `"p{plan_revision}-g{generation}-{state}"`；计划、generation 或状态变化均改变 tag |
| POST `/control/v1/tasks/{task_id}/start` | 单个 If-Match、单个 Idempotency-Key，JSON 仅 `role`；新 intent 为 202，幂等重读为 200 |
| GET `/agent/v1/tasks/{task_id}/runs/{run_id}` | 必须核对 run.task_id；返回安全 RunView，跨 task 不返回记录 |
| POST `/control/v1/tasks/{task_id}/runs/{run_id}/cancel` | 单个 If-Match，JSON `{}`；当前 generation 的取消意图为 202，同 generation/计划的已结束 run 为只读 200 |

Task ETag 与 `/plan` 的整数 revision ETag 是不同资源合同，不混用。只接受带引号、规范数字和状态的单个 strong task tag；weak、通配、多值、前导零、非法或溢出数字均为 400，缺失为 428。新启动必须匹配当前 ready Task，失配为 412。Store/Controller 仍独立执行 generation CAS、当前 ready 和冻结 Target 检查。

重试保存原始 key/role/If-Match（ready 状态的原版本/generation）。已有启动映射在当前 Task 状态校验之前只读，所以运行、终态及 unknown 可以重读；不同身份/角色不能用相同 key 改写原请求。新 key 不能借旧 tag 重启。取消不采用调用者提供的 role/owner/attempt，按路径 run 与当前 Task 的 plan/generation 校验；未知执行不能自动接管。终态取消只读，可接受原同 plan/generation 的状态 tag，不取消其他 attempt，也不改变终态。

RunView 包含 run/task ID、role、attempt、generation、plan revision、state、intent/confirmed 标志和冻结 Target；不包含 Owner、lease、Native session、原始输出、凭据或私有 Spec。首次启动后无 Handle 的失败为 409 reconciliation，保留已提交 intent 的 RunView/Created，不能把它隐藏成“未提交”；调用者重试只读同一记录。所有异常只返回固定 code/message，不回显 Native/Resolver 错误或拒绝输入。

Run/start/cancel 响应的 `X-Fusion-Task-ETag` 是相关 Task 当前条件，Location 指向 run 读取路径；不把 Task tag 标成这些不同响应体的 ETag。HTTP 断线不能取消已提交 execution，当前管理撤销检查用于提交前授权，模型 grant/route/权限/额度仍在 Adapter/Permit 独立强制检查。

10 个 API 测试覆盖真实 loopback HTTP、合法启动/重读/取消/运行读取、强条件与状态变化、15 类越权 body、重复 header、管理撤销发生在 Resolve/Inspection 时、同 key 冲突、失败 intent 与错误脱敏、跨 task/gen 读取取消，以及跨 Store/热替换控制器拒绝。这里的 Runtime/StopProof 为合成 fixture；真实 Native 停止证明的控制器链路证据见 CONTROLLER-01，不能把此接口测试当作真实账号 smoke。

[openapi-fusion.yaml](../openapi-fusion.yaml) 当前是上述已实现执行接口的 draft，后续必须补全已有 preview/submit/plan/budget/events 及尚未实现的 pause/resume/presets/quota；不是完整 WP-15 OpenAPI 交付。实际账户/项目配置、产品注册、暂停/恢复/返工与 UI 继续实施。Jev off。
