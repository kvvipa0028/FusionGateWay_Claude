# WP-13 · SUBSCRIPTION-FORWARDER-01

完成可信 Grok 订阅 HTTPS Forwarder 与官方 chat proxy 模型回显核验；父 WP-13、WP-17、Gate A 和整体目标仍未完成。基线 `22a296b6f941e76e2c4ec00ad172fa9df78ecf74`，工作区干净起步；未新增 Agent。合同见 [订阅 Forwarder](../../../contracts/grok-subscription-forwarder.md)。

实现固定 `POST https://cli-chat-proxy.grok.com/v1/chat/completions` 端点、FileCredential Bearer、官方三必需 header（`X-XAI-Token-Auth: xai-grok-cli`、`x-grok-model-override` 冻结模型）、受检 root-owned 系统 CA、单次 fresh HTTP/1、无代理/redirect/retry/API fallback、有界取消与完整 SSE 后交付。请求体先经 CallGate 既有合同（含 `read_file` 受控工具面与冻结 `reasoning_effort`）校验。模型证明为每个 `chat.completion.chunk` 的 `model` 回显全等校验加 `[DONE]` 终止与至少一帧要求——回显一致性而非独立 header 证明（闭源代理无 OpenAI-Model 式响应头），已在合同中如实区分；三路线真实模型核验仍属未完成。`ForwardResponse` 增加 `ReportedModel` 字段承载验证后回显。反射按 data 帧逐帧 JSON 检测（含嵌套解码），请求与响应双向；429 返回空本地 Body 不 drain 私有错误。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 初始 RED | SubscriptionForwarder/NewSubscriptionForwarder/ErrSubscriptionTransport 未定义，编译失败 exit1 | [初始 RED](grok-fwd-red.log) |
| 最终目标 | 2 主测试 16 子场景 PASS 0 FAIL 0 SKIP，race exit0；真实 TLS 回环（生产 transport 边界断言后注入测试 CA/拨号） | [目标日志](grok-fwd-green.log) |
| 受影响模块 | grok/bootstrap/control 三包 race exit0（ForwardResponse 变更无回归） | [模块回归](grok-fwd-package.log) |
| 有效 mutation | 删除回显全等→model-drift 接受；删除逐帧反射→secret-reflection 接受；429 改为 drain 透传→泄漏，各一个行为子场景实际 FAIL/exit1 | [echo](fwd-mutation-echo.log)、[reflect](fwd-mutation-reflect.log)、[429](fwd-mutation-429.log) |
| Go1.26.3 CLI/GUI/full vet | 三项实际 exit0 | [构建日志](grok-fwd-build.log) |

fail-closed 矩阵：500 私有错误不透传、错误 media、压缩、无回显/空回显、模型漂移、流内与请求内秘密反射、nonstream、body 模型替换、307 redirect、429 空 Body、发送前凭据轮换、取消、超限 Body。Mutation 使用 Go overlay，不修改生产文件。[test-results.json](test-results.json) 记录命令与计数。

## 未完成与范围

本人 Grok 真实登录仍未发生，真实供应商请求 0；合成 TLS 上游与凭据不能替代真实 tier/费用/额度/续期证明。产品 Factory 注册、工具/写入/恢复、完整工程 Smoke、Gate A 与最终 T01–T60 仍待完成。
