# Codex 独占阶段 HTTP 通道

`WP-12-CHANNEL-01` 将固定 Codex0.160.0 的受管 stdio 通道接到已鉴权的 Responses Gate。它验证实际 Native 到合成上游的文字生成，不提供真实 ChatGPT 登录、订阅 Forwarder、生产 Adapter、工具或恢复准入。证据见 [CHANNEL-01](../work-items/WP-12/CHANNEL-01/summary.md)。

## 可信构造与隔离

`NewCodexHTTPChannel` 接收完整 StageRun、私有 Root/Cwd、session、可信 driver/Current、Stage handler 和 PendingModelGrant。HTTP/CLI 请求不能注入这些对象。继承原启动通道的 frozen run/task/owner/role/attempt/plan/generation、精确 Native SHA/argv、readonly/noFork/单次 acquire；额外核对 subscription、controlled_calls、固定 requested/resolved model、已解析 effort、route revision、非插件。共享已有独占模型 lease，在同一端口占有127.0.0.1及::1；仅给 Worker 该 localhost 端口的 outbound 权限。混用 Claude/Grok、fixture env、writable、任意 argv 或外部 endpoint 均拒绝。

原 `NewCodexChannel` 继续仅支持 metadata，无模型授权、网络或真实账号。新增 HTTP 构造器的 scoped endpoint 不自动升级 registry 或计费/权益证据。

私有 CODEX_HOME 为 Root/config/codex，目录0700，config.toml0600/O_EXCL/fsync；配置不包含 secret。Worker 的 `FUSION_CODEX_STAGE_SECRET` 仅为该 run 的临时阶段 Bearer；无 OPENAI_API_KEY、CODEX_API_KEY、环境代理、日常 CODEX_HOME、auth.json 或真实 OAuth 导入。原 Codex 只读偏好许可/MDM 校验保持，Keychain/其他域/偏好写/fork/inbound 继续拒绝。阶段不开放工具，读取私有配置或环境的原生工具不能凭模型声明执行。

## 固定 Native 配置

使用独立本地 provider `fusion_codex_stage`，base_url 指向独占端口，wire_api=responses、env_key 指向阶段变量，requires_openai_auth=false、supports_websockets=false。目标 BillingPath 仍是订阅路线；本地 custom provider 仅表示受控传输。Native account/read 实际返回 account=null、requiresOpenaiAuth=false，不能冒充已登录 ChatGPT。真实订阅身份需由 Controller/registry/Forwarder 另外核验。

固定 [merge_configured_model_providers](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/model-provider-info/src/lib.rs) 不允许普通自定义配置覆盖内置 openai；没有用 fakeJWT、chatgptAuthTokens、内置覆盖或普通 API 计费兜底。现有 production Client 的默认 openai/auth/generation admission 不因此放宽；本项原生 characterization 使用 test-only raw Peer；后续[类型化阶段客户端](codex-gateway-client.md)已核验实际通知与原生终态。

冻结 model/effort、summary=none、read-only、approval=never、web_search=disabled；关闭 analytics/feedback、agents、update_plan/request_user_input、goals、shell、view_image、sleep、unified_exec、shell_snapshot、code_mode、多 Agent、apps/tool_search、remote_models/discovery及压缩。未关闭 Guardian、MDM 或 host-managed requirements。request_max_retries=2、stream_max_retries=2、stream_idle_timeout_ms=5000。

固定 [HTTP retry](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/model-provider-info/src/lib.rs) 关闭429重试，允许5xx/transport重试；[协议错误 retry_delay](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/protocol/src/error.rs) 对 UnexpectedStatus 允许有界 stream retry。因此 HTTP 请求数可能超过真正上游发送数。Gate 的 uncertain/预算/当前性拒绝始终先于额外发送，不能靠 Native 终态或 retry 配置替代预算控制。

## 生命周期与验证

Supervisor ConfirmStarted 后先激活 PendingModelGrant，成功后才启动 stdio driver。失败返回已拥有 Native 的 handle，停止/reap并保持真实 StopProof/预约释放；driver和HTTP均0。活跃 channel.Close 拒绝释放端口，实际 wait/reap后才 revoke/关闭；源/Current漂移、取消、deadline、transport失败遵循已有 Supervisor fence。

实际测试结果（账号/上游/额度均为合成 fixture）：文字1HTTP/1发送/1预算；429为1/1/1并终止；不支持503产生静态502、Native4HTTP但仅1发送/1预算，后续拒绝；三个独立 ephemeral Native turns在 MaxCalls=2时共5HTTP/2发送/2预算，第三轮及其重试均拒绝；inflight取消1/1/1，HTTP context与Native都结束、已消费预算不退。所有场景核验 Native 实际终态或取消、wait/StopProof/release，不将合成 route flags 作为真实准入。

typed Gateway Client已完成受限协议接入；仍须完成生产 Adapter、真实官方私有身份与凭据登记、订阅单次 Forwarder/额度/计费、全部子调用、原生工具与恢复、产品阶段闭环及最终验收。64KiB stdio预算仍沿用原组件边界，不能宣称任意长生成任务容量已验证；真实 Native SHA固定不代表完整 publisher/install trust。
