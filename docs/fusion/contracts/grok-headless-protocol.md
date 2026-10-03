# Grok headless 协议观察合同

本包只解析固定 Grok `1.0.48/b94d5072c95f` 的 `streaming-json` NDJSON，不启动 Worker，不登记真实路线。可执行文件 SHA256 固定为 `1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05`。`Binding` 中的版本/hash 是可信调用方的声明，调用方仍须实际核验文件；字符串本身不是文件或 publisher 验证证据。

`grok.New` 冻结 run/generation/role、准确 Native UUID、canonical absolute cwd、固定版本/hash、model、只读工具集合及 MaxTurns。当前声明允许 `read_file/list_dir/grep`，实际 fixture 验证的是 `read_file`；任何真正 `tool_call/tool_call_update` 都返回 unsupported，尚未授权执行这些工具。每个 Event/Cancel/Finish 都重查可信 current 回调，传给回调及内部保存的 slice 均独立复制。旧 run/generation 帧被拒绝；当前身份失效使执行变为 execution_uncertain。

已支持 `available_commands`、`text/thought`、`usage`、`end`、`error`、`max_turns_reached`。工具声明必须和冻结集合完全一致；commands 只表示 Native 公布的交互命令，不授予 slash command、always-approve、修改设置或未来权限。text/thought 不进入 Outcome，也不作为工程产物或成功证明。

只有有效终态加实际 wait 后传入的 exit code 0 才得到协议层 succeeded。end 必须包含准确 sessionId、合法 requestId、end_turn、预算内 num_turns、唯一匹配的 modelUsage 和一致、非负、有界的 token/call 数。usage 是信息帧，只保存终态汇总，不能重复相加。当前尚未验证 cache/reasoning token 语义，非零值拒绝接受。缺 end、重复 end、end 后帧、成功 end 后非零退出、身份变化均不能成功；error/max_turns 得到 failed；Cancel 是意图，清洁终态变为 interrupted，不等于进程已停。

单帧最大 1MiB，总流最大 8MiB/4096 帧，JSON 深度最多 32。重复 key（含 Unicode/case fold 碰撞）、无效 UTF-8、NUL、trailing JSON、null 及格式错误拒绝。错误只返回固定类别，不回显 prompt、路径、provider 或凭据文本。未知帧、工具执行、压缩、记忆/子 Agent 流以及 Resume 明确 unsupported。

Outcome 的 NativeModel 来自匹配的 end.modelUsage key；NativeModelCalls 仅表示其声明的调用数。UpstreamReportedModel 保持 null，StrictLockVerified/AllCallsVerified/BillingVerified/QuotaVerified/StoppedVerified 始终 false。cwd、effort、账号和上游身份没有由该 stdout 协议独立证明。完整 Adapter 还须接入 Scheduler 的每次调用许可、严格执行出口、受管 Worker 的实际停止证明、私有会话存储和独立准入证据。

实测边界与可复现诊断见 [锁定能力](../work-items/WP-13/grok-lock-capability.md) 和 [PROTOCOL-01](../work-items/WP-13/PROTOCOL-01/summary.md)。

后续 [逐 HTTP Gate](grok-call-gate.md) 使用真实 Scheduler 预算验证了标题/重试；headless end.modelCalls 继续只作观测，不作为总调用证明。生产 NativeForwarder/Worker 与真实准入仍未提供。

[实际工具调查](../work-items/WP-13/TOOLS-01/summary.md) 已固定 tool_call/update 和 tool continuation fixtures，但观察器继续拒绝它们。工具失败也能伴随 Native end_turn/exit0，不能据此提升为验收成功；未来工具准入还需响应进入 Native 前的路径授权及独立凭据边界。
