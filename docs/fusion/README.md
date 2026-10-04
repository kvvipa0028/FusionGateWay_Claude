# Fusion Gateway 开发入口

已完成源码/文档导入、Go 1.26.3、OAuth 修复和 WP-01–WP-11：基线、开发隔离、离线 fixtures、五角色绑定编译、独立任务存储、入口鉴权。GLM/Claude Code 连接诊断通过；strict 出口离线验证通过；产品 API、Runtime Adapter 和最终 Gate 尚未完成。

本机服务已有 [可信执行接线](contracts/execution-host.md)，可由进程内 Factory 安装来源绑定 Controller 与额度读取；鉴权 HTTP→固定 Native 的合成端到端验证已通过。真实 Factory/账号准入与产品 CLI/GUI 尚待完成，草稿 CLI仍不执行任务。

固定 Codex 的 [私有启动通道](contracts/codex-bootstrap-channel.md) 已完成实际 metadata 握手、隔离与停止验证；另有[逐次 Responses HTTP 控制组件](contracts/codex-call-gate.md)、[独占 HTTP 通道](contracts/codex-http-channel.md) 和[类型化阶段客户端](contracts/codex-gateway-client.md)。[受管只读 Adapter](contracts/codex-managed-adapter.md) 已接通真实 Scheduler、来源核验及停止释放；[Controller 接线](work-items/WP-15/CODEX-01/summary.md) 已验证启动前拒绝、幂等、断线、暂停/取消/关闭及实际停止释放。[本机 HTTP 验证](work-items/WP-15/CODEX-HOST-01/summary.md) 已通过固定 Codex 进程的执行、取消与持久结果回读。真实登录、订阅 Forwarder、quota 与产品 Factory 注册仍未完成，不能将组件测试作为真实生成准入。

[阶段配置页面](contracts/stage-editor.md)直接复用原 Magpie UI 的样式与布局组件，已有三个区域、五角色独立设置、全局/项目保存和单阶段预览；通过[可信项目列表](contracts/project-index-api.md)发现登记。隔离浏览器测试使用临时管理凭据；[Native GUI 鉴权桥](contracts/native-stage-ui.md)已通过桥接回归和实际窗口保存/载入/退出验证，[命名预设创建/保存](contracts/preset-ui.md)已验证。[按项目任务分页](contracts/task-index-api.md)及受限 Native 读取通路已补齐，[任务工作台](contracts/task-workbench.md)已接入冻结提交、任务分页和计划/预算详情，[服务端提交回执](contracts/task-submission-receipt.md)已支持原请求在预览清理/宿主重启后的核对；[私有提交记录](contracts/task-submission-journal.md)已有存储与受限 [恢复 API/Native 通路](contracts/task-submission-api.md)，现已由[原请求恢复 UI](contracts/task-submission-ui.md)消费，浏览器关闭/宿主重开及同请求核对通过；实际 Native 恢复窗口验证仍待桌面解锁，[受限运行控制桥](contracts/native-task-control.md)已接入，[运行控制页面](contracts/task-execution-ui.md)已在原任务详情区接入阶段选择、启动、暂停、继续、取消及原请求核对；真实供应商执行和实际 Native 运行操作仍未验证。

[GLM 产品额度宿主](contracts/glm-quota-host.md)已接显式只查询的 CLI 与 Management API，并完成一次真实 CN 额度读取；生成、物理池和账号归属仍未准入，[额度观察区](contracts/quota-ui.md)已以默认折叠列表接入原 Magpie UI，支持缓存读取和手动刷新；浏览器/Native 桥验证通过，实际 Native 点击尚未验证。

[任务事件回读](contracts/task-event-page.md)已在原详情附近加入默认折叠区，逐页读取持久状态元数据；CSS 未改。浏览器、Native 桥和宿主重开验证通过，实际 Native 点击与真实模型输出仍未验证。

