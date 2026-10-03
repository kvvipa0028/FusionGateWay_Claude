# Runtime 与 Worker 合同 v1

WP-13-PROTOCOL-01 新增 [固定 Grok headless 观察器](grok-headless-protocol.md)；它尚未提供公共 Adapter 的 Start/Probe/Resume 或注册 Worker。[CALLS-01](grok-call-gate.md) 提供逐 HTTP Gate 的合成验证，生产 NativeForwarder/Worker 尚未注册。原生终态的 modelCalls 不包含已观察到的辅助请求，不能作为完整调用锁定、额度/计费或停止证明。

`Adapter` 提供 Probe/Start/Resume，Handle 提供 Events/Cancel/Wait。能力明确区分 start/events/cancel/resume/child_processes/network。默认只支持本机 macOS 单进程、无网络；WP-14-CHANNEL-01 增加可信 ClaudeChannel 的唯一双栈 loopback 端口，泛用 Probe 仍不报告任意 network/child_processes。resume 与其他平台返回 unsupported，需要工具子进程或外部网络的路线拒绝准入。不存在隐含的非沙箱 fallback。

WP-15 的 [可信执行控制器](execution-controller.md) 现可通过 PrepareOnce 编排 Adapter、owned lifetime、取消、实际 wait 和可信 release。已有启动请求只读；HTTP 断线不取消。已完成原生合成上游全链路验证，产品管理端与实际账户准入仍未注册。

执行顺序：可信 workspace/route/quota/预算检查 → Scheduler.Prepare 原子保存 intent/reservation → Supervisor 校验 starting/current generation/held → fsync launch intent → sandbox-exec → 记录 PID/出生时间/nonce/可执行文件与 profile hash → ConfirmStarted → heartbeat → native validator + wait/reap → 写终态/stop journal → 可信 StopProof → Scheduler.Release。

Spec 的路径、argv、stdin 与 validator 只由可信 Adapter 构造。argv 是数组，stdin 有界 64KiB；没有 shell 拼接、ambient env 或 ExtraFiles。默认环境仅固定 HOME/XDG/TMP/PATH，额外 fixture 字段有白名单。ClaudeChannel 只增加固定私有 Native 路径、冻结模型、controller endpoint 与尚未激活的 stage 随机值；不接受管理、真实 API key 或 OAuth secret env，也不能与 fixture env 混用。本包不读取任何真实凭据。

macOS profile 可只读系统 ICU 与时区数据目录，供 Native 初始化使用；不开放父目录或这些数据目录的写/执行权限。该启动依赖修复不授予网络、fork 或 Native 凭据环境，GLM 的受管通信仍需独立 Adapter 与执行出口准入。

stdout/stderr 各最大 64KiB，超限取消且不判成功；总时限最大 10 分钟。心跳每 2 秒按 generation 续租。TERM 后 200ms KILL，只针对出生身份匹配的进程；Wait/reap 前不会提供退出证明。派生被 kernel 拒绝，不用 PID 组推断逃逸子进程已停止。

需要源目录批准和稳定源数据，才调用 Copy；复制到仓库外私有目录，源不改写。排除 .git/.claude/.codex/.grok/.fusion-dev/.env；拒绝 symlink/hardlink/特殊文件，使用 os.Root + O_NOFOLLOW 限定读边界。上限 10000 文件、单文件 20MiB/总量 100MiB。只读角色由沙箱拒绝 workspace 写，HOME/缓存仍可写。写 lease 由持久化 reservations 控制。

新控制器不自动接管旧 worker，启动 gap 或失败后 writes 保留为 execution_uncertain/interrupted/needs_review；未对账不能重放或释放锁。Supervisor 退出证据只在本控制器内核验，launch.json/hash 本身不是可恢复授权。

生命周期 channel 是临时进度，不作审计回放；Store.Events 保持 task 内连续序号和 generation。正式 Native parser 必须解析成功/错误/中断及实际 model/session 等字段，不能使用本包 fixture marker 作为上线判据。

控制器每次最多保留 4096 个 launch/proof 防止无限内存增长；到限拒绝新启动，需停派单并对账后重启。

WP-14 的 CallGate 是独立可信 Handler。ClaudeChannel 构造时必须同时绑定 127.0.0.1 与 ::1 的同一随机端口，失败最多重试八次，不能退回单栈。端口由构造器产生，不接受 HTTP DTO 自选 URL、listener、环境或 Claims。两边使用同一个 authenticated Handler，最多 16 个连接、16KiB headers，header/read/write/idle 时限分别 3/10/70/5 秒，服务端日志不回显请求。

