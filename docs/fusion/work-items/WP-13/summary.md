# WP-13 进度

状态：in_progress。已完成 [PROTOCOL-01](PROTOCOL-01/summary.md)：固定 Native headless fixture、辅助调用/工具声明调查、有界协议观察器及回归。

[RESUME-01](RESUME-01/summary.md) 已接通成功归档的可信内部 ResumeCheckpoint：新 prepared run/Root/grant/端口、准确原 UUID、历史及新 Read、私有 HMAC mapping，实际 Native 覆盖中文 cwd、完整 Store/Archives/Manager/Adapter 重开、取消和漂移拒绝。明确恢复的内部 API/幂等 receipt 已在 [WP-15-RESUME-API-01](../WP-15/RESUME-API-01/summary.md) 接通；产品 Worker/实际 bootstrap、真实 upstream/Quota/Source整体准入仍待完成，通用无准确输入的 Resume 继续 unsupported。

[ARCHIVE-01](ARCHIVE-01/summary.md) 已完成同 Adapter 成功 Handle 经实际 wait/StopProof/Release 后的可信归档：完整 14 Native 文件、原批准 Read、冻结目标/目录身份与私有 HMAC seal，支持重开核验。实际文本/Read 归档通过，失败/取消拒绝；可信恢复消费者和实际受管 Resume 已在 RESTORE-01/RESUME-01 接通；整个 Source/当前项目权限服务仍待完成。

[RESTORE-01](RESTORE-01/summary.md) 已完成包内恢复准备与历史 Read 导入：准确 seal/原成功 released run、distinct prepared run/new generation/attempt、完整 Target/cwd/Source 重核，旧批准配对进入新 per-run HTTP 校验。每次 HTTP 继续独立 Permit，历史 Read 不占新 tool turn；Native seed/launch/私有持久恢复 mapping 及实际 Resume 已在 RESUME-01 接通；产品幂等恢复 receipt 仍待完成。

[RESUME-PROBE-01](RESUME-PROBE-01/summary.md) 已完成固定版本 15 个实际恢复诊断：准确 UUID 与复制会话目录可保留旧对话；缺 transcript 可空历史成功，更换模型和另一 cwd 参数被接受，旧工具结果进入后续 HTTP。此项只完成调查，不启用 Resume；完整归档、可信 stopped owner/目标绑定、历史授权及受管持久恢复仍待按 [grok-resume.md](../../contracts/grok-resume.md) 实施。

[WP-15-GROK-01](../WP-15/GROK-01/summary.md) 已通过固定 Grok 1.0.48 的 7 个实际控制器/Adapter 生命周期场景及真实停止证明释放，包含 Read、幂等 receipt、断线、暂停、两类取消和关闭；合成上游与准入没有升级为真实 subscription 准入，产品 Worker/恢复 API 仍待完成；可信内部 Native Resume 见 RESUME-01。

受管只读 grok.Adapter 已实现可信启动/准确 checkpoint 恢复、协议/text终态观察及取消/StopProof/release；真实NativeForwarder/私有X认证/额度计费、稳定Source、写权限和产品注册仍待完成。当前无准确输入的 Session/Adapter.Resume 明确unsupported，未注册真实Grok Worker。

Parent 关联 T01/T02/T14/T15/T25/T31/T32/T35/T44/T56/T59 只新增了组件级证据，60 类最终 Gate 全部仍 not_run。Jev off，现有 GLM/Codex 行为和产品执行开关保持原状。

合同：[headless 观察](../../contracts/grok-headless-protocol.md)，[锁定能力](grok-lock-capability.md)。

已完成 [CALLS-01](CALLS-01/summary.md)：每 HTTP 的冻结绑定/ModelAudience/Scheduler 预算、严格文本 SSE 验证和 Native 标题/重试合成诊断。真实 Store/Scheduler 持久化记账通过，但没有生产 NativeForwarder、真实额度准入或受管 Worker。合同：[grok-call-gate.md](../../contracts/grok-call-gate.md)。

