# Codex ChatGPT 订阅 Forwarder

本组件 `WP-12-SUBSCRIPTION-FORWARDER-01` 为可信控制器提供真实 HTTPS 发送实现，可作为 [CallGate](codex-call-gate.md) 的 NativeForwarder。它尚未登记到产品 Factory，不能证明本人已经登录、账号/地区/套餐、计费、共享额度池或整条路线准入。Jev off。

## 发送及认证

`NewSubscriptionForwarder` 只接受可信管理层登记的 [FileCredential](codex-private-credential.md)，不接受 URL、HTTP client、代理、API key、插件或 fallback 配置。唯一发送端点为：

```text
POST https://chatgpt.com/backend-api/codex/responses
```

依据固定官方 [CHATGPT_CODEX_BASE_URL](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/model-provider-info/src/lib.rs)、[Responses endpoint](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/codex-api/src/endpoint/responses.rs)、[Bearer/account header](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/model-provider/src/bearer_auth_provider.rs) 和[默认客户端标识](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/login/src/auth/default_client.rs)。公共源码指纹见[来源记录](../work-items/WP-12/SUBSCRIPTION-FORWARDER-01/public-source-info.json)。

Send 接收 CallGate 冻结的 target，并核对 FileCredential 的精确路线/版本、账号/workspace/credential identity、subscription 计费、固定 Runtime，无插件；RequestedModel=ResolvedModel、controlled_calls 和 effort 必须满足现有 CallGate 合同。请求至多 1 MiB，再使用同一字段校验拒绝工具、替换模型、summary、存储、service tier、压缩或未知权限字段。

只有控制器读取的 access token 用于 `Authorization: Bearer`，缓存账号用于 `ChatGPT-Account-ID`；不把 id/refresh token 发给模型端点。不透明 token 不用于推定权益。其余固定 header 为 JSON/SSE、OpenAI-Beta、originator、Runtime version 与注明 Fusion Gateway 的 User-Agent。存在 prompt_cache_key 时只映射为 session_id/conversation_id；非空白可打印 ASCII 和 256 字节上限校验先于网络。Worker 的真实认证、账号 header、cookies、Origin 或任意自定义 header 没有传入通路。

可信调用方仍必须先通过 Manager 的当前 model grant 与 Scheduler.Permit。Forwarder 自身是发送组件，不新建 grant、不消费替代预算、不放行 DTO 声明；绕开 CallGate 的进程内调用不属于获准产品入口。

## 连接、取消和私有输出

每次使用私有 Transport 的一个 fresh HTTP/1 连接和单次 RoundTrip：Proxy=nil、GetBody=nil、禁用 keepalive/compression/HTTP2，TLS 验证开启，没有 client redirect 或自动重试。调用 Context 与一分钟期限覆盖连接、header、全部 Body；dial/TLS handshake 各至多五秒。连接失败、301/303/307、401、其他状态、压缩和错误 media 均为静态 `ErrSubscriptionTransport`，无底层 URL、token、header 或私有 error 内容。

429 关闭上游 Body，返回空本地 Body 供 CallGate 输出静态错误；不 drain/透传私有错误，也不刷新认证或切模型。Native 之后的每个请求仍须重新 Permit，不退款、重放或在本组件重试。

macOS 试点使用与独立登录相同的固定 `/private/etc/ssl/cert.pem`。要求 canonical、nofollow、root UID、不可由 group/other 写、普通单 hardlink、有界 public PEM；FD/路径身份、读取时间/大小及内容核验后建立私有 CertPool。构造时冻结公共 CA 的文件身份和内容摘要，每个发送前及返回前重查；忽略 SSL_CERT_FILE/SSL_CERT_DIR、环境代理和用户 Keychain。CA 更新需要重新受信任登记，不能静默切信任源。其他 OS 本组件明确拒绝，未宣称跨平台信任验证完成。

200 Body 最多 8 MiB，完整读取并验证后关闭真实连接；通过前不返回部分 SSE。返回的私有内存 Body 每次读取重查原 Context 和 FileCredential，交付前换缓存即拒绝并清除未读引用。Close 丢弃未读数据，秘密和 Body 不经 JSON/String/GoString 输出。CallGate 继续核验 grant、来源与持久状态，再交付全部通过的 SSE。

## 实际模型证明

固定官方 [Responses SSE 实现](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/codex-api/src/sse/responses.rs) 从 HTTP OpenAI-Model、SSE response.headers 取得 ServerModel，明确不将普通 response.model 作为该证明。Fusion 按该来源建立 ReportedModel：

- HTTP OpenAI-Model 只接受单一有界模型值；重复值或歧义拒绝。
- SSE response.headers 只开放一个 OpenAI-Model 或 X-OpenAI-Model 字符串，大小写不敏感。重复键、两种别名同时出现、null、类型错误和其他 header 均拒绝，不把 cookies 等 header 带入 Native。
- 所有观察到的模型声明必须一致并与冻结模型完全相等，后续 completed 等事件的漂移也拒绝。至少有一个上述证明；缺失时不能从请求或普通 response.model 补造。
- 普通 response.model 如果存在仍要求与冻结模型一致，但不能独自证明实际执行模型。全部 SSE 状态、ID、输出、usage、终态及秘密反射检查继续生效。

真实凭据反射标记每项最多 16 KiB，与 FileCredential 的 token 上限一致；原文或 JSON 解码后的反射均拒绝，单项超限也拒绝。没有放宽 stage Bearer 或其他认证边界。SSE 已有的文字限制保留：工具/写入/恢复的完整供应商接线仍待后续实现。

## 验证、剩余和回退

[组件报告](../work-items/WP-12/SUBSCRIPTION-FORWARDER-01/summary.md)记录真实 TLS 回环、文件漂移、取消、模型来源、逐请求 Gate Permit、环境不继承、有效 mutation、固定 Native 的模型 header 消费及实际停止。TLS 上游和账号/Inspector 为合成数据，本轮真实供应商模型/额度/登录请求 0。

Go1.26.3 的合成目标测试命令：

```sh
go test -mod=readonly -tags fusion,nogui -race -count=1 -timeout=60s \
  ./internal/fusion/runtime/codex \
  -run 'TestCodex(SubscriptionForwarder|CallGateLongCredentialMarkers)'
python3 scripts/fusion/build-dev.py
```

本人真实缓存、账号/费用/额度、生产 Factory、续期和完整工程 Smoke 尚未完成，最终 T01–T60 状态不升级。回退可移除该可信接线；不自动修改或删除认证、不改变日常客户端、原 Magpie UI、Store schema 或 Jev off。
