# Native 工作台运行控制桥

组件 WP-16-CONTROL-BRIDGE-01。只扩展已绑定窗口的 NativeStageBridge；当前 UI/CSS、API DTO、Store/schema、Controller/Runtime/准入均未改变。后续[运行控制页面](task-execution-ui.md)已接入其中的阶段启动、Task 暂停/继续/取消及 run 读取，继续沿用 Magpie 原有任务详情区和按钮。以下验证结果为桥接组件当时的证据。

## 固定路径

| 方法 | 路径 | 意义 |
| --- | --- | --- |
| POST | `/control/v1/tasks/{task_id}/start` | 原 role、完整 Task If-Match、唯一原 Idempotency-Key；首次 202，幂等读同 run 为 200 |
| POST | `/control/v1/tasks/{task_id}/pause` | `{}` 与当前 Task If-Match；按原合同接受暂停意图或读取可靠暂停 |
| POST | `/control/v1/tasks/{task_id}/continue` | `{}` 与当前 Task If-Match；只解冻可靠暂停的派单资格，不启动阶段 |
| POST | `/control/v1/tasks/{task_id}/cancel` | `{}` 与当前 Task If-Match；整项取消，202 不表示已经停止 |
| GET | `/agent/v1/tasks/{task_id}/runs/{run_id}` | 原 ExecutionReply 包装的安全 RunView，核验 run 属于 task |
| POST | `/control/v1/tasks/{task_id}/runs/{run_id}/cancel` | `{}` 与 Task If-Match，只取消原 owned 执行 |

只能使用上表精确段数、规范 opaque ID 和方法；额外段、GET 控制、PUT 控制、POST run 读取、resume/checkpoint、events 与通用代理继续拒绝。Task 存在且所属项目仍登记后才转送；未登记与未知 Task 均为 404。Run 的 task/generation/owner 条件由原 API/Controller/Store 强制，不借窗口授权接管未知执行。

## 权限和回执

先校验原 SDK 请求来源、准确绑定窗口及 owned ControlHost/当前 Source/token；再核验项目 Task，保留上下文期限、Close 取消未完成 body 和缓冲响应后的来源/授权重查。管理凭据仅由 Go 注入，不接受页面 Authorization、cookie、任意 x-* 或代理 header。

If-Match 和 Content-Type 原样保留多值，API 独立拒绝缺失/弱/重复/错误条件；start 的原 Idempotency-Key 同样保留唯一性验证。其他运行控制不转送 key，不伪造新幂等合同。新增安全响应 X-Fusion-Task-ETag，继续剔除 Location/cookie/auth；该 header 是 Task 条件，不是整个 ExecutionReply 的资源 ETag。原响应大小、期限和 no-store 边界不变。

原 start key/role/If-Match 重读同一 run，不重复 Resolve/Start，不换 key 追求成功。pause/cancel 的 202 只表示意图被接受；Run 与 Task 后续状态须独立读取。合成取消结果下，运行暂停收尾为 needs_review，整项取消收尾为 cancelled；cancelled 不证明文件回滚、预算退款或验收成功。

产品 fusion-ui/fusion-control 仍使用 OpenControl 草稿宿主，execution_enabled=false。固定桥接不注册 RuntimeFactory，也不授予真实模型/账号/quota 准入；已登记草稿 Task 的这些控制路径仍返回 503。测试通过 OpenExecutionControl 在私有临时状态中接合成 Factory，不作为真实供应商执行证据，Jev off。

## 验证

新增 4 个顶层测试，包括 3 类运行生命周期及 11 类输入边界；真实 owned loopback HTTP、真实 Store/Controller、合成 Runtime/StopProof。验证 idle pause/continue/cancel 的完整 generation CAS 与重复只读、运行中的暂停/两类取消、原启动 key 在终态的重读、跨 Task Run 拒绝、错误窗口/来源撤销、缺失或重复 header、未知/未登记 Task 和草稿宿主不可执行。既有无条件 start/cancel 测试从“路径 404”按授权的新固定路径改为“缺 Task 条件 428”，仍断言零 Runtime 检查与零执行事件。

54 顶层/100 子测试 PASS、0 FAIL/0 SKIP，Go1.26.3 CLI/GUI/vet exit0。UI/assets 未变，浏览器无需重复，实际 Native 窗口内容/按钮仍未验证。完整日志、边界与复现命令见 [CONTROL-BRIDGE-01](../work-items/WP-16/CONTROL-BRIDGE-01/summary.md)。

后续 [EVENT-PAGE-01](task-event-page.md)新增精确事件分页 GET 及原 UI 默认折叠记录区；仅持久状态元数据回读，原 SSE 不开放到 Native 桥，不修改运行回执或自动推进 Task。CSS 未改，实际 Native 事件按钮未验证。
