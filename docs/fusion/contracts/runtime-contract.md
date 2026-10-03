# Runtime 与 Worker 合同 v1

`Adapter` 提供 Probe/Start/Resume，Handle 提供 Events/Cancel/Wait。能力明确区分 start/events/cancel/resume/child_processes/network。首版只支持本机 macOS 单进程、无网络；resume 与其他平台返回 unsupported，需要工具子进程或外部网络的路线拒绝准入。不存在隐含的非沙箱 fallback。

执行顺序：可信 workspace/route/quota/预算检查 → Scheduler.Prepare 原子保存 intent/reservation → Supervisor 校验 starting/current generation/held → fsync launch intent → sandbox-exec → 记录 PID/出生时间/nonce/可执行文件与 profile hash → ConfirmStarted → heartbeat → native validator + wait/reap → 写终态/stop journal → 可信 StopProof → Scheduler.Release。

Spec 的路径、argv、stdin 与 validator 只由注册的可信 Adapter 构造。argv 是数组，stdin 有界 64KiB；没有 shell 拼接、ambient env 或 ExtraFiles。环境仅固定 HOME/XDG/TMP/PATH，额外 fixture 字段有白名单，不接受管理/API/OAuth secret env。真实 Adapter 的授权通道必须单独验证。本包不读取任何真实凭据。

macOS profile 可只读系统 ICU 与时区数据目录，供 Native 初始化使用；不开放父目录或这些数据目录的写/执行权限。该启动依赖修复不授予网络、fork 或 Native 凭据环境，GLM 的受管通信仍需独立 Adapter 与执行出口准入。

stdout/stderr 各最大 64KiB，超限取消且不判成功；总时限最大 10 分钟。心跳每 2 秒按 generation 续租。TERM 后 200ms KILL，只针对出生身份匹配的进程；Wait/reap 前不会提供退出证明。派生被 kernel 拒绝，不用 PID 组推断逃逸子进程已停止。

需要源目录批准和稳定源数据，才调用 Copy；复制到仓库外私有目录，源不改写。排除 .git/.claude/.codex/.grok/.fusion-dev/.env；拒绝 symlink/hardlink/特殊文件，使用 os.Root + O_NOFOLLOW 限定读边界。上限 10000 文件、单文件 20MiB/总量 100MiB。只读角色由沙箱拒绝 workspace 写，HOME/缓存仍可写。写 lease 由持久化 reservations 控制。

新控制器不自动接管旧 worker，启动 gap 或失败后 writes 保留为 execution_uncertain/interrupted/needs_review；未对账不能重放或释放锁。Supervisor 退出证据只在本控制器内核验，launch.json/hash 本身不是可恢复授权。

生命周期 channel 是临时进度，不作审计回放；Store.Events 保持 task 内连续序号和 generation。正式 Native parser 必须解析成功/错误/中断及实际 model/session 等字段，不能使用本包 fixture marker 作为上线判据。

控制器每次最多保留 4096 个 launch/proof 防止无限内存增长；到限拒绝新启动，需停派单并对账后重启。
