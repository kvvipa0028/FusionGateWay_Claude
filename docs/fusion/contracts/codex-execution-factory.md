# Codex 生产执行 Factory

本组件 `WP-12-FACTORY-01` 提供进程内可信 Factory，把 [受管只读 Adapter](codex-managed-adapter.md)、[私有文件凭据](codex-private-credential.md)、[固定订阅 Forwarder](codex-subscription-forwarder.md)、独立账号 epoch、Registry 准入和私有执行根组装为真实生产注册。它不证明本人已登录或账号/地区/套餐/计费/共享额度池准入；三路线真实准入与完整工程 Smoke 仍未完成，最终 T01–T60 不升级。Jev off。

## 可信构造与登记

`CodexRuntimeConfig` 是可信 server wiring，不是 HTTP/JSON DTO。`NewCodexRuntimeFactory` 构造时不启动进程、不查询 quota、不做准入判定：必须给出绝对规范路径的固定 Native exe、私有缓存文件、显式 upstream account、私有执行根和当前项目 source。执行根与认证根互不重叠，也不与任何已登记项目路径重叠；构造即冻结 TestingWritePaths 与 Verification 副本。公开入口没有上游替换缝：只有包内测试可用 buildForwarder 注入合成上游，产品调用方不能覆盖 URL、client、key、代理或 fallback。

登记（RuntimeFactory 调用）逐项核验后才返回注册：环境齐全且当前、执行根未替换、项目可读且 native route 精确为 `codex-chatgpt`（防止与 GLM `glm-cn-claude` 或 Grok 订阅路线互换）、Registry candidate 为 openai/codex/oauth/local_user/subscription 且地区已核验、resolved route 绑定固定 CLI 版本、非插件、subscription 计费、controlled_calls、项目声明的 route 与 admitted route 逐字段一致、独立 Identity epoch 与 route 的账号/workspace/credential identity 精确匹配、私有缓存按 [FileCredential](codex-private-credential.md) 作用域打开且文件权限/身份钉住。任何一项漂移即拒绝登记。

## 共享阶段执行与锁内权限

工程 resolver、owned handoff、硬验证、review/acceptance/rework 与运行期撤销 watcher 由两条路线共享（`stage_execution.go`）：GLM 既有行为不变，Codex 复用同一合同——冻结 prompt/角色/目标/副本、独占 launch 根、写入路径约束、durable handoff 发布与 Store 记录。

Store 的授权记录在自身事务锁内回调 `registrationCurrent`。该回调只做有界本地检查（调用 context、工厂关闭标志、source current），不读 Store、不调 Registry/Identity/凭据；完整 credential/Registry/epoch 复查在记录前后以 `current` 独立执行并保持原语义。此边界以真实死锁复现测试固定：Identity 服务自身依赖 Store 时，锁内回调不得调用它。

## 档位与目标核对

官方 0.160.0 的 ReasoningEffort 是开放枚举（none/minimal/low/medium/high/xhigh/max/ultra/persistent 及模型宣告的自定义值，见[官方源码](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/protocol/src/openai_models.rs)）。Codex 工厂因此不复用 GLM 的固定档位白名单：`codexFactoryTarget` 以 admitted route 声明的 Efforts（含 DefaultEffort）为准确认目标档位，再以配置编译器从 route 逐字段重建 target 并要求完全一致。未声明档位（如 ultra 未在 route 声明时）与任何 target 漂移在复制项目源码之前拒绝。

## 运行期撤销与能力边界

每次 launch 的 watcher 以 100ms 间隔复查完整 `current`：Registry 撤销、缓存轮换、epoch 漂移、source 替换或执行根替换都会实际取消 owned Native 进程的上下文，仅凭终态 Wait 退役 watcher，StopProof 与 release 仍归 Controller。五角色与 writer 配置不降级：Codex 当前不支持的写入/工具/resume 在 intent 之前由 Adapter 预检拒绝，不会静默降级为只读成功，也不以父包完成冒充。

## 验证、剩余和回退

[组件报告](../work-items/WP-12/FACTORY-01/summary.md)记录合成准入/identity/cache/quota 下的私有登记拒绝矩阵、精确副本与角色/目标/档位核对、复制前漂移拒绝、固定 Native 0.160.0 的完整生命周期（成功发布 durable artifact 与 Store 重开、inflight 取消、Registry/缓存/epoch/source 撤销终止真实 owned 进程、写入 intent 前拒绝）、锁内回调死锁复现与修复，及 GLM lifecycle/handoff 与受影响包 race 回归。合成数据不能替代真实账号/地区/计费/额度池/续期证明。

Go1.26.3 的目标测试命令：

```sh
go test -mod=readonly -tags fusion,nogui -race -count=1 -timeout=120s \
  ./internal/fusion/bootstrap -run 'TestCodexFactory(PrivateRegistration|ExactCopiesAndRoles|AdmittedEfforts|CurrentBeforeCopy|PinnedNativeProductLifecycle)$' \
  -fusion-host-native-codex <固定 0.160.0 可执行文件>
python3 scripts/fusion/build-dev.py
```

真实登录、三路线真实准入、Codex/Grok 工具/写入/恢复、产品 CLI/GUI 注册、Gate A 与最终验收仍待完成。回退可撤销该可信 Factory 登记；不影响认证缓存、原 Magpie UI、Store schema 或 Jev off。
