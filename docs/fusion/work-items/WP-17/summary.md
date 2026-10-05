# WP-17 · 三条路线真实受控 Smoke Test

状态：in_progress。已完成 [PUBLISHER-01](PUBLISHER-01/summary.md)：三条固定 Runtime 的实际文件与官方发布内容/签名核验、可复现无认证工具、负向回归。该组件不使用账号、不消耗模型额度、不授予生成权限。

[PRIVATE-LOGIN-01](PRIVATE-LOGIN-01/summary.md) 已提供本人独立 Terminal 设备登录入口。最终生产 profile 和环境下两条路线均实际取得官方设备挑战并取消/wait；13 个新增测试与 15 个发布者回归通过，没有本人登录、缓存、模型调用或生成准入。操作见 [私有登录说明](../../integration/private-runtime-login.md)。

整体 Smoke 尚未完成：OpenAI/X 官方私有登录、账号/权益/地区与生产 Forwarder、GLM 账号/计费/物理额度池、三路线真实模型与工程读写/测试/取消证据仍缺失。Native/Store/HTTP 合成回归不算真实 Smoke。[当前矩阵](../../integration/live-matrix.md)。所有相关最终 T01–T60 验收状态仍保持原值，Gate A 未完成。