[原阶段启动持久记录](contracts/task-start-journal.md)已完成 Store 基础：准备原 key/身份、原子关联 run 和封存迟到启动，schema8迁移/回退已验证。[恢复 API 与 Native 通路](contracts/task-start-api.md)已接入，[原 Magpie 详情区恢复页面](contracts/task-start-ui.md)已消费，浏览器关闭/宿主重开后的原记录回读、独立运行核对、确认和明确封存已验证；全部 CSS 未改，实际 Native 点击/像素仍未验证。

## 阅读顺序

1. [当前需求摘录](requirements-summary.md)：已知目标与未取得的原始需求文档。
2. [实施计划](planning/Fusion_Magpie_Fork_实施计划_v1.0.md)：M0–M5 的范围和退出条件。
3. [Agent 工作包](planning/Fusion_Magpie_Fork_Agent工作包_v1.0.md)：WP-01–WP-30 的实施边界。
4. [验收矩阵](planning/Fusion_Magpie_Fork_验收矩阵_v1.0.md)：T01–T60；当前均为 `not_run`。
5. [源码与工具链锁](upstream-lock.json)、[基线报告](baseline/baseline-report.md)：本轮实际准备与验证证据。

`planning/implementation_tasks.json` 和 `planning/acceptance_tests.json` 由本地三份 Markdown 提取，已核对编号、引用及依赖无环；它们不是原 ZIP 的恢复副本。WP-01–WP-11 为 `done`，WP-12–WP-16 为 `in_progress`，其余包为 `planned`；最终验收仍为 `not_run`。

## 源码与 Git

- `origin`：`https://github.com/kvvipa0028/FusionGateWay_Claude.git`。
- `upstream`：`https://github.com/yetone/magpie.git`。
- 当前上游固定为 `main@1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`，后续不自动跟随 `main`。
- 原项目的 `.git`、初始提交及 `origin` 保留。通过 `git archive` 导入源码快照，上游历史保存在 `upstream/main`；本轮没有创建将两套历史合并的提交。
- 保留 `github.com/yetone/magpie` module path，避免修改全仓 import。根 LICENSE 保留上游与本项目版权；上游原文在 `upstream/`。
- 原版 README 见 [README.magpie.md](upstream/README.magpie.md)。原版能力不能当作 Fusion 增强功能已交付。

## 本轮处理范围

源码、文档、来源/许可证记录、发布 workflow 隔离和 Go 1.26.3 已准备。上游 release、Docker 与 UI preview job 限定仅在 `yetone/magpie` 运行，避免本 Fork 继承发布或付费调用。普通 Test workflow 保留。

未加 `fusion` tag 时保留原版回归语义。Fusion 产品构建已有独立身份、目录和端口；旧插件/更新/同步等入口被关闭，共享 Keychain 发现被阻断。原版 `Group.Picked()` fallback 和 loopback 鉴权不能用于严格任务执行；WP-06 已关闭旧宽松入口，WP-07 已实现独立 strict 出口，真实 Runtime 准入仍待验证。隔离范围见 [fork-isolation.md](decisions/fork-isolation.md)。

## 本地验证

要求 `go version` 返回 `go1.26.3`，以及可用的 Python 3 和 macOS 编译工具。执行：

```sh
python3 scripts/fusion/build-dev.py
python3 scripts/fusion/run-dev.py -- fusion-status
```

构建脚本使用临时 HOME/XDG 和环境白名单，执行 Fusion CLI/GUI 编译与 vet。启动入口使用仓库外私有状态，不继承供应商 Token 或原客户端目录。二进制及构建记录保存在 `.fusion-dev/`。完整操作步骤见 [开发隔离说明](decisions/fork-isolation.md)。

`verify-baseline.py` 保留为当前 Fork 的无 tag 检查工具，结果写入 `.fusion-dev/baseline-validation/`，不覆盖原始 `docs/fusion/baseline/` 或 Fusion 产品二进制。无 tag 的结果不是产品隔离验收。

这次验证不覆盖真实账号、GUI 交互、多设备部署或尚未实现的 Fusion 验收。

## 文档缺口

