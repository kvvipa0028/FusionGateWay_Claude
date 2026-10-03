# Fusion Gateway

基于 [Magpie](https://github.com/yetone/magpie) 的个人 AI 工程工作平台，计划增加按阶段指定模型、受控任务执行、阶段交接与真实验证证据。

**当前状态：**已完成源码/文档导入、Go 1.26.3、WP-01–WP-06 基线、隔离、fixtures、绑定编译、存储与入口鉴权；GLM/Claude Code 连接诊断通过。五角色、strict locked、任务闭环和三路准入尚未验收。

- [开发入口与当前状态](docs/fusion/README.md)
- [需求摘录](docs/fusion/requirements-summary.md)
- [实施计划](docs/fusion/planning/Fusion_Magpie_Fork_实施计划_v1.0.md)
- [Agent 工作包](docs/fusion/planning/Fusion_Magpie_Fork_Agent工作包_v1.0.md)
- [验收矩阵](docs/fusion/planning/Fusion_Magpie_Fork_验收矩阵_v1.0.md)
- [固定源码与工具链](docs/fusion/upstream-lock.json)
- [本轮基线报告](docs/fusion/baseline/baseline-report.md)

上游原版产品说明在 [README.magpie.md](docs/fusion/upstream/README.magpie.md)。保留其 Go module path 和源代码边界；本项目新增内容同样采用 MIT License，保留上游版权与许可原文。

本地验证：

```sh
go version
python3 scripts/fusion/verify-baseline.py
```

验证只编译和运行隔离测试，不启动应用、登录或真实模型调用。
