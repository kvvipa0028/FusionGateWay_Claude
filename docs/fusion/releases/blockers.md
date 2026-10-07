# 阻断项清单 — Gate A

| # | 阻断项 | 影响工作包 | 类别 | 解除条件 | 状态 |
| --- | --- | --- | --- | --- | --- |
| B1 | 本人官方设备授权登录未完成 | WP-17, WP-18, WP-23, WP-26 | 用户操作 | 官方登录界面已实际调出（设备授权链路验证通过），浏览器授权待用户择时完成 | 等待用户授权 |
| B2 | ~~Codex implementation turn 终态报 interrupted~~ 已修复：委托沙箱 + gateway 事件过滤（commandExecution/null 容错）后 8 场景全过 | WP-12 写入闭环 | 代码 bug | 已通过全场景验证 | 已解除 |
| B3 | Grok effort 档位不支持 | WP-13 effort 支持 | 能力限制 | 需要真实模型 metadata（native 给未知模型剥离 reasoning_effort） | 等真实准入 |
| B4 | 产品 CLI 草稿不执行任务 | WP-15 CLI | 功能缺口 | CLI 集成待做 | 待做 |
| B5 | 实际 Native UI 点击验证 | WP-16 部分 UI 组件 | 用户操作 | 桌面解锁后执行桌面验证 | 等待用户 |
| B6 | 真实工程 Smoke 未执行 | WP-17, WP-18, WP-23 | 用户操作 + 代码 | B1 解除后执行三路真实 Smoke | 等待 B1 |
| B7 | 原版 Magpie 聊天/Sessions/Gateway 与 Fusion 未互通 | 产品集成 | 功能缺口 | Sessions 导航已接入当前项目 Fusion 任务记录（只读）；剩余：Provider 注册/登录集成（依赖 B1）与原 Gateway 页面 | 部分完成 |
| B8 | 自动工作流/Jev/插件更新 off | WP-27–WP-29 | 设计决策 | Gate B 后按需开启 | Gate B 前保持 off |
