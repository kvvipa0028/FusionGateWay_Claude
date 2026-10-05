# 阶段代码冻结与交接 v1

`workspace.Freeze` 从受管阶段的实际私有副本生成独立 `code/` 和 `manifest.json`；`handoff.Publish` 形成 `handoff.json`。GLM Factory 在真实进程停止、原 Supervisor 核验 StopProof、Store Task/Plan/Run/target 对应后执行发布，再释放原资源预留。失败、取消不发布成功包；冻结、绑定或发布失败保留预留，进入显式 reconciliation。UI 沿用原 Magpie 页面，本组件不修改 UI/CSS。

## 内容与 hash

```
launch-*/
  workspace/              # 可变生产者副本
  worker/                 # 原隔离 Runtime 状态，不交接
  handoff/
    code/                 # 独立代码快照，不含认证/Native 状态
    manifest.json
    handoff.json
```

Manifest v1 固定原始来源的 `base_commit`、`base_tree_hash`、本次 `tree_hash` 和 `change_hash`。Git 元数据只从项目根显式 `.git` 获取；已有有效 HEAD 必须可验证，不搜索父仓库、不自动 commit。非 Git 或空 `.git` 占位目录的 base_commit 为 null，不伪造 commit。没有可解析 HEAD 的实际 Git 仓库拒绝。读取使用 `/usr/bin/git rev-parse`、2 秒期限、2048 字节输出上限、环境白名单及禁用全局配置/fsmonitor/hooks；源码仍由打开的目录与 inode/content seal 核验。

树条目按相对 Path 排序，包含目录（包括空目录）、文件 SHA-256/长度/owner execute 位。tree hash 是该条目数组的 Go `encoding/json.Marshal` 字节 SHA-256。Copy 保留 owner execute，移除 group/world 权限；不会因此增加 Native 的进程或工具权限。私有文件写模式不是源码语义；冻结代码文件为 0400，脚本为 0500。

Changes 按 Path 排序，每项为 before/after 条目，null 表示新增/删除。change hash 是该数组同一 JSON 编码的 SHA-256。这是可复核的完整树变化摘要，支持二进制文件；**不是 Git 文本 patch 的 hash**。交接同时保留全部真实代码，消费者无需由模型文字或不完整 patch 重建。测试阶段的新文件、产品代码改动和删除都产生新的 tree/change hash，旧审查不得覆盖新版本。UTF-8 不合法的路径拒绝冻结，避免 JSON 替换字符造成身份歧义。

沿用原有 20000 条目/10000 文件/单文件 20 MiB/总计 100 MiB 边界；`.git`、`.claude`、`.codex`、`.grok`、`.fusion-dev`、`.env` 不进入代码快照。文件/目录符号链接、硬链接、特殊文件拒绝。文件写保护配合内容和身份再核验；这不是对当前用户其它进程的不可变磁盘锁。

## 来源与消费者

`FrozenArtifact`、`Bundle`、`SourceGuard` 保留私有内存 provenance；Path/JSON/Files/hash 字符串不能创建授权对象。`Manifest()`/`Read()` 返回副本，改动返回值不影响原对象。受控重启恢复使用私有 Store 的 released-run receipt，以及独立登记的原项目和执行根；格式与边界见 [持久产物合同](durable-stage-artifact.md)。没有从任意 HTTP JSON/path 自动恢复可信对象的入口。

`Bundle.Copy(expectedBinding, privateRoot, name)` 先核对完整 binding、代码和清单，再生成新的私有工作副本，之后复查父包。binding 必须对应 task_id、plan_revision/hash、run_id、generation、role、target_hash。跨任务、跨计划、错误角色或目标都拒绝。原始来源、冻结代码、manifest 或 header 的内容/inode/权限发生变化也拒绝；坏包不发布 consumer 副本。

消费者 guard 仍绑定原登记的项目目录，并同时检查每个冻结祖先，最多 16 层；不会把产物目录冒充已登记原项目。后来修改可变 producer workspace 不影响已经冻结的代码；修改原项目或任一冻结祖先会使下游 guard 失效。Freeze/Copy 拒绝写入原登记目录。并行消费者拿到独立副本；后续 Runtime 仍须分别核验角色和只读/写权限。

## 工程证据边界

handoff.json 包含 version/binding/task/artifact/decisions/evidence/changes/pending。完整格式见 [schema](handoff.schema.json)。本版本 task 保留实际冻结 goal；design decisions 尚未提取，数组为空且 pending 明示。evidence 仅记录原 Runtime output hash 和实际 StopProof report hash，status 固定 unverified，tests_executed 固定 false。没有原始 stdout、凭据、原 Native session 或账号授权；消费者使用新 Runtime/session。Schema 只验证结构，不能证明 hash 内容、停止或供应商准入。

当前真实 GLM producer 发布前后核对 Store 和原来源，成功包需真实停止与资源释放才能用于后续工作。私有 Store 的 durable artifact index 与重启后受控 rehydrate 已实现，并校验 Task/Plan/Run/target、实际 released StopProof 与父包来源链。尚缺 **Task/Plan/parent-run 绑定的产品下一阶段 resolver**。因此多角色 Factory 仍返回 unsupported。磁盘上的包不是自动派单许可，也不是测试执行、审查、验收或 Gate A 通过；WP-20 继续 in_progress，完整 WP-19–WP-23 与 T01–T60 保持要求。

## 复现、故障与回退

在 implementation 工作树根使用 Go1.26.3 和原隔离 runner：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/artifact-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=150s -p=2 -v ./internal/fusion/workspace ./internal/fusion/handoff

PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/artifact-native-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=90s -run '^TestGLMFactory' -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
```

Native 使用固定 Claude Code 2.1.287 和合成模型/准入/凭据；不发送真实 key，不调用真实模型或 Jev。不具备 pin 时 Native 测试 skip，不能算通过。实际上游生产传输继续为固定 CNTransport。

目录冲突、source/base/hash 漂移或发布不完整时不覆盖、不重放、不删除未知目录，也不把成功文本视为交接成功；保留 launch 目录和 reservation 供人工对账。回退先停止派单、核验 owned Handle 已停止及预留，再移除 producer 接线；不回滚 SQLite、不自动提交原项目、不删未经核验的旧目录。当前没有自动恢复部分发布包的授权入口。

完整证据见 [FROZEN-ARTIFACT-01](../work-items/WP-20/FROZEN-ARTIFACT-01/summary.md)。
