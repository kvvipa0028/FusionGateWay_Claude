# WP-13 进度

状态：in_progress。已完成 [PROTOCOL-01](PROTOCOL-01/summary.md)：固定 Native headless fixture、辅助调用/工具声明调查、有界协议观察器及回归。

[ARCHIVE-01](ARCHIVE-01/summary.md) 已完成同 Adapter 成功 Handle 经实际 wait/StopProof/Release 后的可信归档：完整 14 Native 文件、原批准 Read、冻结目标/目录身份与私有 HMAC seal，支持重开核验。实际文本/Read 归档通过，失败/取消拒绝；可信恢复消费者、整个 Source/当前项目权限及实际受管 Resume 仍待实现。

[RESTORE-01](RESTORE-01/summary.md) 已完成包内恢复准备与历史 Read 导入：准确 seal/原成功 released run、distinct prepared run/new generation/attempt、完整 Target/cwd/Source 重核，旧批准配对进入新 per-run HTTP 校验。每次 HTTP 继续独立 Permit，历史 Read 不占新 tool turn；Native seed/launch/持久恢复 receipt 及实际 Resume 仍未实现。

[RESUME-PROBE-01](RESUME-PROBE-01/summary.md) 已完成固定版本 15 个实际恢复诊断：准确 UUID 与复制会话目录可保留旧对话；缺 transcript 可空历史成功，更换模型和另一 cwd 参数被接受，旧工具结果进入后续 HTTP。此项只完成调查，不启用 Resume；完整归档、可信 stopped owner/目标绑定、历史授权及受管持久恢复仍待按 [grok-resume.md](../../contracts/grok-resume.md) 实施。

[WP-15-GROK-01](../WP-15/GROK-01/summary.md) 已通过固定 Grok 1.0.48 的 7 个实际控制器/Adapter 生命周期场景及真实停止证明释放，包含 Read、幂等 receipt、断线、暂停、两类取消和关闭；合成上游与准入没有升级为真实 subscription 准入，产品 Worker/Native Resume 仍待完成。

受管只读 grok.Adapter 已实现生产启动、协议/text终态观察及取消/StopProof/release；完整接入中的真实NativeForwarder/私有X认证/额度计费、稳定Source、写权限、准确Resume与产品注册仍待完成。当前Session/Adapter.Resume明确unsupported，未注册真实Grok Worker。

Parent 关联 T01/T02/T14/T15/T25/T31/T32/T35/T44/T56/T59 只新增了组件级证据，60 类最终 Gate 全部仍 not_run。Jev off，现有 GLM/Codex 行为和产品执行开关保持原状。

合同：[headless 观察](../../contracts/grok-headless-protocol.md)，[锁定能力](grok-lock-capability.md)。

已完成 [CALLS-01](CALLS-01/summary.md)：每 HTTP 的冻结绑定/ModelAudience/Scheduler 预算、严格文本 SSE 验证和 Native 标题/重试合成诊断。真实 Store/Scheduler 持久化记账通过，但没有生产 NativeForwarder、真实额度准入或受管 Worker。合同：[grok-call-gate.md](../../contracts/grok-call-gate.md)。

[TOOLS-01](TOOLS-01/summary.md) 调查了实际 read_file/后续请求与原生配置读取：较宽 private HOME profile 下合成 key 会进入工具输出，而工具被 OS 拒绝后 Native 仍可能 exit0。现有文本 Gate 的实际 Native 攻击回归在工具开始前拦截；继续不支持生产工具。

[READ-01](READ-01/summary.md) 已完成可选受控文本Read：Native前路径/文件批准、trace/后续HTTP关联、JSON转义凭据保护，真实固定Native+Store/Scheduler的13行项目读取与私有config拦截通过。生产NativeForwarder/Worker、OS隔离、写工具、Resume和真实准入继续未完成。

[CHANNEL-01](CHANNEL-01/summary.md) 已连接可信 GrokChannel、独占双栈端口、私有GROK_HOME/config与公共Supervisor。实际固定Native8场景通过真实Store/Manager/Scheduler和default-deny profile，包含读取、拒绝、重试、预算、取消/StopProof/release；C探针验证OS边界，Claude兼容性回归。完整Adapter/真实NativeForwarder/准入、Resume/写权限/产品注册仍未完成。合同：[grok-managed-channel.md](../../contracts/grok-managed-channel.md)。

[ADAPTER-01](ADAPTER-01/summary.md) 已实现可信readonly Adapter：Scheduler重核、复制prompt、私有prompt-file、全新UUID/固定argv、ReadTools/Gate/channel/Supervisor、终态Observation与可信release。实际Native8场景及snapshot/40KB/drift3场景通过；完整真实路线、写/effort/Resume、产品注册与final Gate仍未完成。合同：[grok-adapter.md](../../contracts/grok-adapter.md)。
