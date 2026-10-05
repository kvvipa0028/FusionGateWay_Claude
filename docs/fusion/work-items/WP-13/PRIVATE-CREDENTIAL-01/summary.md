# WP-13 · PRIVATE-CREDENTIAL-01

完成 Grok 官方 OAuth 私有文件凭据服务；父 WP-13、WP-17、Gate A 和整体目标仍未完成。基线 `820d21fc56cce99e3af11af9edf9bae0dc6c02fd`，工作区干净起步；未新增 Agent。合同见 [Grok 私有凭据](../../../contracts/grok-private-credential.md)。

`grok.FileCredential` 从 Git 外私有官方 auth.json 读取不透明订阅 Bearer：冻结精确路线/账号/workspace/credential identity/官方 issuer、文件与末三层目录身份，拒绝替换、轮换、权限漂移、symlink、硬链接别名与非规范路径。不解析 token 或 JWT 推定账号、tier、地区、计费或额度；API key 形态字段混入时拒绝，不提供 PAT/deployment key 退路。

## 官方格式证据（固定 1.0.48 二进制，只读分析）

Grok CLI 闭源；本机日常 `~/.grok` 存在真实缓存但[未被读取](#未完成与范围)，格式以固定二进制内嵌官方文档与字符串为唯一来源：auth.json 为 issuer 键控存储，官方 jq 示例 `."https://accounts.x.ai/sign-in".key` 即 Bearer；订阅调用协议为 `POST https://cli-chat-proxy.grok.com/v1/chat/completions`，必需 `Authorization: Bearer`、`X-XAI-Token-Auth: xai-grok-cli` 与 `x-grok-model-override` 三个 header，多数模型仅支持 streaming，token 七天过期由官方 `grok login` 刷新。OAuth device flow scope 与 issuer 常量亦在二进制内确认（`grok-build`、accounts.x.ai）。

闭源导致 entry 的完整字段清单不可枚举。裁定：顶层必须恰好一个官方 issuer 键；entry 必须含非空可打印 `key`（≤16 KiB）；其余字段有界容忍（严格深度/计数/重复键解码，总文件 ≤256 KiB）但一律不读取、不推断。代价是格式校验弱于 Codex 版；真实登录后观察到实际 schema 再收紧（后续项）。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 初始 RED | FileCredentialScope/NewFileCredential 未定义，编译失败 exit1 | [初始 RED](grok-cred-red.log) |
| 最终目标 | grok 包全部测试 PASS：281 子场景 0 FAIL 0 SKIP，race exit0 | [目标日志](grok-cred-green.log) |
| 有效 mutation | 删除 digest 复查→rotated 接受；删除 workspace 绑定→drift 接受；放宽 0600→0644 接受，各一个行为子场景实际 FAIL/exit1 | [rotation](mutation-rotation.log)、[binding](mutation-binding.log)、[permissions](mutation-permissions.log) |
| Go1.26.3 CLI/GUI/full vet | 三项实际 exit0 | [构建日志](grok-build-dev.log) |

覆盖矩阵：target 漂移（account/workspace/identity/route/revision/billing/runtime/plugin/cancel/nil-context）；schema（双 issuer、错误 issuer、缺 key、空 key、超长 key、控制字符、entry 非对象、api_key 混入、重复 JSON 键、尾随内容、超限文件、空文件）；文件安全（轮换、文件/目录权限、symlink 文件/目录、硬链接、浅路径、错误文件名）；redaction（String/GoString/JSON 不泄 bearer、路径或 issuer）。Mutation 使用 Go overlay，不修改生产文件。[test-results.json](test-results.json) 记录命令与计数。

## 未完成与范围

本人 Grok 真实登录仍未发生，私有缓存不存在，本轮真实供应商请求 0。本机日常 `~/.grok` 缓存未被读取或借用。生产订阅 Forwarder、可信账号/tier/费用/额度、Factory 注册、工具/写入/恢复与最终 Gate 仍待完成；本组件不能升级路线准入、父包或 T01–T60。
