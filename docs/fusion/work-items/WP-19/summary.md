# WP-19 · 精简工作流与设计检查点

状态 **in_progress**。BASE feb78b8；[DESIGN-GATE-01](DESIGN-GATE-01/summary.md)完成有限种类/角色序列、不可变设计与验收标准、当前计划的人类批准记录及启动出口双重约束，见[合同](../../contracts/workflow-design-gate.md)。

investigate 仅 design，review 仅 review，change/bugfix 固定五角色；未冻结完整设计或未批准当前计划时不能实施，批准也不替代来源、Runtime、预算与权限核验。schema9 迁移保留旧手动任务，失败回滚与 checksum/future10 拒绝已验证。

[WORKFLOW-API-01](WORKFLOW-API-01/summary.md)完成 Management 读取/附加、设计提交/批准、Store 事务内权限复核及固定 Native 通路；HTTP/桥和 OpenAPI 分别验证，不等于实际窗口或真实供应商准入。

仍未完成：原 Magpie 界面工作流消费；真实三路线 Gate A；实际写范围执行、阶段产物/Handoff、测试审查证据与有限返工。版本 1 核心没有自动派单或失败循环，不修改 CSS、原主资源或导航；后续 UI 仅在原页面小范围扩展。

本组件不代表 WP-19 或 M3 完成。整体目标 active，WP-12–WP-16 继续 in_progress；最终 T01–T60 not_run，真实模型/额度调用零，Jev off。
