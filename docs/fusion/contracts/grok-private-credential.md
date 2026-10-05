# Grok 私有官方登录缓存服务

本组件 `WP-13-PRIVATE-CREDENTIAL-01` 为固定 Grok 订阅路线提供真实生产凭据读取器。它不是登录入口、不证明本人已登录、账号 tier、地区、计费或共享额度池，也不授予任何执行权。父 WP-13 与 Gate A 未完成。

## 官方格式与冻结读取

Grok CLI 1.0.48 为闭源发布；格式证据来自固定二进制（SHA256 与[发布者记录](../work-items/WP-17/PUBLISHER-01/grok-live.json)一致）的内嵌官方文档与字符串，本机日常缓存从未被读取：

- auth.json 为 issuer 键控存储：`{"https://accounts.x.ai/sign-in": {"key": <Bearer>}}`，官方 jq 示例即 `.key`。
- OAuth issuer 与 device flow（`--device-auth`，scope `grok-build`）为官方固定值；token 七天过期，仅官方 `grok login` 刷新。
- 订阅调用协议：`POST https://cli-chat-proxy.grok.com/v1/chat/completions`，必需 `Authorization: Bearer`、`X-XAI-Token-Auth: xai-grok-cli`、`x-grok-model-override` 三个 header，多数模型仅支持 streaming。该协议供后续 Forwarder 组件使用，本组件不发起网络请求。

`NewFileCredential` 仅接受 Git 外规范私有路径 `…/grok/home/.grok/auth.json` 与可信作用域（路线/账号/workspace/credential identity/官方 https issuer）。读取使用描述符相对遍历：逐层拒绝 symlink 与 Git 祖先，末三层目录与文件必须本 UID、无 group/other 位；文件 0600、单硬链接、256 KiB 上限，打开前后 FD/路径身份与字节数复核。构造即冻结内容摘要与文件/目录身份。

`Load` 逐次重查调用 context、冻结 target（路线 revision、账号、workspace、credential identity、subscription 计费、固定 Runtime、非插件）与文件身份/摘要；任何漂移 fail-closed，轮换或替换需要新的可信登记。环境变量（GROK_AUTH、GROK_AUTH_PATH、XAI_API_KEY 等）、Keychain、Native account/read 或 ambient 认证一律不作为来源。

## 解析边界

顶层必须恰好一个官方 issuer 键；entry 必须含非空可打印 `key` 字符串（≤16 KiB）。`api_key`、`api-key`、`XAI_API_KEY`、`deployment_key` 字段非 null 即拒绝：PAT 或部署密钥不能顶替订阅会话。解码拒绝重复键、null 信封、尾随内容、超深/超多字段与非法 UTF-8。

闭源导致 entry 完整字段清单不可枚举：其余字段有界容忍但一律不读取、不解析、不推断——无 expiry、scope、账号、tier 或额度权威来自本文件。真实登录后观察到实际 schema 时再收紧白名单（已登记后续项）。token 仅作控制器私有 Bearer 供后续订阅 Forwarder；String/GoString/JSON 输出一律脱敏。

## 验证、剩余和回退

[组件报告](../work-items/WP-13/PRIVATE-CREDENTIAL-01/summary.md)记录 RED→GREEN、281 子场景、三项有效 mutation（digest/workspace 绑定/权限各一个行为失败）与 Go1.26.3 CLI/GUI/vet。合成 fixture 仅为不透明字符串，无真实账号。

```sh
go test -mod=readonly -tags fusion,nogui -race -count=1 -timeout=90s \
  ./internal/fusion/runtime/grok -run 'TestGrokPrivateCredential'
python3 scripts/fusion/build-dev.py
```

本人真实登录、真实 tier/费用/额度、订阅 Forwarder、Factory 注册、工具/写入/恢复与完整工程 Smoke 未完成；最终 T01–T60 不升级。回退可停用该读取器；不修改认证缓存、日常客户端、原 Magpie UI 或 Store schema。
