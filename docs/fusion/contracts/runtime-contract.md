# Runtime 与 Worker 合同 v1

`Adapter` 提供 Probe/Start/Resume，Handle 提供 Events/Cancel/Wait。能力明确区分 start/events/cancel/resume/child_processes/network。默认只支持本机 macOS 单进程、无网络；WP-14-CHANNEL-01 增加可信 ClaudeChannel 的唯一双栈 loopback 端口，泛用 Probe 仍不报告任意 network/child_processes。resume 与其他平台返回 unsupported，需要工具子进程或外部网络的路线拒绝准入。不存在隐含的非沙箱 fallback。

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
