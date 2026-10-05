# 持久阶段产物与受控恢复 v1

本合同补充 [阶段交接](stage-handoff.md)。Store schema10 为真实成功阶段保存不可变 `ArtifactRecord`；它是内部受保护数据，没有 HTTP importer、CLI JSON 授权开关或 UI 导入按钮。原 Magpie UI 与所有 CSS 保持原状，Jev off。

## 发布与可消费条件

GLM 每次 launch 仍核验原 Supervisor 的真实 StopProof、owned Handle 终态、Store Task/Plan/Run/target、当前私有登记与来源，再冻结和发布真实代码。`Bundle.Reference` 输出完整 binding、代码/base tree hash、header hash/身份、StopProof hash 和版本化 witness。`RecordArtifactAuthorized` 在同一 Store transaction 内校验真实成功 run、confirmed startup intent、原预留、冻结目标、Task 当前 ready/generation/plan、父阶段与 current fence，然后写入 receipt 和 event。callback 必须是有界本地检查，不得重入 Store。

顺序固定为：真实停止核验 → 文件冻结/发布 → 私有 receipt 登记 → 原 backend Release。文件系统、SQLite 和 Native 没有虚构的跨系统原子提交。登记幂等且不可替换；SQL UPDATE/DELETE 保护历史，event 或最终 current 检查失败会回滚。单角色及工作流首阶段必须空 parent，input tree 等于原 base tree。后续工作流阶段必须指向同 task 的紧邻前角色最新较早 generation run，父 receipt 已 released、input tree 等于父 tree，base 一致且 witness 包含全部原父链后只增加一个产物。

`Store.Artifact(runID)` 重新校验 receipt canonical JSON/checksum、真实历史 Plan/目标和成功 run，只在原预留 **released 且 StopProof hash 完全匹配** 时返回。已登记但仍 held 返回 `ErrArtifactPending`；不存在返回 `ErrNotFound`。返回历史 receipt 不证明当前文件仍有效，也不自动提交下一阶段。原目标即使相同，另一 run/plan/generation 的 receipt 也不能替代。

## 重启恢复边界

`handoff.Restore(reference, registeredSource, privateExecutionRoot)` 的 reference 必须来自上述受保护查询，source/root 必须来自独立当前宿主登记。SHA-256 自校验只是完整性检查，不能将用户自写 witness/hash 当作授权。恢复不会发放 Runtime、账号或角色权限，不会复用 Native session。

witness v1 记录原来源、执行根、全部冻结祖先的 device/inode/mode/owner/link/文件长度/mtime、内容 hash 与 manifest/header 指纹。源码和代码目录先用真实 `os.FileInfo` 校验持久指纹，再读取内容并复查；不构造可绕过 `os.SameFile` 的虚假 FileInfo。额外/缺少条目、文件/目录替换、链接、权限改变、来源/Git base 漂移、header/manifest/code 内容变化、版本或 canonical 编码不匹配都拒绝。

原 source 必须保持原登记路径，执行根及每个 artifact 必须私有且位于其登记根内。外层 metadata 目录允许增加兄弟内容，因此仅忽略外层 directory nlink；固定代码树的条目与文件 nlink 检查保持。witness 上限 32 MiB，祖先最多 16 层；每个树继续遵循原 20000 条目/10000 文件/20 MiB 单文件/100 MiB 总量边界。receipt canonical JSON 上限 40 MiB。witness 不含文件正文、认证、stdout 或 Native session，但含私有路径与身份，保存在私有 Store，不能作为公共日志输出。

恢复后 `Bundle.Copy` 仍核验完整 binding 和所有祖先，创建独立 mutable copy。真实测试、新代码和变更会生成新树；原父 code、manifest 或 handoff header 漂移使下游 guard 失效。改动旧可变 producer 不影响已经独立冻结的产物。

## Schema10、故障与回退

`010.sql` 增加 `stage_artifacts`、写保护/插入核验 triggers 和 `migration_010_sha256`。升级在 transaction 内执行；失败无部分表/trigger/version，修复原因后重开可重试。旧任务、submission、workflow/design/approval 不重写。schema10 checksum 漂移及 future schema11 均拒绝打开；001–009 不改。

生产回退必须先停止派单，核验 owned 进程真正停止、reservation 与 receipt 状态，关闭宿主。升级前使用原已关闭实例的完整私有 stateRoot 备份；应连同原 executionRoot 保留，不能用重新复制的代码冒充原 inode。不要删除 stage_artifacts、更改 user_version 或覆盖 checksum 来让旧程序打开 schema10。旧版本不能读取新数据库；只有在明确接受备份后新增运行记录不会随旧备份保留的前提下，才能恢复升级前独立备份并使用旧二进制。当前组件不提供自动数据库 downgrade 或自动搬迁 artifact。

若文件发布成功但 DB/event 失败，或 receipt 已登记而 Release 未完成，保留原目录/预留并显式对账。不要覆盖 foreign 目录、重放模型请求、生成新 run 掩盖原记录，或在重开时自动运行下一阶段。持久 witness 绑定 inode，移动/重建原根会拒绝；迁移恢复需另行设计受控身份迁移，不忽略校验。

## 验证与剩余工作

在 implementation 工作树根，使用 Go1.26.3 和现有隔离 runner；它创建临时 HOME/XDG，不继承真实认证：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/durable-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...

PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/durable-native-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=90s -run '^TestGLMFactory' -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287

PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

固定 Native 测试经过实际 Claude Code、产品 HTTP、Controller 和 SQLite，在实际 Edit 后关闭 producer 宿主、重开数据库、从 receipt 恢复并独立复制修改代码；预算也从重开 Store 读取。模型、准入和 key 均为合成 fixtures，不调用真实模型/额度。缺少固定 Native 时明确 skip，不算通过。

[本组件证据](../work-items/WP-20/DURABLE-ARTIFACT-01/summary.md)。[产品下一阶段 resolver](glm-stage-handoff.md)与 implementation/testing 实际写范围已接入。[实际 testing 直接验证与持久证据](verification-runner.md)已接入；受控子进程工具链、完整 review/acceptance 与有限返工仍需实现，多角色审查/验收保持阻断，WP-20 继续 in_progress。真实 Gate A、Native UI 和最终 T01–T60 未通过。
