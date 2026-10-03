# 项目额度 API 合同

此接口属于 Management 保护的内部 Handler，尚未注册产品 listener/GUI。QuotaSource 仅由可信 bootstrap 登记，绑定已存在项目的可信 route-registration revision、精确 route revision 和 quota identity（provider/account/workspace/region/generation）；HTTP 不能提供 token、URL、账号、workspace、generation 或采集器。Current 必须是本地、无阻塞的当前 quota-purpose/凭据身份检查，Fetch 必须在自己的出站边界独立检查用途与身份。额度查询准入独立于生成准入。

| 方法/路径 | 行为 |
|---|---|
| GET `/control/v1/projects/{project_id}/quota` | 只读登记与缓存，不调用 Fetch；响应为 configuration_revision、routes、pools |
| POST `/control/v1/projects/{project_id}/quota/{route_id}/refresh` | JSON 必须是 `{}`；仅刷新登记来源，随后返回同一项目当前视图 |

每行 route 包含 RouteRef、当前有效 Identity、status、可选 Snapshot 和固定 Error。未登记采集来源为 unsupported；有效登记但没有观测为 unknown。授权/身份已撤销或可信 SetProject 变更使原登记失效时为 unverified，不返回其缓存 Snapshot。未知项目/route 为 404，来源失效为 409 `quota_source_changed`，无采集能力为 503 `quota_query_unsupported`。无/错误 Management 为 401；stage fgs_ 和 query credential 为 403，沿用 WP-06。非法 body 为 400。

读取按 quota.State 和最多一分钟的 source age 重新评估，不改 ObservedAt/ReceivedAt；旧值、ResetAt 已到期、缺少完整性或物理 pool 证明不能变为 available。保留窗口单位、原始数值、resources 与来源；不把 money/tokens 当作订阅百分比。采集失败只返回固定错误，已有观测由 Broker 降为 stale/auth_required/unsupported/unknown；没有首次观测时保留失败状态，GET 不隐藏 auth_required，也不自动重试。

用 quota.GroupPools 合并有证明的物理池，aliases 不累加剩余额度；更晚的不完整观测不能被较早的 available 观测遮盖，未验证 pool 保持独立。此接口的 snapshot/pool flags 不构成工程调用准入，Scheduler 仍独立检查。

Broker 对精确 Identity 的同时刷新合并，并在后台保有最多十秒的 query deadline；整个 Server 最多八个 Fetch Worker。采集器 Context 保留已验证的 Management issuer 值，取消与 deadline 来自 Broker，支持出站授权重查且不会继承第一个 HTTP 请求的断开。HTTP 断开不会释放仍执行的 Worker 名额，采集器实际返回才释放；不配合 deadline 的采集器继续占用名额，不另起替代请求。Broker 自身和服务名额满返回 429。Management issuer、可信 route-registration revision 和来源 Current 在 Fetch 前后以及响应前重查；撤销后结果不会作为该项目当前可用缓存返回。

登记每个可信 route-registration revision 一次；修改 SetProject 后必须重新登记来源，旧缓存不能隐式接入新配置。当前只保存进程内 quota 缓存与观测失败状态，不做持久额度缓存，也不自动轮询。旧登记中已开始的采集仍受全局 Worker 名额、deadline 和出站 Current 约束。

12 项 API 测试覆盖实际 loopback HTTP、cache-only GET、来源时间与过期、越权请求、身份/配置撤销、错误脱敏、首次授权失败、登记失配与热替换、合并刷新撤销、客户端取消后的实际名额、全服务名额耗尽、共享 pool 的更晚部分观测和采集 Context 的 issuer/deadline。本项采集器为 synthetic fixture，真实 GLM QuotaReader 的单次 CN 查询证据见 WP-14；本项没有新增真实模型/额度调用。产品接线继续实施，当前 Handler 的完整 OpenAPI 已纳入验证，Jev off。

默认层写入仅改变选择配置，quota registration 使用独立 routeRevision，因此保留原 reader/cache/source age；选择变更不能重新登记相同来源或刷新 ObservedAt。响应 configuration_revision 仍为当前有效选择配置版本，不能拿它替代内部来源授权条件。新增默认层回归覆盖此行为，见 [default-layer-api.md](default-layer-api.md)。
