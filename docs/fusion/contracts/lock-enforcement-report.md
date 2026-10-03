# 严格执行出口合同 v1

WP-07 增加独立的 `policy.Dispatcher`，不调用 Magpie `Group.Picked()`、smart/usage、Jev、自动 effort 或跨账号 fallback。入口仅接受服务端签发身份的 Context，按持久化 run 获取冻结目标；客户端 model/effort 只可用于一致性断言，不能选择账号、供应商、路由组或任意 header。

每次调用和重试核对 run/generation/lease、exact route revision、model、account、credential identity、workspace、billing、Runtime 版本、能力、effort 及 transport ID。预算/权限 gate 必须提供；gate 等待后再次核对路线和身份。Gate 和 Executor 收到目标的深复制，不可修改持久化合同。账号凭据的同身份续期可用；换身份、账号、版本或账单路径必须停止旧执行。

当前 Dispatcher 只允许 controlled_calls、无插件的已准入路线；primary_only 与插件路径暂不开放。路线注册表、Executor 和 Permit 均属受信任服务端模块，不能从请求构造。WP-08 必须把准入证据绑定到具体 Executor/transport/Runtime 版本；一个 callback 或 route.admitted 布尔值不能证明官方 Runtime 已受控。

重试次数由服务端给定，范围 1–3，且每次消耗共享 Permit。仅受信任 Adapter 明确声明没有交付输出、工具或写入效果的 DispatchFailure 可重试。普通网络错误、部分响应、模型不一致和 HTTP 错误不会自动重试。真实运行预算归 WP-10，不通过 maxCalls 参数制造独立无限预算。

HTTPJSONExecutor 是内部有界 JSON 合同，用于受准入控制的服务端路径和离线验证，不是 GLM Coding Plan 或三订阅路线的通用 API 代理。它固定 endpoint/transport，拒绝 redirect、credential identity/account 不匹配、重复或未知响应字段、尾随 JSON、超限和截断响应。凭据只进入服务端 Authorization header；下游错误返回静态错误，不回显供应商 body 或 key。三订阅路线仍使用 WP-12–WP-14 的官方宿主 Adapter。

结果分别记录 requested_model、resolved_model、upstream_reported_model。上游未声明时保留 null；声明不同模型时返回错误并保留不同值，不补写成请求模型、不重试到另一个模型。该记录证明本系统可观测调用，不证明供应商内部权重身份。

原版不加 fusion tag 的 Group/manual 行为保留。Fusion 旧网关入口仍关闭，WP-15 将显式鉴权的新 API 接到 Dispatcher；本包未开放任何真实账号调用或绕过 master 开关。最终产品 Gate 和真实 Runtime 的摘要/子 Agent 调用控制仍未验收。
