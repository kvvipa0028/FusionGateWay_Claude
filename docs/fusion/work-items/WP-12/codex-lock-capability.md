# Codex 协议与锁定能力

当前 native pin：`0.160.0`，本机 executable SHA-256 见 schema manifest。publisher trust、账户权益和全部模型调用的执行控制仍 `unverified`。

`Client` 是私有协议状态机，`StdioPeer` 是有界传输；它们尚不是可启动生产任务的 `runtime.Adapter`。注册路线仍保持禁止真实生成。

## 接口边界

`Binding` 固定 `Scope`、`Identity`、`ExecutionTarget`、`Cwd`、`CodexHome`。调用者必须来自受信任管理层，提供 Store/lease/stage grant 的当前检查与 registry 的真实准入证据；产品请求不得直接提供 callbacks 或 Boolean 准入。

协议解析使用锁定版本的关键字段投影，并拒绝 duplicate/case-colliding keys、null/trailing values、超限和身份漂移。它不是通用全量 JSON Schema validator；schema-validation.json 验证的是完整 synthetic vectors。

私有 metadata 启动已有 [supervised duplex stream](../../contracts/codex-bootstrap-channel.md)，但生产 Codex Factory、登录管理、全部模型调用与 stage grant 控制尚未提供，所以 `generationAdmitted` 默认 nil/false，不能用 fixtures 的 true callback 作为真实模型准入。

Native `modelVerification` 中的 `trustedAccessForCyber` 是能力访问状态，不是本次上游实际模型证明。requested、resolved 与 upstream reported 模型不能混为一项。

## 状态与恢复

turn/start 发出前先进入 execution_uncertain；成功收到 inProgress 的匹配确认后才转 running。重放 uncertain turn 禁止。turn/interrupt 确认后仍等待原生终态与独立 OS stop proof；传输关闭不能释放 pool/write reservation。

当前 Resume 仅覆盖同一个 Client 持有的 thread 与相同账号/workspace/credential identity/generation，调用前重新验证准入。跨进程恢复、原生凭据续期和账户变更的实测尚未完成。

## 能力证据

- synthetic protocol success/failure/interruption 与取消竞争可验证。
- schema 与 Native CLI pin 的 DTO 结构匹配可验证。
- 原生 App Server 在旧 WP-11 单进程沙箱内启动失败；[NATIVE-ISOLATION-01](NATIVE-ISOLATION-01/summary.md) 已通过固定 Native 的 metadata 握手与受管停止验证。仅专用通道可读取限定 Codex 偏好域，原生 MDM 保留；通用 Worker 的权限不变。
- 原生 network/child/resume、隐藏模型调用、quota 与计费归属尚不可验证。

冻结源码参考：[initialize](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/app-server/src/request_processors/initialize_processor.rs)、[managed preferences](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/config/src/loader/macos.rs)。本地 CLI schema 的指纹与请求 vectors 随代码保存。

## Responses HTTP 组件

[CALLS-01](CALLS-01/summary.md) 已验证单入口、每次 Permit/持久预算、冻结 Binding 和有界完整 SSE 的保守文字投影。CALLS-01 当时没有实际 Native model HTTP；后续 [CHANNEL-01](CHANNEL-01/summary.md) 补充实际 scoped Native HTTP，真实 Forwarder、私有官方登录及全部子调用仍无完整证据；production generation remains unverified。现有 metadata-only CodexChannel 不因此增加网络权限。
