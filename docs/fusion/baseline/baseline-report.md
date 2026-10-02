# 本轮基础准备与基线报告

日期：2026-10-02。用户授权范围：使用最新源码、将文档加入项目仓库、处理 Fork 基础问题、安装 Go 1.26.3。本轮未自动提交、推送、登录或执行真实模型请求。

## 完成内容

- 从上游 main 获取并锁定 `1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`；tree 为 `0a63ef36060459f78a425fa0f8e26f31a69ea25e`。相较原计划 `4afb698` 增加 10 个提交，58 个文件变化，4,032 行新增、155 行删除。详细统计见 [delta-from-plan.txt](../upstream/delta-from-plan.txt)。锁定后不自动追随 main。
- 通过 git archive 将 1,769 个上游文件加入 FusionGateWay_Claude，保留其初始提交和 origin。upstream/main 保留上游历史，upstream push URL 为 DISABLED。没有创建或完成跨历史的 merge commit。
- 根 LICENSE 同时保留 yetone 与 tony zhao 的版权；保存原版 LICENSE、README、AGENTS，并恢复 `internal/qoder/LICENSE`。仅 7 个上游文件有预期差异：LICENSE、README.md、AGENTS.md、.gitignore 及 release/docker/ui-preview workflow。Go 应用实现和 go.mod/go.sum 均与固定上游一致。
- 将三份本地实施文档复制到 `docs/fusion/planning/`，原父目录文件保留。更新项目副本的执行基线和准备状态；补齐从 Markdown 提取的 30 个工作包与 60 类验收 JSON，核对编号完整、引用有效、依赖无环。全部 WP 仍为 planned，Fusion 验收仍为 not_run。
- 为上游发布、Docker 推送及付费 UI preview 添加仓库条件，Fork 中不会执行这些 job；普通 Test workflow 保留。4 份 workflow 的 YAML 和 6 个发布/预览 job 的条件已解析核对。
- 按官方归档 SHA256 `875cf54a15311eee2c99b9dd67c68c4a49351d489ab622bf2cfd28c8f2078d3c` 安装 Go 1.26.3 darwin/arm64 至 `/Users/zhaojianzhi/.local/share/go/1.26.3`，通过 ~/.local/bin 启用；现有 ~/.zprofile 保留内容后增加 PATH 入口，原始副本在 `/Users/zhaojianzhi/.local/share/go/zprofile.before-go1.26.3`。原 Go 1.23.12 安装保留。
- 新 login 与 interactive shell 均返回 `go version go1.26.3 darwin/arm64`；本项目及参考项目的 codebase-memory 索引已建立/刷新。

## 验证结果

命令、退出码、日志 hash 与环境见 [verification-results.json](verification-results.json)。验证使用临时 HOME/XDG、环境白名单、公开依赖与 compiler cache；不继承真实供应商 Token、CODEX_HOME 或 live-test 开关。

| 检查 | exit code | 用时秒 | 原始日志 |
|---|---|---|---|
| dependencies | 0 | 21.06 | [dependencies.log](dependencies.log) |
| module-integrity | 0 | 0.8 | [module-integrity.log](module-integrity.log) |
| build-cli | 0 | 1.36 | [build-cli.log](build-cli.log) |
| build-gui | 0 | 13.65 | [build-gui.log](build-gui.log) |
| vet | 0 | 3.04 | [vet.log](vet.log) |
| targeted-tests | 0 | 87.03 | [targeted-tests.log](targeted-tests.log) |

Targeted test 覆盖 internal/appdir、internal/access、internal/provider、internal/gateway、internal/qoder，五个包均通过。CLI 使用与上游 Makefile 一致的 CGO_ENABLED=0；GUI 使用 CGO_ENABLED=1 与显式 macOS SDK。GUI build 存在编译警告，退出码为 0；本轮没有修改上游源码来消除这些警告。

二进制只构建、不运行，位于忽略的 `.fusion-dev/`，hash 见 [artifacts.json](artifacts.json)。JSON/Python 语法、源码导入一致性、版权原文与文档引用已检查。

## 初始失败及处理

首次 CLI 构建误用了 /usr/local/include 下的 SDK 头文件并触发 runtime/cgo 的 nullability error；原日志见 [build-cli-initial.log](build-cli-initial.log)，初始结果见 [verification-results-initial.json](verification-results-initial.json)。发现外部 shell 存在 CPATH 后，采用环境白名单、Apple clang、显式 SDK，并按上游 CLI 设置关闭 cgo。没有删除系统头文件、降低编译错误级别或修改上游实现。修正环境后的 CLI/GUI 均通过。

依赖下载与 module verify 已通过且依赖文件未变，恢复运行时复用这两项结果；没有重复下载或将初始失败描述为通过。

## 明确未完成与未验证

- WP-01–WP-03 尚未整体验收：完整基线 Gates、应用默认目录/端口/更新身份隔离和 fake Runtime 仍需后续实施。
- strict locked、阶段身份、冻结快照和插件出口控制尚未实现；当前源码保留原版 manual、fallback 与 loopback 语义。复用点和后续边界见 [feature-to-code-map.md](feature-to-code-map.md)。
- 未运行完整上游测试套件、GUI 浏览器交互、真实三家订阅、部署或 Fusion T01–T60。
- 原最终方案/简化说明 v1.1 与原交付 ZIP 未在本地提供；本轮需求摘录和派生 JSON 不冒充原件。
- 本轮变更已暂存，HEAD 保持原初始提交 e2bdb76；未提交、未推送、未发布。源码历史引用已保留，但 GitHub 上的仓库尚未因此变成平台意义的 Fork。