Supervisor 从持久 Store 重核冻结 target、role/attempt/revision/generation/project，独占 acquire 通道后才写启动 intent。prepared TTL 须覆盖执行时限加 20 秒，通道执行最长四分钟。ConfirmStarted 后激活一次；激活失败返回已知 Handle 加错误，撤销身份并 TERM/KILL/wait，不丢掉已启动进程。取消、心跳失败、超时、listener 异常与终态均撤销 grant；存活 Worker 的 Close 被拒绝，只有实际 wait/reap 后释放两个端口。原启动失败路径完成 wait 或确认未启动后才能释放端口，不能用 token 撤销代替进程退出。

固定 Claude 2.1.287 在真实 Supervisor/profile 内，通过 CallGate 与真实 Store 完成无工具合成成功、429 重试、预算耗尽和调用中取消，已产生当前控制器 StopProof。Native api_retry 仅是有界信息，实际每个 HTTP 仍各自 Permit；原手工 profile 的历史诊断不升级为本次证据。产品 Adapter/任务 API 尚未注册，实际 Transport、账号、地区、额度、计费与 GLM 执行 effort 仍需独立准入，详见 [本项证据](../work-items/WP-14/CHANNEL-01/summary.md)。

WP-14-TOOLS-01 在同一生产边界证明固定 Native 的 bare/restricted 模式可使用 Read 与 Edit，Edit 的空 old_string 可以创建项目新文件。Write 不在该 pin/mode 的实际 init tools 中，不能因协议 schema 允许 Write 就将它准入；Bash/其他需子进程的工具仍未验证。工具许可只来自冻结可信配置的 tools/allowedTools 与 OS 工作区边界，不使用 bypassPermissions 或扩大目录权限。

Native 可以在 tool_result.is_error=true 后返回 result/success。协议观察器必须拒绝 true、null 或非 boolean is_error；只有省略或明确 false 才可完成该工具。真实只读项目写入拒绝的 fixture 先 RED 后 GREEN，不会被最终成功文本掩盖；越界读/创建、未支持的工具同样不能成功，写角色失败保留 interrupted 状态。细节见 [工具证据](../work-items/WP-14/TOOLS-01/summary.md)。

WP-14-ADAPTER-01 的 glm.Adapter 实现公共 Runtime 接口。可信配置必须提供 Scheduler/Inspector、Manager、固定 executable、当前身份检查、私有 Credential loader 和已准入 single-send Transport。Adapter 自建绑定同一 Store 的 Supervisor，不接受 caller 的 executable/hash/argv/env/session/validator/channel；请求只含私有 Root/Workspace、UTF-8 prompt、时限和受许可的 writable 范围。只读角色使用 Read，可写 implementation/testing 使用 Read/Edit；不存在 Write、Bash 或跳过权限的退路。

启动前 CheckPrepared 无调用计费地重查 starting/run owner/冻结 target、当前路线/权限/额度/验证证据与物理 reservation；加载凭据后再次检查。核对固定 CLI SHA256、生成 Native UUID、构造固定 argv、启动后的 model grant、CallGate 与可信协议 validator。凭据 Identity 必须等于冻结 credential_identity，真实 key 不进入 Spec/env/observation。所有 HTTP 使用 Scheduler.Permit，不能用任意回调替代持久预算。

Adapter 的 Observation 仅在同 generation 的终态后读取，包含已解析 Native 输出与文本；未完成拒绝，非成功终态不返回成功文本或状态，所有独立验证 flags 保持 false。它不代表工程验收或恢复授权。每个 Adapter/Supervisor 最多保留 4096 条执行记录，Resume 仍 unsupported；Release 使用本 Adapter 的可信 Supervisor StopProof。产品控制器注册与真实路线证据仍未完成，详见 [Adapter 证据](../work-items/WP-14/ADAPTER-01/summary.md)。

[READ-01](grok-read-tools.md) 提供绑定到同一任务Cwd的可选文本Read授权与Native trace/后续HTTP关联；公共Adapter/Worker注册、真正OS隔离、恢复与停止证明仍待接入。

[WP-13-CHANNEL-01](../work-items/WP-13/CHANNEL-01/summary.md) 将可信GrokChannel接入同一私有lease/Supervisor。Spec拒绝双typed channel/零值wrapper；GLM拒绝caller注入GrokChannel。冻结1.0.48实际hash、subscription target与独占双栈/v1 endpoint，config0600/privateGROK_HOME只含stage grant、标题同模型/禁用turn summary与auto update，保持Mach/fork/其它网络拒绝。实际Native合成读取/重试/预算/拒绝/取消/reap/StopProof/release已验证；完整Grok Adapter、真实Forwarder/准入/Resume/写权限与源目录稳定性仍待完成，详见[专门合同](grok-managed-channel.md)。
