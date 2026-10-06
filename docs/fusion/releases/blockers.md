# 阻断项清单 — Gate A

| # | 阻断项 | 影响工作包 | 类别 | 解除条件 | 状态 |
| --- | --- | --- | --- | --- | --- |
| B1 | 本人官方设备授权登录未完成 | WP-17, WP-18, WP-23, WP-26 | 用户操作 | 运行 `login-codex.command` / `login-grok.command` 完成官方设备授权 | 等待用户 |
| B2 | Codex implementation turn 终态报 interrupted | WP-12 写入闭环 | 代码 bug | 调查 native turn status 或 driver observation | 待修 |
| B3 | Grok effort 档位不支持 | WP-13 effort 支持 | 能力限制 | 需要真实模型 metadata（native 给未知模型剥离 reasoning_effort） | 等真实准入 |
| B4 | 产品 CLI 草稿不执行任务 | WP-15 CLI | 功能缺口 | CLI 集成待做 | 待做 |
| B5 | 实际 Native UI 点击验证 | WP-16 部分 UI 组件 | 用户操作 | 桌面解锁后执行桌面验证 | 等待用户 |
| B6 | 真实工程 Smoke 未执行 | WP-17, WP-18, WP-23 | 用户操作 + 代码 | B1 解除后执行三路真实 Smoke | 等待 B1 |
| B7 | 原版 Magpie 聊天/Sessions/Gateway 与 Fusion 未互通 | 产品集成 | 功能缺口 | 需要设计聊天→五阶段入口和 Provider 注册集成 | 待设计 |
| B8 | 自动工作流/Jev/插件更新 off | WP-27–WP-29 | 设计决策 | Gate B 后按需开启 | Gate B 前保持 off |
