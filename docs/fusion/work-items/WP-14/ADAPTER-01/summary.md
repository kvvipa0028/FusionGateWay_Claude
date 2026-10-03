# WP-14-ADAPTER-01：GLM Native Adapter

子工作项 `done`，父 WP-14 保持 `in_progress`。新增 glm.Adapter 实现公共 Runtime Probe/Start/Resume，组装已有 Supervisor、ClaudeChannel、PendingModelGrant、CallGate、Session 与 Scheduler。没有注册产品入口或将假路线升级为真实准入。

可信 AdapterConfig 必须提供 Scheduler/Inspector、Manager、固定 executable、LoadCredential、Current 与已准入 single-send Transport。构造时复制配置，内部 Supervisor 始终使用同一 Store；不接受 HTTP 提供这些服务或其 true/false 证据。固定 CLI 2.1.287 SHA256 为 6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea；Probe/Start 会核对实际文件，不能沿 symlink 自动升级版本。

Start 拒绝 caller 提供 executable/hash/argv/env/Native session/validator/channel，也拒绝空或非法 UTF-8/超长 prompt、超过四分钟的时限和只读角色的写请求。实际 writable 必须匹配已冻结物理 reservation。只读使用 Read，可写 implementation/testing 使用 Read/Edit；UUID、模型/effort、bare/restricted/dontAsk 等 argv 与 outcome validator 均由 Adapter 生成。没有 Write、Bash、MCP、bypassPermissions 或付费 fallback。

新增 Scheduler.CheckPrepared 无预算消费地重核 starting 状态、run owner/target/完整 scope、当前路线/数据权限/额度/验证、物理 pool/write/proof reservation 和剩余预算。Adapter 在加载真实 key 前调用，加载后再次调用，避免准备成功后权限/额度漂移。loader 的 Credential.Identity 必须匹配冻结值；Credential JSON/String/GoString 不输出 key。真实 key 只进入控制器 CallGate，Worker 接收 scoped entropy，确认启动后激活；所有 HTTP 的 Permit 固定使用 Scheduler.Permit。

Current 同时核对仍 running 的持久 Store scope/owner/target 与可信当前身份，完整 Native 协议仅在 Supervisor 的实际 EOF/exit 0 后验证。Observation 只能在同 generation 终态读取；非成功不会返回成功文本/状态，各独立验证标志保持 false。Observation 是不可信 Native 产物，不能冒充 EvidenceGate 验收；4096 条记录上限、Resume unsupported、真实 StopProof/Release 继续保留。已启动的激活失败保留 Handle 和真实回收流程，不能丢掉进程。

## 验证

缺失公共 API 先 RED。CheckPrepared 单测覆盖合法准备、quota/proof 漂移、错误 owner/target 和已 running 重放，均不新增 model call；早期预算测试错误地试图在 starting 消费调用，被真实 Store fencing 拒绝，修正为合法 running 后消费的重放反例，未改变 Store 合同。

两组 Adapter 单测覆盖缺少可信依赖与 13 种禁止的 launch 控制，证明未加载 secret、未生成 launch journal、未消费预算。真实固定 Native 的七个场景：

| 场景 | 假上游 / 持久调用 | 判据 |
| --- | --- | --- |
| design | 1 / 1 | 生成文本正确、协议与实际退出/StopProof |
| implementation | 2 / 2 | Read/Edit 集合、实际创建文件与回读、StopProof |
| sdk_retry | 2 / 2 | SDK 429 retry 再次 Scheduler.Permit |
| wrong_credential | 0 / 0 | key Identity 错误，未写启动日志 |
| late_quota | 0 / 0 | loader 期间额度变 unknown，第二次检查拒绝启动 |
| current_identity | 0 / 0 | 当前身份无效，未加载 key、未写启动日志 |
| cancel_inflight | 1 / 1 | 运行中 observation 拒绝，取消后无成功文本，真实 wait/reap/StopProof |

前三个成功与取消场景均核对 generation、独立验证 flags=false、实际预算、停止证明及 Release。四个执行场景全程 synthetic key/upstream/account/quota，真实模型调用数 0。七个场景为 1 个 top-level/7 个 subcase PASS，完整 Fusion tagged race 为 212 个 top-level PASS、6 个 SKIP（两个父进程 helper、四个显式 Native 入口），Go 1.26.3 CLI/GUI 编译和全仓 Fusion/nogui vet exit 0。

仍需真实 Credential service/Transport/quota Reader、Registry 完整证据、产品控制器/API/UI 接线、恢复/工程执行和 Gate A。本项不将 API 可查询、Native 标签、连接诊断或这些 fake Inspector 作为真实 Coding Plan 计费/身份/effort 证明，详见 [集成核验](../glm-integration-verification.md)。
