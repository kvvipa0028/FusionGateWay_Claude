# 执行准入与调度阻断 v1

Scheduler 仅接受受信任的路线、权限、sandbox、验证执行器与额度 Reader 的 Inspection。客户端不能提交这些判定。Prepare 先核对 exact 冻结目标、模型、账号、effort、费用、准入、数据权限、验证能力和受控内部调用，再原子提交 startup intent 与 reservation；未满足条件不派单，不降级模型/证据要求或启用付费 API。

| 阻断码 | 含义 |
|---|---|
| admission_unavailable / admission_proof_missing | 服务或当前证据未准备 |
| task_unavailable / task_not_ready / plan_unavailable | 任务/版本/状态不允许启动 |
| target_not_approved / route_changed | 目标不在快照中或路线身份/版本已变化 |
| data_permission_denied / write_permission_denied | 数据或写入权限未获准 |
| sandbox_unverified / verification_unavailable | 隔离或实际验证要求无法执行 |
| internal_execution_uncontrolled | 摘要/子 Agent/重试不能计数或尚未限制为单个受管执行 |
| quota_query_not_admitted / quota_identity_mismatch | 查询权或账号/workspace/region/generation 不符合当前路线 |
| quota_unknown / quota_stale / quota_unverified / quota_auth_required / quota_unsupported / quota_zero | 额度分别缺失、过旧、池未核验、需认证、不支持、耗尽 |
| budget_missing / budget_exhausted | 共享任务预算未配置或耗尽 |
| capacity_unavailable / capacity_busy / reservation_failed | 容量读取失败、共享池/全局/写 lease 占用或原子预留失败 |
| stage_identity_invalid / execution_fenced / reservation_changed | 调用身份、当前 generation 或预留合同失效 |
| stop_unverified / execution_not_reconciled | 无可信进程树停止证明或执行状态未对账 |
| cancelled | 调用者已取消 |

默认全局上限为 2 个受管执行，每个 verified 物理 pool 只能有 1 个 held reservation；同一写入 lease key 只允许 1 个 writer。同一任务即使收到 terminal 协议事件，也须先释放已确认停止的旧 reservation，才能开始新阶段。全局容量可在无 held/unknown 执行时由受鉴权控制端配置为 1–16，不能让请求或 Worker 自带不同上限。首版关闭不能计数的内部并行/子 Agent。

task_budgets 持久化全部阶段共享的 model call 与 rework 计数，不能因 HTTP 重试、阶段变化、取消、服务重启或新 attempt 重置。配置一次后仅允许同值幂等提交；model call 最多 1000、rework 最多 1。产品默认值和用户修改由 WP-15/WP-16 的管理合同提供，当前不替用户创建真实任务预算。Permit 在发送前原子扣减；已授予的调用即使上游失败也不退款。

Dispatcher 在同一 issuer/run 中只允许一个模型调用在途，防止同阶段凭据并行请求绕过 pool 限制。实际 Native Adapter 必须证明所有模型调用都经这个出口计数，或关闭该路线；一个受管宿主进程不自动证明内部只有一个 Agent。

SQLite schema 2 将预算、global policy 和 pool/write reservations 与任务同库保存，迁移保留 schema 1/checksum，并核验新增迁移 hash。并发争抢/事件写入失败都在同一事务中处理。重启/lease expiry 产生 unknown，预留和已用预算继续保留，不释放成“可重试”。该本地预留不等于供应商余额，也不能阻止用户其他应用消耗同池额度。

Release 需受信任 Supervisor 核验 native session、generation、process identity、全部 descendants 已停止与 report hash，再检查持久化 terminal 状态；来自用户请求的 stopped=true 不构成证明。unknown 必须先由后续恢复合同对账。WP-11 提供实际 OS/进程证明，当前测试使用合成 StopProof，未开放真实执行。
