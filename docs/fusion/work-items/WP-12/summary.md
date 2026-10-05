# WP-12 进度与验证证据

状态：`in_progress`。本次完成子工作项 `WP-12-PROTOCOL-01`：锁定 Codex 0.160.0 的协议客户端与私有 stdio 传输。**完整官方 Runtime Adapter、真实登录与严格执行准入尚未完成。**

## 已交付

- `internal/fusion/runtime/codex/`：initialize/initialized、account/read、thread/start/resume、turn/start/interrupt 和原生事件状态机。
- 冻结模型、effort、账号/workspace/credential identity、generation、项目 cwd 和独立 Codex home；每次 RPC 前后核对当前身份。
- initialize 显式选择 gateway OAuth，禁止恢复自动登录；不调用登录或任意管理方法。私有登录和续期尚待受信任管理层实现。
- 主模型、子 Agent、自动压缩、MCP、动态工具等控制无法证明时返回 `unverified`；未知 item 与只读阶段的 fileChange 进入 `execution_uncertain`。
- Native 启动确认丢失后禁止重放。只有匹配 thread/turn 的原生 completed/failed/interrupted 才产生协议终态；文本、进程退出、取消 RPC 确认都不能代替它。
- stdio 请求 ID 配对、重复/混合字段拒绝、有界帧/累计输出/通知队列、超时关闭、权限请求拒绝。真实发现并修复最终响应与 EOF 竞争导致确认丢失的问题。
- `testdata/native-0.160.0/`：完整 synthetic DTO vectors 与 13 个本地 Native JSON Schema 的指纹；未导入真实账号或凭据。

## 实际验证边界

详见 `test-results.json`、原始 RED/GREEN 日志与构建报告。Native schema 校验通过仅证明 fixtures 与锁定版本的结构匹配。

`native-handshake.json` 记录一次无账号、无网络、无 fork 的真实 Native 启动检查：CLI 在任何握手响应前以 exit 1 退出，stderr 为 `Error: Failed to synchronize managed preferences`。已 wait/reap；模型与登录请求数均为 0。该结果是已记录的启动阻断，不是握手成功或生产停止证明。

冻结版本的 [官方 managed preferences 实现](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/config/src/loader/macos.rs) 使用 CoreFoundation 同步 `com.openai.codex` 的受管配置。以上为 PROTOCOL-01 当时的启动阻断；后续 NATIVE-ISOLATION-01 已以专用通道解决，通用 WP-11 沙箱不增加这组权限，见下文。

## 未完成

- 将实际生成会话接入生产 Adapter/Factory 与 stage grant、全部模型调用控制；metadata 启动已有 Store/Supervisor 双向管道及原生停止证明，不能代替生成会话证据。
- 用户自己的独立 ChatGPT 登录 home、账号/workspace 身份验证、同账号凭据续期与真实恢复验证。
- 所有模型子调用/摘要的冻结目标、预算/额度控制与实际计费路线证明。
- 原生成功/失败/中断、实际网络与子进程的受控验证，以及 WP-17/Gate A 的三路线实测。

T01/T02/T14/T15/T25/T31/T32/T35/T44/T53/T59 的最终端到端状态继续为 `not_run`。已验证协议不代表真实路线已准入；其余独立工作继续实施。

## 协议解析追加修复

[WP-12-PARSE-01](PARSE-01/summary.md) 修复 Unicode case-fold 重复字段与 invalid UTF-8，24 项 Codex race tests 通过。完整 Native 接入状态不变。

## 私有 Native 启动组件

[NATIVE-ISOLATION-01](NATIVE-ISOLATION-01/summary.md) 实现固定 Codex 的专用只读偏好许可与受管双向 stdio，保留 MDM，拒绝 Keychain/其他偏好域/网络/fork。真实 Client+StdioPeer+Supervisor+Store 验证15场景、12次 Native、3类 intent 前拒绝；所有执行均 wait/StopProof/release，空账号/私有 home，登录、模型与额度请求0。另修复实际通知 `emittedAtMs` 被错误拒绝的问题。合同见 [Codex bootstrap](../../contracts/codex-bootstrap-channel.md)。完整父 WP-12、真实账号准入与最终 Gate 仍未完成。

## 受控模型 HTTP 组件

[CALLS-01](CALLS-01/summary.md) 增加 authenticated text-only Responses Gate，冻结 target/identity/effort，每次发送和429重试通过持久 Scheduler 预算；完整有界 SSE 与真实上游 ReportedModel 校验后才交付，未知工具/事件、未完成项、漂移和凭据反射均拒绝。仅合成 transport/准入验证；未接入实际 Native HTTP，不开放原生生成或真实订阅路线。合同见 [codex-call-gate.md](../../contracts/codex-call-gate.md)。父 WP-12 与最终 Gate仍未完成。

