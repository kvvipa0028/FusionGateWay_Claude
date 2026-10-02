# Fusion Gateway 开发入口

本仓库以 Magpie 为基座，增加由用户指定阶段模型的工程工作流。当前只完成源码导入、文档归档与 Go 工具链准备；五角色配置、strict locked、任务管理和 Runtime 尚未实现。

## 阅读顺序

1. [当前需求摘录](requirements-summary.md)：已知目标与未取得的原始需求文档。
2. [实施计划](planning/Fusion_Magpie_Fork_实施计划_v1.0.md)：M0–M5 的范围和退出条件。
3. [Agent 工作包](planning/Fusion_Magpie_Fork_Agent工作包_v1.0.md)：WP-01–WP-30 的实施边界。
4. [验收矩阵](planning/Fusion_Magpie_Fork_验收矩阵_v1.0.md)：T01–T60；当前均为 `not_run`。
5. [源码与工具链锁](upstream-lock.json)、[基线报告](baseline/baseline-report.md)：本轮实际准备与验证证据。

`planning/implementation_tasks.json` 和 `planning/acceptance_tests.json` 由本地三份 Markdown 提取，已核对编号、引用及依赖无环；它们是本轮派生文件，不是原 ZIP 的恢复副本。工作包状态仍是 `planned`。

## 源码与 Git

- `origin`：`https://github.com/kvvipa0028/FusionGateWay_Claude.git`。
- `upstream`：`https://github.com/yetone/magpie.git`。
- 当前上游固定为 `main@1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`，后续不自动跟随 `main`。
- 原项目的 `.git`、初始提交及 `origin` 保留。通过 `git archive` 导入源码快照，上游历史保存在 `upstream/main`；本轮没有创建将两套历史合并的提交。
- 保留 `github.com/yetone/magpie` module path，避免修改全仓 import。根 LICENSE 保留上游与本项目版权；上游原文在 `upstream/`。
- 原版 README 见 [README.magpie.md](upstream/README.magpie.md)。原版能力不能当作 Fusion 增强功能已交付。

## 本轮处理范围

源码、文档、来源/许可证记录、发布 workflow 隔离和 Go 1.26.3 已准备。上游 release、Docker 与 UI preview job 限定仅在 `yetone/magpie` 运行，避免本 Fork 继承发布或付费调用。普通 Test workflow 保留。

原版 `Group.Picked()` 的 fallback、loopback 鉴权语义和插件请求路径未修改。Fusion 的严格锁定由 WP-04–WP-07 建立完整合同后实施；插件/账号/effort 路径需纳入同一个严格出口。应用身份、默认目录、端口、更新与插件自动更新的全面隔离仍属于 WP-02，尚未完成。

## 本地验证

要求 `go version` 返回 `go1.26.3`，以及可用的 Python 3 和 macOS 编译工具。执行：

```sh
python3 scripts/fusion/verify-baseline.py
```

脚本下载锁定的公开 Go 依赖，执行 CLI/GUI 编译、vet 和相关包测试。运行测试使用临时 HOME/XDG 目录和环境白名单，不继承供应商 Token 或 live-test 开关；不运行应用登录、真实模型或 Jev。二进制留在 `.fusion-dev/`，命令、退出码和日志摘要留在 `docs/fusion/baseline/`。

这次验证不覆盖真实账号、GUI 交互、多设备部署或尚未实现的 Fusion 验收。

## 文档缺口

当前工作区未提供《Fusion_最终实施方案_v1.1.md》《Fusion_简化审阅说明_v1.1.md》及原始交付 ZIP。现有需求摘录不能冒充这些文件全文；后续取得原件时应核对遗漏和差异。
