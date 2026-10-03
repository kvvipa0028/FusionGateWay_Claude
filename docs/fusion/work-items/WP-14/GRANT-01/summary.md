# WP-14-GRANT-01：启动后激活 model capability

本子工作项 `done`，父 WP-14 保持 `in_progress`。新增 PendingModelGrant 原语，为 Native 启动预先分配随机值；它在激活前没有任何调用授权。不修改 StoreValidator，不提前签发可用 model capability，也不把 worker_events 用途改成 model。

可信控制器可调用 PrepareModel 取得 `fgs_` 形式的 256-bit 随机值及不可变 Claims。Manager 的 prepared map 只保存 SHA256、Claims、一分钟准备期限和激活后的 TTL；它与 active grant 共用最多 4096 个容量。这个 map 不被 AuthenticateStage、WithStage 或 ModelCurrent 当作授权。

Activate 必须在真实 ConfirmStarted 后调用，使用原有 validator 核对当前持久化 run/role/attempt/revision/generation/project/lease。成功时只将匹配、未过期的准备项一次性转入 active grant；重放不会续期。准备超时、错误当前身份、run/management 撤销、Cancel 或另一个 issuer 均不获得授权。激活后的 TTL 最大五分钟，从激活开始计时；没有修改阶段执行时限或授权恢复合同。

PendingModelGrant 的 String/GoString 与 JSON 不输出随机值，Secret 仅用于可信 Native 启动通道。Cancel 清除 prepared/active 中这一个 capability；run 撤销清理其全部准备项，management 撤销清理整个 issuer。直接 Issue 也计算 prepared 的容量，不能绕过上限。

新增模块/API 缺失先 RED，3 个新 top-level tests 后 GREEN，覆盖无权限预分配、激活前拒绝、确认后激活、重放、撤销、过期、issuer 隔离、错误 audience/scope/TTL、4096 容量与格式化脱敏。既有真实 Store scope 测试增加准备流程：starting run 不能 Activate，ConfirmStarted 后成功，Finish 后 ModelCurrent 拒绝；原来的直接 Issue 行为同时保留验证。

完整 Fusion tagged race：202 个 top-level PASS、3 个 SKIP（两个父进程 helper、一个显式 Native 诊断入口）。Go 1.26.3 CLI/GUI 编译和全仓 Fusion/nogui vet exit 0，真实模型调用数为 0，未读取 key 到 Worker 或改写用户配置。

这里只完成原语，尚未将它接到 Supervisor 与 GLM Adapter。后续受管启动需要固定 executable/profile、唯一 loopback 服务、Native 环境中的未激活随机值，以及 ConfirmStarted 后的 Activate；任何失败必须撤销并执行真实进程 wait/reap。不能因为有了准备对象就允许任务 HTTP 自选 Claims、端口、环境或 Transport，也不能据此声明真实计费/额度、全部调用 OS 边界或 Native StopProof 已通过。