当前工作区未提供《Fusion_最终实施方案_v1.1.md》《Fusion_简化审阅说明_v1.1.md》及原始交付 ZIP。现有需求摘录不能冒充这些文件全文；后续取得原件时应核对遗漏和差异。

## Google OAuth 前置修复

用户已选择外部客户端配置方案，Google 订阅路径不再提供上游硬编码客户端值。配置和旧账号处理见 [Google OAuth 外部配置](decisions/google-oauth-external-config.md)；当前可以保持未配置。`baseline/` 是修改前的上游基线记录，修复后的 [验证报告](decisions/oauth-validation-report.md)、[未发布历史清理](decisions/oauth-history-cleanup.json)、[GitHub 推送回读](decisions/oauth-push-readback.json) 和 [执行取舍](decisions/oauth-execution-decisions.md) 已归档；开发分支 `fusion/implementation` 已推送。原基线二进制不代表当前代码。

## GLM 接入选择

GLM 已确定为中国大陆 Coding Plan + API key，通过私有入口录入并完成 Claude Code 真实连接诊断。端点、认证/计费边界与未验证项见 [GLM 接入决定](decisions/glm-coding-plan-api-key.md)。诊断不代表 WP-14、额度、计费或严格模型锁定已验收。

WP-03 离线 fixtures 已完成，见 [证据](work-items/WP-03/summary.md) 与 [fixture-index](../../tests/fusion/fixtures/fixture-index.json)。五角色、strict locked、产品 Runtime 与最终 Gate 尚未完成。

WP-04 role merge/freeze contract completed: [evidence](work-items/WP-04/summary.md), [contract](contracts/route-contract.md). Strict execution/admission/API gates remain pending.

[WP-05 evidence](work-items/WP-05/summary.md), [state/storage contract](contracts/state-transition-contract.md).

[WP-06 evidence](work-items/WP-06/summary.md), [stage credential contract](contracts/stage-credential-contract.md), [ingress auth map](contracts/ingress-auth-map.md).

WP-08 official native candidate inventory: [account routes](integration/account-route-matrix.md), [capabilities](integration/capability-matrix.json), [live checklist](integration/live-probe-checklist.md). All real generation/quota routes remain unverified.

WP-09 quota normalization and bounded broker completed with synthetic inputs: [contract](contracts/quota-adapters.md), [schema](contracts/quota-snapshot.schema.json), [fixtures](integration/quota-fixtures.json). No real quota query admitted yet.

WP-10 准入/调度预算与原子预留已完成离线验证：[阻断原因与调度合同](contracts/blocked-reason-catalog.md)、[验证证据](work-items/WP-10/summary.md)。实际进程树退出证明和真实路线准入仍待后续工作包。

WP-11 Worker/项目副本基础已完成本机原生沙箱验证：[Runtime 合同](contracts/runtime-contract.md)、[沙箱证据](contracts/sandbox-proof.md)、[结果](work-items/WP-11/summary.md)。多进程、网络、resume 未获准，真实 Native Adapter 仍待接入验证。

## Grok 接入进度

Grok 固定 Native 的 headless 观察器、逐 HTTP Gate 和可选受控文本Read已完成本机合成诊断与真实 Store/Scheduler 预算回归，WP-13 仍在实施中：[进度](work-items/WP-13/summary.md)、[锁定边界](work-items/WP-13/grok-lock-capability.md)。真实 Grok Worker、订阅准入及准确恢复尚未完成。

Grok受管生命周期组件：[CHANNEL-01证据](work-items/WP-13/CHANNEL-01/summary.md)、[channel合同](contracts/grok-managed-channel.md)。实际固定Native合成8场景与StopProof已验证；完整Adapter/真实X路线与产品执行仍未准入。

Grok只读Runtime Adapter：[ADAPTER-01证据](work-items/WP-13/ADAPTER-01/summary.md)、[Adapter合同](contracts/grok-adapter.md)。实际固定Native/private prompt/read/取消/停止已验证；真实X准入、Resume/write/工程闭环与final Gate继续待完成。
