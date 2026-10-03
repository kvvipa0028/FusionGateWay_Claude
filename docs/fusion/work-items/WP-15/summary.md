# WP-15 任务控制 API 进度

状态：`in_progress`。子工作项 [WP-15-PREVIEW-01](PREVIEW-01/summary.md) 已完成：受鉴权的无调用计划预览、冻结提交和任务读取组件。

当前 Handler 尚未注册到生产 listener/GUI，旧执行入口继续关闭。完整 WP-15 仍需执行预算与输入事务、阶段计划修订、暂停/取消/继续、预设/额度、持久 sequence 的 SSE、断线重连和真实调度接线；OpenAPI 与实际 UI 合同随后按最终接口交付。此组件测试不作为 T13/T25/T28/T33–T36/T43/T45/T49/T50/T56 的完整最终验收。

[WP-15-BUDGET-01](BUDGET-01/summary.md) 已完成预览预算、提交预算事务和预算读取；任务、快照、幂等与预算共同提交，重试不退还已消耗的次数。162 个 Fusion tagged race tests 通过。剩余阶段修订、控制、事件流与接线继续实施。

[WP-15-EVENTS-01](EVENTS-01/summary.md) 已完成持久序列 SSE、重连补读、当前管理 Context 验证与撤销、连接/页/写入界限。实际 loopback HTTP 验证通过，完整 Fusion tagged race 173 个通过。剩余 If-Match、阶段控制、预设/额度与生产接线仍未完成。

[WP-15-REVISION-01](REVISION-01/summary.md) 已完成尚未开始角色的计划预览、If-Match 修订及读取组件；冻结已开始/已结束绑定，原子重查竞态，保留活动凭据、历史与预算。189 个 Fusion tagged race tests、CLI/GUI 编译和全仓 vet 通过。暂停/取消/继续、预设/额度、OpenAPI 与生产/UI/调度接线仍未完成。