## 独占 HTTP 通道

[CHANNEL-01](CHANNEL-01/summary.md) 完成固定 Native 的私有 custom stage provider、独占双栈端口、仅阶段 Bearer、grant 激活先于 driver、严格观测字段。实际 Native 验证7个新增场景，包含文字成功、429终止、unsafe-response后重试拒绝、MaxCalls=2三轮预算拒绝、inflight取消、激活顺序及激活失败；真实 wait/StopProof/release。上游与账号为合成 fixture，未导入日常登录，默认 production Client/Factory 准入仍关闭；父工作包和最终 Gate 不升级。

## 类型化阶段协议客户端

[GATEWAY-CLIENT-01](GATEWAY-CLIENT-01/summary.md) 增加可信NewGateway，固定本地stage provider与独立上游Identity/准入，原生account=null不打开默认openai客户端。实际typed Client完成thread/turn/严格通知/成功失败/turn interrupt；丢失thread确认、未观察终态item、迟到取消确认均拒绝。五个新增Native场景与前项回归通过；生产Adapter/官方登录/Forwarder/真实quota/工具/恢复仍未完成，父WP和最终Gate不升级。

## 受管只读 Adapter

[ADAPTER-01](ADAPTER-01/summary.md) 已将类型化 Client、独占 HTTP 通道接到真实 Store/Scheduler/Manager/Supervisor，强制来源证明和私有目录、冻结 prompt/Target、逐次 Permit/预算、原生终态文字、实际停止证明及 reservation 释放。13 个实际固定 Native 生命周期场景和 6 类启动前身份拒绝通过。真实官方账号/Forwarder/quota、产品 Controller/Factory 注册、工具/写入/恢复及最终 Gate 仍未完成；父 WP-12 继续 in_progress。

后续 [WP-15-CODEX-01](../WP-15/CODEX-01/summary.md) 已验证 BindAdapter/Controller 的实际 Codex 生命周期，并将 ValidateLaunch 接到 intent 前检查；相同静态 Target 合同用于 NewGateway 与 Adapter preflight。13 次 Adapter 原生回归通过，真实账号/Forwarder/quota、产品 HTTP/Factory、写/工具/恢复与最终 Gate 仍待完成，父包状态保持。

[WP-15-CODEX-HOST-01](../WP-15/CODEX-HOST-01/summary.md) 已补充 Codex authenticated loopback HTTP/Factory 的5个实际 Native 生命周期场景、2类 preintent 拒绝、鉴权和幂等及真实停止后 Store 重开。Grok 同路径5次进程回归通过；完整 Fusion race 514 PASS/30 SKIP/0 FAIL。现有生产代码无需修改，真实账号/Forwarder/quota/生产注册、CLI/GUI、写入/工具/恢复、工程闭环和最终Gate仍未完成；父包继续 in_progress。

## 私有官方登录缓存服务

[PRIVATE-CREDENTIAL-01](PRIVATE-CREDENTIAL-01/summary.md) 新增 `codex.FileCredential`：从 Git 外私有官方 auth.json 读取不透明 OAuth token，冻结路线/账号/workspace/credential identity、文件与目录身份，拒绝替换、模式混用、权限漂移及解析歧义。官方设备登录写入逻辑已核对；缺失账号不能从 JWT 补出或推定权益。6 个目标测试/75 子场景、受影响四模块 race 与 Go1.26.3 CLI/GUI/vet 通过，三项有效 mutation 均被断言捕获。合同见 [Codex 私有凭据](../../contracts/codex-private-credential.md)。本人真实登录缓存仍未产生，生产 Forwarder、可信账号/费用/额度、Factory 注册与完整父包/最终 Gate 尚未完成。

## 固定订阅 Forwarder 与实际模型来源

[SUBSCRIPTION-FORWARDER-01](SUBSCRIPTION-FORWARDER-01/summary.md) 已实现固定 ChatGPT 订阅端点的单次 HTTPS、FileCredential、受检系统 CA、无代理/redirect/retry/API fallback、有界取消及完整 SSE 后交付。官方实际模型来源为 HTTP OpenAI-Model 或 SSE response.headers，普通 response.model 不能作为证明；新增支持保留全部状态、模型漂移及秘密反射拒绝。TLS/Gate 合成回归、四种 mutation 和固定 Native 消费模型 header 的10场景实际停止均通过。合同见 [订阅 Forwarder](../../contracts/codex-subscription-forwarder.md)。真实账号登录、地区/费用/额度、Factory 注册、续期和完整工具/写入/恢复仍待完成；本组件不能升级路线准入、父包或最终验收。
