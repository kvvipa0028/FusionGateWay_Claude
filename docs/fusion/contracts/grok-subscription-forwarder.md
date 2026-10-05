# Grok 订阅 Forwarder

本组件 `WP-13-SUBSCRIPTION-FORWARDER-01` 为可信控制器提供真实 HTTPS 发送实现，可作为 [CallGate](grok-call-gate.md) 的 NativeForwarder。它尚未登记到产品 Factory，不能证明本人已经登录、账号 tier、地区、计费、共享额度池或整条路线准入。Jev off。

## 发送及认证

`NewSubscriptionForwarder` 只接受可信管理层登记的 [FileCredential](grok-private-credential.md)，不接受 URL、HTTP client、代理、API key、插件或 fallback 配置。唯一发送端点为官方 CLI chat proxy（固定 1.0.48 二进制内嵌文档确认）：

```text
POST https://cli-chat-proxy.grok.com/v1/chat/completions
```

必需 header 按官方协议冻结：`Authorization: Bearer`（私有缓存 `.key`）、`X-XAI-Token-Auth: xai-grok-cli`（会话 token 验证模式）、`x-grok-model-override`（代理按该 header 而非 body model 路由，取冻结 ResolvedModel）；另有 JSON/SSE、注明 Fusion Gateway 的 User-Agent。请求体经 CallGate 既有合同校验（含 `read_file` 受控工具面与冻结 `reasoning_effort`），至多 1 MiB；Worker 的认证、cookies、`XAI_API_KEY` 或任意自定义 header 没有传入通路。

可信调用方仍必须先通过 Manager 的当前 model grant 与 Scheduler.Permit。Forwarder 自身是发送组件，不新建 grant、不消费替代预算、不放行 DTO 声明。

## 连接、取消和私有输出

每次使用私有 Transport 的一个 fresh HTTP/1 连接和单次 RoundTrip：Proxy=nil、GetBody=nil、禁用 keepalive/compression/HTTP2，TLS 验证开启，没有 client redirect 或自动重试。调用 Context 与一分钟期限覆盖连接、header、全部 Body；dial/TLS handshake 各至多五秒。非 200/429 状态、压缩、错误 media、redirect 均为静态 `ErrSubscriptionTransport`，无底层 URL、token、header 或私有 error 内容。

429 关闭上游 Body，返回空本地 Body 供 CallGate 输出静态错误；不 drain/透传私有错误，不刷新认证或切模型。Native 之后的每个请求仍须重新 Permit，不退款、重放或在本组件重试。

macOS 试点使用与独立登录相同的固定 `/private/etc/ssl/cert.pem`（root-owned、nofollow、单硬链接、有界 public PEM），构造时冻结 CA 文件身份与内容摘要，每个发送前后重查；忽略 SSL_CERT_FILE/SSL_CERT_DIR、环境代理和用户 Keychain。其他 OS 明确拒绝。

200 Body 至多 8 MiB，完整读取后经私有内存 Body 交付；每次读取重查原 Context 和 FileCredential，交付前缓存轮换即拒绝。秘密与 Body 不经 JSON/String/GoString 输出；`ForwardResponse` 新增的 `ReportedModel` 是 Forwarder 验证后的模型回显，不是调用方声明。

## 模型回显与反射边界

官方代理按 `x-grok-model-override` 路由并在每个 `chat.completion.chunk` 的 `model` 字段回显。Fusion 据此建立 `ReportedModel`：**每个** data 帧的 `model` 必须存在且与冻结模型完全相等，至少一帧存在，`[DONE]` 终止标记必须存在；缺失、漂移或空回显一律拒绝交付。这是回显一致性校验而非独立 header 证明（闭源代理未提供 OpenAI-Model 式响应头）；三路线真实模型核验仍属未完成，本组件不冒充该证明。

真实凭据反射按 data 帧逐帧 JSON 检测：帧内任何字符串包含 Bearer、或其 JSON 解码嵌套后包含，均拒绝；请求体同样先经反射检查再发送。无放宽 Bearer 或认证边界。

## 验证、剩余和回退

[组件报告](../work-items/WP-13/SUBSCRIPTION-FORWARDER-01/summary.md)记录真实 TLS 回环、固定 header/端点/body、模型回显矩阵、取消、轮换（发送前与交付时）、逐请求边界与三项有效 mutation。TLS 上游与账号为合成数据，真实供应商请求 0。

```sh
go test -mod=readonly -tags fusion,nogui -race -count=1 -timeout=90s \
  ./internal/fusion/runtime/grok -run 'TestGrokSubscriptionForwarder'
python3 scripts/fusion/build-dev.py
```

本人真实登录、真实 tier/费用/额度、产品 Factory 注册、工具/写入/恢复与完整工程 Smoke 未完成；最终 T01–T60 不升级。回退可移除该可信接线；不自动修改认证缓存或日常客户端。
