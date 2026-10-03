# WP-15 任务控制 API 进度

状态：`in_progress`。子工作项 [WP-15-PREVIEW-01](PREVIEW-01/summary.md) 已完成：受鉴权的无调用计划预览、冻结提交和任务读取组件。

当前 Handler 尚未注册到生产 listener/GUI，旧执行入口继续关闭。完整 WP-15 仍需执行预算与输入事务、阶段计划修订、暂停/取消/继续、预设/额度、持久 sequence 的 SSE、断线重连和真实调度接线；OpenAPI 与实际 UI 合同随后按最终接口交付。此组件测试不作为 T13/T25/T28/T33–T36/T43/T45/T49/T50/T56 的完整最终验收。

[WP-15-BUDGET-01](BUDGET-01/summary.md) 已完成预览预算、提交预算事务和预算读取；任务、快照、幂等与预算共同提交，重试不退还已消耗的次数。162 个 Fusion tagged race tests 通过。剩余阶段修订、控制、事件流与接线继续实施。

[WP-15-EVENTS-01](EVENTS-01/summary.md) 已完成持久序列 SSE、重连补读、当前管理 Context 验证与撤销、连接/页/写入界限。实际 loopback HTTP 验证通过，完整 Fusion tagged race 173 个通过。剩余 If-Match、阶段控制、预设/额度与生产接线仍未完成。

[WP-15-REVISION-01](REVISION-01/summary.md) 已完成尚未开始角色的计划预览、If-Match 修订及读取组件；冻结已开始/已结束绑定，原子重查竞态，保留活动凭据、历史与预算。189 个 Fusion tagged race tests、CLI/GUI 编译和全仓 vet 通过。暂停/取消/继续、预设/额度、OpenAPI 与生产/UI/调度接线仍未完成。

[WP-15-START-ONCE-01](START-ONCE-01/summary.md) 已完成启动 intent 的持久幂等映射、task generation CAS 和新建/重读区分，与容量预留同事务提交；重启 unknown、终态重试、故障回滚及 schema 2→3 数据保留通过验证。此项是启动控制的存储基础，尚未注册产品 endpoint 或连接实际 Runtime；父工作包继续 in_progress。

[WP-15-PREPARE-ONCE-01](PREPARE-ONCE-01/summary.md) 将上述事务接入 Scheduler；已提交请求在准入检查之前只读 receipt，并发准入失败时也能只读另一个已提交的相同请求，新 key 仍需实时准入和 generation CAS。237 个 Fusion race tests、原生 Claude Code 合成上游 8 个场景及编译/vet 通过。真实控制器/产品 endpoint、暂停/取消/恢复、预设/额度、OpenAPI 与 UI 接线继续实施。

[WP-15-CONTROLLER-01](CONTROLLER-01/summary.md) 已实现可信服务端控制器的启动、幂等重读、owned lifetime、取消/Close、未知执行保留及可信退出释放。固定 Claude Code 经此控制器完成合成上游一次执行/Permit、HTTP 断线与重试不重复执行、真实 Supervisor StopProof 释放。248 个 Fusion race tests 通过，8 个条件跳过；编译/vet 通过。产品 HTTP 鉴权与接口注册、实际项目/账户配置、暂停/恢复/返工、预设/额度、OpenAPI 和 UI 仍未完成。

[WP-15-EXECUTION-API-01](EXECUTION-API-01/summary.md) 已将内部 Handler 的受管理鉴权启动/取消/运行记录读取接到可信 Controller；Task strong ETag 跟踪 revision/generation/state，重读原启动映射不重复执行，慢预检查期间管理身份撤销会阻止新 intent。258 个 Fusion race tests、CLI/GUI 和 vet 通过，新增执行接口 OpenAPI draft。完整 OpenAPI、产品 listener/GUI 注册、实际配置、暂停/恢复/返工、预设/额度仍未完成，父包继续 in_progress。

[WP-15-CANCEL-REVISION-01](CANCEL-REVISION-01/summary.md) 将取消的 Task revision 条件放入 cancel_intent 写入事务，关闭未来角色修订保持活动 generation 时的预检查竞态。旧版本不会取消 Worker，当前版本可取消历史计划的活动 run；终态重读仍检查当前版本。父包继续 in_progress。
