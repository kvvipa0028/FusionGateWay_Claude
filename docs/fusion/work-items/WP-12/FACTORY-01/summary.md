# WP-12 · FACTORY-01

完成可信 Codex 生产执行 Factory 组件；父 WP-12、WP-17、Gate A 和整体目标仍未完成。基线 `d1b90970435f96ce41c28afcec259efdbb1effd0`，工作区承接本组件 7 文件未提交工作并在真实 diff 上继续；未新增 Agent。合同见 [执行 Factory](../../../contracts/codex-execution-factory.md)。

`CodexRuntimeConfig`/`NewCodexRuntimeFactory` 绑定独立 admitted Registry candidate、显式 upstream account、私有 pinned cache、冻结独立 Identity epoch、固定 SubscriptionForwarder、私有执行根与当前项目 source；构造不启动进程、不查 quota、不做准入，公开入口没有上游覆写缝（buildForwarder 仅包内测试）。登记逐项核验 native route 精确为 `codex-chatgpt`、Registry/Identity/缓存/项目声明一致、执行根与认证根及项目互不重叠。GLM 既有 resolver/owned handoff/硬验证/review/acceptance/rework/撤销 watcher 提取到共享 `stage_execution.go`，两条路线复用同一合同；`StageVerificationConfig` 保留 `GLMVerificationConfig` 别名兼容。

本轮修复两项中断时未决风险：一是 Store 授权锁内回调改为独立 `registrationCurrent`（仅 ctx/closed/source current，不读 Store、不调 Registry/Identity/凭据），完整 `current` 复查保留在记录前后，恢复并固化原语义——真实死锁 RED 以依赖 Store 的 Identity 服务探针复现（45s timeout panic，堆栈为 `Controller.observe → RecordArtifactAuthorized → transaction(Store.mu) → storeCurrent → Identity → Store.Task` 等待同一把锁）；二是 Codex 不再复用 GLM 的 low/medium/high/max 白名单，`codexFactoryTarget` 以 admitted route 声明的 Efforts（含 DefaultEffort）核对档位并以编译器逐字段重建 target（官方 0.160.0 ReasoningEffort 为开放枚举，见合同内官方源码链接），未声明档位在复制源码前拒绝。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 初始 RED | CodexRuntimeConfig/构造函数缺失，编译失败 exit1 | [初始 RED](red.log) |
| 首轮合成 GREEN | 3 顶层/34 子场景 PASS（nativeRoutes 核对加入前） | [首轮 GREEN](green-first.log) |
| native route RED→GREEN | 错挂 `grok-subscription` 场景真实 FAIL 后修复 PASS，race exit0 | [RED](native-route-red.log)、[GREEN](native-route-green.log) |
| 档位 RED→GREEN | xhigh/minimal 被 GLM 白名单误拒 FAIL；修复后 xhigh/minimal/undeclared 3 子场景 PASS | [档位 RED](effort-red.log)、[档位 GREEN](effort-green-v.log) |
| 锁内回调 RED→GREEN | 依赖 Store 的 Identity 探针真实死锁（timeout panic + 锁堆栈）；修复后完整 Native 生命周期 7 子场景 PASS，race exit0 | [锁 RED](lock-red.log)、[Native GREEN](lock-green.log) |
| 合成 targeted 完整回归 | PrivateRegistration/ExactCopiesAndRoles/AdmittedEfforts/CurrentBeforeCopy 4 顶层（18+2+3+15 子场景）包级 exit0 | [targeted](effort-green.log) |
| GLM 共享提取回归 | lifecycle+handoff 2 顶层 42 子场景 PASS，固定 Claude 2.1.287 真实进程，race exit0 | [GLM 回归](glm-regression.log) |
| 受影响模块 race | bootstrap/runtime/codex/codexadapter/glm/control/store 6 包 exit0 | [模块回归](bootstrap-full.log) |
| Go1.26.3 CLI/GUI/full vet | 三项实际 exit0 | [构建日志](build-dev.log) |

固定 Native 生命周期覆盖：成功发布 durable handoff 并重开 Store 回读/消费端复制/未登记根拒绝；inflight 取消；Registry 撤销、缓存轮换、epoch 漂移、source 替换均实际终止 owned 进程且仅以终态 Wait 退役 watcher；writable 项目的 Implementation 角色在 intent 前被拒（上游合成调用 0、任务状态与预算不变）。identity/model/quota/账号全部合成，真实供应商调用 0。Native exe 为固定版本化路径 `~/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex`（SHA256 与 [publisher 记录](../../WP-17/PUBLISHER-01/codex-live.json)一致），不使用 mutable launcher。

私有 GLM key 文件当前不存在，exact-byte 排除检查不适用（本轮无 GLM key 使用）；staged 范围内无任何凭据。[test-results.json](test-results.json) 记录命令与计数。

## 未完成与范围

真实登录、三路线真实账号/地区/套餐/计费/额度池准入、产品 CLI/GUI Factory 注册、Codex/Grok 工具/写入/恢复、完整工程 Smoke、Gate A 与最终 T01–T60 验收均未完成；本组件是生产 Factory 装配，不是准入或完成声明。