[TOOLS-01](TOOLS-01/summary.md) 调查了实际 read_file/后续请求与原生配置读取：较宽 private HOME profile 下合成 key 会进入工具输出，而工具被 OS 拒绝后 Native 仍可能 exit0。现有文本 Gate 的实际 Native 攻击回归在工具开始前拦截；继续不支持生产工具。

[READ-01](READ-01/summary.md) 已完成可选受控文本Read：Native前路径/文件批准、trace/后续HTTP关联、JSON转义凭据保护，真实固定Native+Store/Scheduler的13行项目读取与私有config拦截通过。生产NativeForwarder/Worker、OS隔离、写工具、Resume和真实准入继续未完成。

[CHANNEL-01](CHANNEL-01/summary.md) 已连接可信 GrokChannel、独占双栈端口、私有GROK_HOME/config与公共Supervisor。实际固定Native8场景通过真实Store/Manager/Scheduler和default-deny profile，包含读取、拒绝、重试、预算、取消/StopProof/release；C探针验证OS边界，Claude兼容性回归。完整Adapter/真实NativeForwarder/准入、Resume/写权限/产品注册仍未完成。合同：[grok-managed-channel.md](../../contracts/grok-managed-channel.md)。

[ADAPTER-01](ADAPTER-01/summary.md) 已实现可信readonly Adapter：Scheduler重核、复制prompt、私有prompt-file、全新UUID/固定argv、ReadTools/Gate/channel/Supervisor、终态Observation与可信release。实际Native8场景及snapshot/40KB/drift3场景通过；完整真实路线、写/effort/Resume、产品注册与final Gate仍未完成。合同：[grok-adapter.md](../../contracts/grok-adapter.md)。

[WP-15-CHECKPOINT-API-01](../WP-15/CHECKPOINT-API-01/summary.md) 已把同Adapter成功归档接到owned Controller/Management生产入口，取得stable opaque reference后再调用明确恢复；真实Native的批准Read、取消及断线回归通过。进程重启不通过producer重造停止证明，产品注册/真实准入/整个Source仍待完成。

后续 [WP-15-SOURCE-GUARD-01](../WP-15/SOURCE-GUARD-01/summary.md) 完成私有来源记录接入执行/归档边界，固定Grok/Claude Native源漂移拒绝成功并实际停止/释放通过。完整Fusion460PASS22SKIP0FAIL，Native7顶层/17子测试通过；整体产品登记/真实路线/最终Gate仍未完成。

## 私有官方登录缓存服务

[PRIVATE-CREDENTIAL-01](PRIVATE-CREDENTIAL-01/summary.md) 新增 `grok.FileCredential`：从 Git 外私有官方 auth.json 读取不透明订阅 Bearer，冻结路线/账号/workspace/credential identity/官方 issuer、文件与目录身份，拒绝轮换、权限漂移、symlink、硬链接与 API key 形态混入。格式证据来自固定 1.0.48 二进制内嵌官方文档（issuer 键控 `.key`、cli-chat-proxy 三 header 协议），本机日常缓存未被读取。grok 包 281 子场景 race、三项有效 mutation 与 Go1.26.3 CLI/GUI/vet 通过。合同见 [Grok 私有凭据](../../contracts/grok-private-credential.md)。真实登录、订阅 Forwarder、tier/费用/额度、Factory 注册与工具/写入/恢复仍待完成；父包保持 in_progress。

## 固定订阅 Forwarder

[SUBSCRIPTION-FORWARDER-01](SUBSCRIPTION-FORWARDER-01/summary.md) 实现固定官方 CLI chat proxy 端点的单次 HTTPS、FileCredential Bearer、官方三必需 header、受检系统 CA、无代理/redirect/retry/API fallback、有界取消与完整 SSE 后交付。模型证明为 chunk `model` 回显全等校验（回显一致性，非独立 header 证明）；反射按 data 帧逐帧 JSON 检测。真实 TLS 回环 16 子场景、三项有效 mutation、grok/bootstrap/control 三包 race 与 Go1.26.3 CLI/GUI/vet 通过。合同见 [订阅 Forwarder](../../contracts/grok-subscription-forwarder.md)。真实登录、tier/费用/额度、Factory 注册与工具/写入/恢复仍待完成；本组件不能升级路线准入、父包或最终验收。
