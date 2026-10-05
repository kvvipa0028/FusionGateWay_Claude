# Codex 受控 Responses HTTP 出口 v1

WP-12-CALLS-01 实现 `runtime/codex.CallGate`，供可信 Controller 的已登记 ChatGPT 订阅路线接线。它是内部 HTTP 控制组件；不是生产 Codex Adapter、登录管理器或全部原生调用的准入证据。metadata bootstrap 保持无网络；[独占 HTTP 通道](codex-http-channel.md) 已验证真实 Native 与合成上游的文字调用，真实订阅准入仍未完成，Jev off。

## 身份、冻结目标与预算

构造器固定 Binding 的 run/generation/role、账号/workspace/credential identity/身份 generation、ExecutionTarget、Cwd 和 CodexHome。完整 Claims 还固定 task/project/attempt/plan revision/model audience。只接受 Codex0.160.0、subscription、controlled_calls、相同 requested/resolved model、已解析的字符串 effort 和非插件路线；没有 API key 或替换路线退路。callbacks/Forwarder 仅由服务端进程内提供，不从请求 DTO 建立授权。

每个 HTTP 请求先经过 Manager.Stage 的单一 Authorization Bearer 鉴权；Worker 请求的账号、权限和真实认证 header 不传给 Forwarder。未知/过期/错误用途凭据、Origin 和认证 query 的拒绝不污染有效 run。获准但不符合固定协议的请求将 Gate 标为 uncertain；后续模型发送拒绝。

每次实际发送必须取得与 Dispatcher 共用的 BeginModelCall，再调用生产 Scheduler.Permit，逐次核对当前路线、权限、额度、reservation 和持久预算。成本为一个实际 HTTP；429 后的客户端重试重新 Permit。已经消费的预算不退回；Gate 不执行重试或 redirect。Permit 前后、发送结果交付前重查当前身份/Source/作用域，Current callback 返回后再次核验 model grant，防止 callback 内撤销漏过。

Current 必须核对全部冻结 Binding、当前登记凭据、来源工作区与持久执行状态；单一 Boolean 或 synthetic Inspector 不是生产准入。健康检查仅能使用同一 Manager 的有效 model Context，不能用裸 context 或旧 secret。

## 接受的有限协议

唯一入口是 `POST /responses`，JSON、无压缩，无 query/RawPath/绝对 URL；拒绝 subagent header。请求最多1MiB，完整对象解析沿用 Codex 的 UTF-8、重复/case-fold 字段、深度、null 与 trailing 拒绝。input 最多128项。

本版仅允许文字消息与无 summary 的 encrypted reasoning history，tools 缺省或空数组；拒绝历史工具项、工具定义、图像、外部搜索、service tier、未知 client metadata、structured output、WebSocket、压缩与 compact 等路径。effort 必须等于冻结值，summary/context 不开放，stream=true、store=false；include 仅允许 reasoning.encrypted_content。这是受限字段投影，不承诺未经实测的原生工具集兼容。

`client_metadata` 只接受固定 Native 实测的七个观测键：root_turn_id、session_id、thread_id、turn_id、x-codex-installation-id、x-codex-turn-metadata、x-codex-window-id。每项必须是非空字符串，最多4096字节；未知账号键、null、类型错误、重复/case aliases 及凭据反射拒绝。观测字段不参与账号、路由、权限或预算授权。

可信 NativeForwarder 只接收冻结 target 与请求正文副本，执行一次 context-bound 发送，独立核对订阅端点、账号、凭据身份与计费归属。ReportedModel 必须来自真实上游响应，不能从请求填造；上游 JSON 中若另有 model，同样必须匹配。真实凭据反射标记仅在可信进程内参与检查，单项上限 16 KiB 与 FileCredential token 一致。[订阅 Forwarder](codex-subscription-forwarder.md) 已实现固定端点、私有缓存、显式系统 CA 和单次 HTTPS；从 HTTP OpenAI-Model 或 SSE response.headers 取得实际模型，普通 response.model 不补造该证明。生产 Factory、真实身份/计费/额度与完整 Native 工程接线仍待实现。

200 响应只接受无压缩 SSE、正确 ReportedModel。最多8MiB/4096帧/每帧1MiB，完整 UTF-8、JSON keys、事件名、响应 ID、输出项身份/类型、文字 delta/final 一致、completed output 与已完成输出一致、usage 数值均校验后才释放原文。拒绝未知事件、工具、summary、failed/incomplete、缺终态、未完成输出项、负数/矛盾 usage、模型漂移及 raw/JSON-decoded 凭据反射。拒绝时没有部分 SSE；没有上游 header/cookie/error 透传。429 只返回本地静态错误，后续实际发送仍逐次消费预算。固定0.160.0的 HTTP retry_429=false，不能声称它自动重试429；503等非本版支持的响应将 Gate 标为 uncertain。Native 后续重试只收到拒绝，不产生额外上游发送。

本版保守接受单个文字 content part 的消息输出；不把辅助事件观察、SSE completed 或 Gate Healthy 当作 Native turn 终态、全部模型调用、账号登录、额度或停止证明。原生协议、OS sandbox、thread/turn 绑定和 Supervisor 的 wait/StopProof/release 仍须另行核验。

## 生命周期与验证边界

单请求60秒，最多四个获准请求等待，同一 run 的出口串行；取消等待者不消费预算，出现 uncertain 的并行请求会阻止仍在发送的结果交付。HTTP Server 必须配置读取期限；Forwarder 的网络读、Body 与 context 必须受控。可信 callbacks 与 Forwarder 不是不受信任代码的隔离界面。

验证使用真实 Store/Manager/Scheduler 与合成订阅 Inspector/Forwarder：预算上限2时，429、成功、拒绝产生持久 UsedCalls=2，仅两次发送；下一次额度、数据权限或 proof 改变时拒绝，不额外消费预算。另覆盖冻结副本、撤销、并发取消、响应末尾错误和敏感数据保护。CALLS-01 当时未运行真实 Native HTTP；后续 [CHANNEL-01](../work-items/WP-12/CHANNEL-01/summary.md) 已验证固定 Native 的实际 scoped HTTP/终态/取消，仍未请求真实订阅模型，不升级 WP-17/最终 Gate。

固定源码参考及 SHA 见 [CALLS-01](../work-items/WP-12/CALLS-01/reference-sha256.json)：[Responses 请求 DTO](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/codex-api/src/common.rs)、[HTTP endpoint](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/codex-api/src/endpoint/responses.rs)、[SSE 投影](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/codex-api/src/sse/responses.rs)。冻结 Native 源码的 chatgptAuthTokens 标为 OpenAI 内部使用，不能作为生产登录替代；真实私有官方 ChatGPT 登录管理待单独完成。
