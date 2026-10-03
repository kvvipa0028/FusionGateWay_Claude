# 阶段调用身份合同 v1

管理身份与 Worker 身份分开。Manager 只保留管理秘密的 SHA256，比较使用 constant time；不把管理秘密放入 Worker 环境、任务快照、事件或响应。产品的私有管理 Key 引导和 GUI 会话接入在 WP-15/WP-16 完成，当前只验证安全原语并关闭旧入口。

Worker 获得随机 256-bit、`fgs_` 前缀的不透明短期 secret。服务端内存仅保存其 SHA256、Claims 和 expiry，最大 TTL 五分钟。原生子进程启动前可签发 worker_events 用途；model 用途必须先确认启动。签发和每次调用都验证持久化的 task/run/role/attempt/plan revision/generation/project/lease，不能把客户端的 role/account header 当成身份。

Claims 包含 task_id、run_id、role、attempt、plan_revision、generation、project_id、audience。audience 仅为 model 或 worker_events；一个用途的凭据不能访问另一个用途。终态、未知状态、过期 lease、旧 generation、错误项目/角色/attempt 均拒绝；观察到 lease 过期时持久化 fencing，时钟回拨不能恢复旧调用权限。

撤销 run 会删除其所有 secret 并禁止为同一个 run 重新签发。管理身份撤销会关闭管理调用及新签发，并清除现有 stage secrets。issuer 重启默认撤销全部阶段 secret；不能从一个 token 的文本重建授权。需要续行时必须核对当前执行状态，再由服务端向获准当前 run 签发新身份。

HTTP 只接受单个 Authorization Bearer；不接受 cookie、URL query key 或任意 x-api-key 退路。失败响应只有 401/403 和静态文本，不回显凭据或 URL。管理接口额外拒绝未登记 Origin、cross-site 和 URL 中的认证字段；删除 stage 字段也不能将 fgs_ 凭据变成管理身份。

通过鉴权后清除 Authorization、API key、账号 header 和全部 X-Fusion-* 权限声明（包括非 canonical 大小写），仅在服务端 Context 放入经核对的 Claims。执行层仍需按冻结目标、当前路线准入、额度、工作区权限和预算核验；鉴权通过本身不能代替 strict-policy。长调用取消与已启动进程停止由 WP-11 的受管 Runtime 控制，不把 HTTP token 撤销冒充写进程已停止。

Manager 的 StoreValidator 为已验证的默认作用域检查器。其他 validator 仅限受信任服务端逻辑，不从请求提供；缺少 validator、缺少管理身份或无效 Claims 时不能签发。Stage secret 只能经受保护的服务端编排逻辑交给 Worker，不能公开一个接受任意客户端 Claims 的签发接口。

WP-15 长连接管理 Context 仅保存当前 issuer 的私有标识，不携带原始管理秘密。ManagementCurrent 必须核对同一 issuer、未取消/过期的 Context 和仍有效的管理身份；其他 Context 或 issuer 的值不获授权。SSE 每次发送及轮询都重查，撤销后关闭 stream；已发送字节不会被追回，HTTP EOF 不改变 Worker 状态或提供 stop proof。管理 Key 引导和真实 GUI 接入仍待后续完成。

WP-14 CallGate 在当前 Manager.Stage Context 上调用 ModelCurrent：核对同一 issuer 的仍有效 model grant 与完整预期 Claims，不用 FromContext 单独建立授权。BeginModelCall 与 Dispatcher 共用 per-run 互斥，release 幂等且不退预算。Native HTTP 入口须每次 Permit 后和响应交付前重查；静态请求/事件数量不能替代全部实际 HTTP 预算。当前只完成 Handler 与合成 Native 接线，生产 model grant 仍须在 ConfirmStarted 后交付，不通过放宽 StoreValidator 解决启动顺序。
