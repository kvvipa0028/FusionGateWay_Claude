# WP-13 · FACTORY-01

完成可信 Grok 生产执行 Factory 组件；父 WP-13、WP-17、Gate A 和整体目标仍未完成。基线 `5d7d148d89f2262379a37debec7b979329510c0c`，工作区干净起步；未新增 Agent。合同见 [执行 Factory](../../../contracts/grok-execution-factory.md)。

`GrokRuntimeConfig`/`NewGrokRuntimeFactory` 绑定独立 admitted Registry candidate、私有官方 issuer 缓存、固定 SubscriptionForwarder、私有执行根与当前项目 source；构造零启动/零查询/零准入，公开入口无上游覆写缝。登记核验 native route 精确为 `grok-subscription`、Registry/Identity/缓存/项目声明一致、根互不重叠。Grok 无独立账号 epoch：身份绑定为 route 声明加文件身份钉住，轮换即重新登记。共享阶段执行（含锁内有界 `registrationCurrent`）与 Codex 工厂同一合同。能力边界如实收窄：受管 Adapter 当前只支持 no-effort 只读面，工厂要求 NoEffort 路线并拒绝任何 effort 档位（支持落地后按 route 声明放开）；新增 `Adapter.ValidateLaunch` 使 writable 启动在 intent 前拒绝，杜绝静默只读降级。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 初始 RED | GrokRuntimeConfig/NewGrokRuntimeFactory 未定义，编译失败 exit1 | [初始 RED](grok-factory-red.log) |
| 合成 targeted | PrivateRegistration(13 子含 native_route)/ExactCopiesAndRoles(2)/CurrentBeforeCopy(13) 包级 exit0 race | [合成日志](grok-factory-green.log) |
| 固定 Native 生命周期 | 6 子场景 PASS race exit0：success durable handoff/Store 重开/消费端复制、cancel、registry/credential/source 撤销终止 owned 进程、writer_preintent 0 调用 | [Native 日志](grok-factory-native.log) |
| 受影响模块 | bootstrap/grok/control 三包 race exit0（含新增 ValidateLaunch） | [模块回归](grok-factory-package.log) |
| 有效 mutation | 删除 ValidateLaunch 写入拒绝→writer 静默降级被捕获，exit1 | [mutation](grok-factory-mutation.log) |
| Go1.26.3 CLI/GUI/full vet | 三项实际 exit0 | [构建日志](grok-factory-build.log) |

固定 Native 为版本化路径 `~/.grok/downloads/grok-1.0.48-macos-aarch64`（SHA256 与 [发布者记录](../../WP-17/PUBLISHER-01/grok-live.json)一致）。开发修正记录：初始 fixture 缺 Issuer、误设 effort 档位（受管 Adapter 仅支持 no-effort 面）与 ValidateLaunch 缺失导致的 nil 调用，均已按产品语义修正而非放松断言。[test-results.json](test-results.json) 记录命令与计数。

## 未完成与范围

真实登录、tier/费用/额度、effort 档位、写入/恢复支持、产品 CLI/GUI 注册、Gate A 与最终 T01–T60 均未完成；本组件是生产 Factory 装配，不是准入或完成声明。
