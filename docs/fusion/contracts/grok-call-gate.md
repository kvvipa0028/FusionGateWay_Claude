# Grok Native 每次 HTTP 调用的 Gate 合同

WP-13-CALLS-01 在 `internal/fusion/runtime/grok/call_gate.go` 中提供 `CallGate`，连接既有 `policy.Manager` 与每次调用的 `Scheduler.Permit`。它还未注册生产 Worker、私有 Native transport 或真实订阅路线。只有可信 controller 可以提供 `GateBinding`、`Claims`、`Current`、`Permit` 和 `NativeForwarder`；HTTP body 不能提供路由权限。

`GateBinding` 冻结 exact route/revision、requested/resolved model、account/workspace、credential identity、subscription billing path、runtime version、effort，以及 headless Binding 中的 executable hash、run/generation、role、session UUID、cwd、readonly tools。Target 必须为 `ControlledCalls`，且没有 PluginVersion；账号字符串、锁定模式和版本声明自身不构成准入证据。Native 文件、账户、effort 能力、权限与计费仍由可信接入方独立核验。

每次请求执行以下顺序，包括标题和 SDK 重试：

1. `Manager.Stage` 核验 ModelAudience grant 和当前 task/run/role/attempt/plan/generation/project。未经认证的请求不能使合法运行失效。
2. 只接受 POST `/v1/chat/completions`、无 query/alternate URL/encoding、JSON body。body 上限 1 MiB，JSON 拒绝重复字段、歧义、NUL、过深、尾随数据；model/effort 必须与快照一致。messages 只允许 system/user/assistant 的字符串 content；stream 必须 true。
3. bounded queue 最多四个请求，调用持有 `Manager.BeginModelCall` 的同一 run 锁。竞争在一分钟 context 内等待，等待前后重查权限和 `Current`。
4. `Permit(ctx,claims,target,1)` 必须执行既有 Scheduler 的路线、权限、额度、物理池 reservation/proof 和共享持久化调用预算检查。每次 HTTP 独立消耗一次许可。成功扣账后不退还，也不将 Native `end.modelCalls` 当成实际请求次数。
5. 再查 current，可信 `NativeForwarder.Send` 仅接收冻结 Target 和复制的 body。它须单次发送、遵守 context、校验 endpoint/credential identity/billing，禁止内部 retry/redirect；不接收 Native Authorization、Cookie 或路由 headers。本组件没有自动普通 xAI API endpoint/key、网页 Cookie 或 OAuth 替代路径。
6. 原始错误响应不发给 Native。429 返回固定 JSON，允许 Native 根据已验证的冻结 SDK 配置重新发起请求；重试须再次完成上面全部许可。其他上游失败、身份改变或解析失败使 Gate permanently uncertain，本次运行不能借助标题 fallback 被提升为成功。
7. 200 响应只接受 text/event-stream，无压缩；完整 body 上限 8 MiB，先缓冲并校验后输出。每个 chunk 都须为冻结模型/稳定 id、单个 index0 choice、assistant/text delta，最终 stop 和 `[DONE]` 完整；拒绝工具、backend 指令、未知 choice/delta/root 字段、其他结束原因和不完整流。usage 若存在必须合法且总量一致。先完整验证能阻止末尾模型不一致或未知扩展泄漏前面的回复。

请求的 function tools 只接受冻结 readonly tool 名和辅助 `session_title`；通用工具发现、任意后端及 subagent 名拒绝。当前仅接受文本响应，实际 tool call/update、tool execution、tool messages 和 Resume 尚未支持。function 声明通过不代表实际工具执行已验证。

`ForwardResponse.PrivateMarkers` 应由私有 transport 提供 credential markers，供响应反射检查；stage grant 也单独检查。marker、原始 body、transport error、上游 headers 不写入 audit。`CallAudit` 仅记录 Attempts/Permits/Forwarded/Completed/Retryable/Uncertain。拒绝、写失败或身份失效后 uncertain 不可重置；下一次任务应创建新的 Gate。

`Healthy(ctx)` 需要真实认证的 model context，而非 background context；它只说明当前 Gate 没有不确定状态、没有处理中请求，至少完成一次响应且计数相符。它不是完整 Worker/all-calls/strict-lock/停止证明。调用方还必须验证 Native headless 终态、准确会话、产物和受管 Worker 停止。许可成功但 Send 前权限变化时预算已消耗，Forwarded 仍可为零；不能以二者总相等作为普遍规则。

2026-10-04 的实际固定 Native `1.0.48/b94d5072c95f` 在私有 HOME/XDG/GROK_HOME、空项目、loopback 假上游中验证了标题/主请求、预算耗尽、默认标题模型、隐式工具和 429 retry。测试使用真实 Store/Manager/Scheduler，quota/准入 inspection 是合成 oracle，不能变成真实路线准入。正常 title+main 为两次预算，SDK retry 为三次，而 CLI end 仍为一次；预算第二次不足只转发第一次。诊断 profile 禁 fork/securityd、仅许 loopback，但较宽 Mach lookup，不是生产 Worker isolation 证明。

原始 RED/GREEN、实际 Native 五场景及重现方法见 [CALLS-01](../work-items/WP-13/CALLS-01/summary.md)。真实 X 登录、订阅/Heavy、额度池和计费、生产 forwarder、受管启动/取消、准确恢复与最终 Gate 尚待完成，WP-13 继续 in_progress，Jev off。
