# 三条路线真实接入矩阵

2026-10-05 当前核对。所有生成路线仍未准入。下表分开记录实际发布者证明、已有真实请求、尚缺账号/费用/额度证据及完整工程 Smoke；不能把单次连接成功或 Native 合成上游回归算作真实工程闭环。

| 路线 | 固定 Runtime 发布者/文件 | 私有账号及费用 | 已有真实连接/额度 | 合成项目真实读写、测试、取消 |
|---|---|---|---|---|
| codex-chatgpt | [0.160.0 已核验](../work-items/WP-17/PUBLISHER-01/codex-live.json) | [独立设备登录入口](private-runtime-login.md)已准备；本人登录、账号/workspace/地区、subscription 权益待核验 | 最终登录 profile 已取得官方设备挑战并取消/wait；模型生成及额度未核验 | 未执行；[私有缓存读取](../contracts/codex-private-credential.md)及[固定 Forwarder](../contracts/codex-subscription-forwarder.md)组件已验证，生产 Factory/完整准入待完成 |
| grok-subscription | [1.0.48 已核验](../work-items/WP-17/PUBLISHER-01/grok-live.json) | [独立设备登录入口](private-runtime-login.md)已准备；本人登录、账号/workspace/地区、subscription 权益待核验 | 最终登录 profile 已取得官方设备挑战并取消/wait；模型生成及额度未核验 | 未执行；生产 Forwarder 与完整准入待完成 |
| glm-cn-claude | [Claude Code 2.1.287 已核验](../work-items/WP-17/PUBLISHER-01/claude-live.json) | 已登记本人的私有 key；实际账号、Coding Plan 费用和权益仍未核验 | 工具关闭连接诊断与真实只读额度查询已有记录；额度 snapshot 仍为 unverified，Complete=false、Pool.Verified=false | 未执行真实工程闭环；Factory 合成上游验证不能替代此项 |

历史 [GLM 接入核验](../work-items/WP-14/glm-integration-verification.md)及[额度宿主证据](../work-items/WP-15/GLM-QUOTA-HOST-01/summary.md)继续保留原时间和证明范围。不重复消耗订阅生成做同一诊断，也不把历史额度观察视为当前可用额度。

发布者核验输入仅为公共发布内容，三条路线本次模型调用均为 0。临时文件已清理，固定文件版本未变化。账号、模型/effort、上游实际声明、全部调用控制、sandbox、物理额度池和账单路径必须在生成准入前补齐。后续按 [真实核查清单](live-probe-checklist.md)在独立合成目录执行，不使用日常凭据、公司代码、未批准的 API fallback 或充值。Jev off。
