# Codex 受管只读 Adapter

`internal/fusion/runtime/codexadapter` 将固定 Codex 0.160.0 的 [类型化阶段协议](codex-gateway-client.md) 接到真实 Store、Scheduler、Manager、Supervisor 和私有 HTTP 通道。这个组件已完成文字执行与停止释放核验；真实账号、订阅 Forwarder、额度来源及产品 Factory 注册仍待完成。证据见 [ADAPTER-01](../work-items/WP-12/ADAPTER-01/summary.md)。

## 可信配置与入口

`AdapterConfig` 仅由进程内管理层构造，包含 Scheduler、Manager、固定可执行文件、独立 registry 的 `Identity` 读取、`Current` 判定和单次 `NativeForwarder`。它不能由任务 JSON、模型返回值或 Worker 配置构造。Native 的空账号只表示本地阶段 provider，不提供真实上游身份或权益证明。

包单独放在 `codexadapter`，因为父 runtime 的专用通道已经引用 codex 协议包；将 Adapter 放入 codex 会形成 import cycle。沿用已有生命周期和协议组件，不增加启动框架或依赖。

`Start` 只接受已准备的 StageRun、可信 SourceGuard、私有 Root/Workspace、UTF-8 prompt 和 timeout。要求：

- SourceGuard 必须存在、绑定当前复制目录且来源仍有效；缺省不沿用通用 Supervisor 的诊断语义。
- Root 和 Workspace 为独立、规范化的私有目录，互不包含；保留 Root 目录身份并持续重核。
- 五个角色均只读且无工具；Writable 或写 reservation 被拒绝，角色本身不代表写入授权。
- timeout 为正且不超过 4 分钟；prompt 不为空、不含 NUL，原始上限 64 KiB、JSON 字符串编码上限 32 KiB，以适配现有累计 stdio 写入上限。
- 调用方不得指定 executable/hash、argv、环境、session、channel 或 outcome validator。

在任何可信服务 callback 前复制 prompt 和输入 Target；随后通过 `CheckPrepared` 核对权威 Store 中的 run、冻结目标、权限、quota、reservation 和预算。每个模型 HTTP 请求均使用真实 `Scheduler.Permit`，持续重核准入后记录持久调用预算。没有预算重置、退款或替换模型。

## 原生执行与结果

Adapter 验证可执行文件的固定 SHA，创建新的私有进程 session 标记和本阶段 grant，固定启动为 `app-server --listen stdio:// --strict-config`。session 标记用于受管进程归属；真正的 Native thread/turn ID 来自严格核验的 RPC 回复，二者不能混作恢复身份。

独占 HTTP 通道在 driver 前激活 grant。Driver 使用私有 StdioPeer → NewGateway → initialize → account/read → thread/start → turn/start → 通知状态机。每次 RPC、通知与 HTTP 前后检查冻结绑定、独立身份 epoch、Store owner/generation/目标、来源及私有目录。callbacks 接收 Target 副本；callback 内撤销身份也不能凭返回 true 通过后置重核。

只从已经严格验证的 `item/completed` agentMessage 提取 final_answer 或无 phase 的正文，按观察顺序累计，最多 32 KiB。delta、reasoning、错误正文与额度通知不成为最终文字。Output 是不可信模型产物，不代表工程验收结论。

成功必须同时满足类型化 Client 的 Native succeeded、当前 Gate Healthy、来源与绑定仍有效、stdout 中无阶段凭据、Supervisor 实际 wait/reap、受管输出未溢出及其他原有条件。Supervisor 会记录 Native stdio，因此 stdout 并非空；逐行 JSON 凭据检查只负责保密投影，不能替代协议或终态核验。

`Observation` 只在 Store 终态后返回，且必须属于同 Adapter 的准确 run/generation。失败、取消、中断与 uncertain 不暴露成功文字。调用方仍必须执行项目/任务访问鉴权。Adapter 的格式化输出隐藏配置，JSON 序列化被拒绝。

## 取消与释放

沿用 Handle.Cancel、父 context 取消和 timeout：持久 cancel intent、HTTP context 撤销、原生进程停止、wait/reap 和 StopProof。来源或身份漂移导致保护性停止，在本组件实际场景中记为 cancelled；不能把这类保护性取消描述为正常模型完成。

grant 激活失败后若 Supervisor 返回已知 Handle，Adapter 保留它供实际等待和释放，不把启动错误当作已经没有进程。清理在 Handle.Wait 后进行。`Release` 仅接受本 Supervisor 认可的准确 StopProof，再调用 Scheduler.Release；不凭 PID、文本或退出码自行释放 reservation。

Native 429 为 failed，unsafe503 被 Gate 拒绝，其后原生重试没有新增上游发送或预算。准入在实际 HTTP 前变为 unknown quota 时，上游发送和预算均为零。

## 当前验证与剩余边界

实际固定 Native 覆盖 13 个生命周期场景，另有 6 类启动前身份拒绝；每次启动均验证实际停止证明与 reservation 释放。成功 prompt/effort 副本测试证明 callback 修改调用方原始值不会改变已冻结的执行输入。所有身份、quota 和上游响应为合成数据，Store/Scheduler/Manager/OS 生命周期为真实组件。

普通 Resume 明确返回 unsupported；工具、文件写、checkpoint/恢复、真实 ChatGPT 私有登录/续期、官方 subscription Forwarder、真实额度 pool、Controller 产品执行注册、完整五阶段闭环及最终 T01–T60 均未由本项完成。没有 API key fallback、日常登录/Keychain 导入或真实模型请求。Jev off，默认 openai Client 的认证约束保持。
