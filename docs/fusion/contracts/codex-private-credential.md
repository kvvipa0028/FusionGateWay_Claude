# Codex 私有 OAuth 缓存读取合同

本合同对应 [WP-12 PRIVATE-CREDENTIAL-01](../work-items/WP-12/PRIVATE-CREDENTIAL-01/summary.md)。它实现可信控制器的本地凭据服务，供后续订阅 Forwarder 使用；尚未接到生产 Factory，不提供 HTTP、CLI 或 UI 凭据导入接口，不授予生成权限。Jev off。

## 来源与绑定

缓存只能来自[独立官方设备登录入口](../integration/private-runtime-login.md)准备的 Git 外目录：

```text
/Users/zhaojianzhi/.local/share/fusion-gateway/auth/codex/home/.codex/auth.json
```

构造参数为可信服务端提供的绝对、clean 路径和 `FileCredentialScope`：路线 ID/Revision、本地 Account、Workspace、CredentialIdentity 及明确登记的 UpstreamAccount。末三级必须为 `codex/home/.codex`。这些字段不是客户端提交的权限声明，也不证明真实账号归属、地区、套餐、计费或共享额度池。

构造时读取并冻结文件内容摘要、device/inode 和末三级目录身份；摘要只留在私有内存，不写入任务 DTO、日志或证据。每次 `Load` 重读并核对全部冻结身份，以及 ExecutionTarget 的路线/版本、账号、workspace、credential identity、`BillingPath=subscription`、`RuntimeVersion=0.160.0` 和无 PluginVersion。没有环境凭据、Keychain、旧 provider、日常 HOME、备用文件或自动替换路径。

## 官方格式及拒绝条件

依据固定版本的官方源码：[AuthDotJson 与文件保存](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/login/src/auth/storage.rs)、[TokenData](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/login/src/token_data.rs)、[设备登录](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/login/src/device_code_auth.rs)、[persist_tokens_async](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/login/src/server.rs)。公共源码指纹见[来源记录](../work-items/WP-12/PRIVATE-CREDENTIAL-01/public-source-info.json)，不包含私有缓存。

- 固定设备登录会写 `auth_mode=chatgpt`；Fusion 要求该值明确存在，不猜测旧格式的缺省模式。
- `tokens` 必须有原名 `id_token`、`access_token`、`refresh_token`、`account_id`，无额外字段。前三项为非空、无空白/控制字符的 ASCII 字符串，各至多 16 KiB；作为不透明凭据读取，不解析 JWT。
- 官方 `account_id` 是可空字段。Fusion 仅接受非空且与独立登记 UpstreamAccount 完全相等的缓存；缺失/null/不匹配均返回 `ErrIdentity`，不能从 JWT 补出账号。该限制是执行绑定条件，不是账号权益证明。
- `OPENAI_API_KEY`、`agent_identity`、`personal_access_token`、`bedrock_api_key`、`bedrock_access_keys` 只允许缺失或 null。其他 auth_mode、未知字段、字段大小写别名、重复键、invalid UTF-8、尾随 JSON 值和非对象均拒绝。
- `last_refresh` 可以缺失/null；存在时要求 RFC3339 时间字符串。该字段不用于判定 token 有效期、当前登录状态或额度。

整个文件至多 256 KiB；从 `/` 开始逐层使用目录 FD 和 `openat(O_NOFOLLOW)`，任意 symlink、Git 祖先或 `.git` 标记均拒绝。末三级目录要求当前 UID 且无 group/other 权限；文件要求当前 UID、普通文件、权限严格 `0600`、单个 hardlink。FIFO、超限、空文件、读取期间的文件身份/大小/时间漂移均拒绝。darwin/linux 使用这些实际系统检查；其他平台返回 `ErrIdentity`，不模拟成功。

## 生命周期与信息边界

`Load` 遇到取消或 nil Context、目标不匹配、文件/目录替换、内容变化或权限漂移时返回静态 `ErrIdentity`，不回显路径、底层文件错误、账号或 token。Credential 与 FileCredential 的 `String`/`GoString` 均脱敏，JSON 不导出秘密；返回的 access/id/refresh token 和 account ID 是包内私有字段，只供可信控制器处理。

服务不刷新、不覆盖、不 logout，也不修改原缓存。凭据更新后，原服务停止接受它，需可信管理层重新核验账号、重新登记 credential identity 和对应配置/任务版本；不能让已冻结的任务静默使用新凭据。生产续期流程仍待实现，不能以重建本对象代替续期授权。

后续 Forwarder 必须继续遵守[逐次 HTTP 控制](codex-call-gate.md)、冻结目标、独立权益/费用/额度证据和当前 grant。读取成功、JWT 声明或本地摘要都不能设置 Registry/Inspection 准入。当前没有真实缓存加载、模型调用、上游实际模型或完整工程 Smoke 证据。

## 验证与复现

本组件使用临时私有 HOME/XDG 和环境白名单、合成文件、不透明测试 token，不继承真实认证。目标测试：

```sh
go test -mod=readonly -tags fusion,nogui -race -count=1 \
  ./internal/fusion/runtime/codex -run TestCodexPrivateCredential
python3 scripts/fusion/build-dev.py
```

Go 必须为 1.26.3。首个命令只运行合成凭据测试；构建脚本自行建立隔离环境。实际受影响模块回归、跳过项、RED/GREEN 与有效 mutation 记录见[组件报告](../work-items/WP-12/PRIVATE-CREDENTIAL-01/summary.md)。本组件未改变 UI、原 Magpie 接入、Store schema 或最终工作包状态。
