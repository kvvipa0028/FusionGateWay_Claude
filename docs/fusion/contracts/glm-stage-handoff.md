# GLM 阶段 resolver 与写范围

本合同接续[持久产物](durable-stage-artifact.md)与[Factory](glm-execution-factory.md)。当前实际接入 design→人工批准→implementation→testing。实际 testing 已接入直接执行器、硬证据和重启回读。完整目标仍包含受控子进程工具链、审查、验收和有限返工；多角色 review/acceptance 在其受控消费者完成前保持 unsupported。原 Magpie UI/CSS 不改，Jev off。

## 精确输入与执行

`Store.StageArtifactInput(taskID, TaskVersion, role)` 在私有 Store 锁内匹配当前完整 ready Task condition、冻结 Plan binding、有限 workflow 的 Next/Blocker、实际 Design/Approval 和精确前角色 run。首阶段无 parent；后续必须读取原 released StopProof 对应的不可变 artifact receipt，不能用另一个 Task、虚构 hash 或磁盘文件存在代替授权。历史父包保留真实历史 Plan/target binding；当前阶段仍核验当前冻结 Plan 和批准，不用当前模型重新解释旧包。

Factory 核对实际 Store Task 和冻结目标，然后恢复父包，检查原登记 source/ExecutionRoot 及所有 code/manifest/header 祖先。后续阶段调用 `Bundle.Copy`，从真实父代码生成独立副本。没有父产物时拒绝，不能偷偷复制原项目；首阶段和独立单角色才使用原项目 Copy。每次启动由原 Adapter 创建新 session，不跨账号、供应商或阶段复用原 Native session。

Prompt 保留 role/goal、完整冻结 Workflow Design/Approval、父 binding/tree/base/change/header hash、advisory evidence/pending 和实际 WritePaths。它不携带 witness、认证或原 session，不把 output success 当作 tests executed。完整代码已在工作副本，不能靠模型文字重建。继续沿用 64 KiB input 上限，超限明确拒绝，不截断方案或改变合同。

Controller/Store 继续独占 prepare/start/generation 和唯一 writer reservation。Factory 在实际 Start 前复查 prepared run generation/Plan/target、Spec 目录/输入/写范围、原来源/父包 guard 和当前 Design/Approval。实际成功停止后再核验上下文、冻结真实代码，并把 exact ParentRunID/InputTreeHash 登记到原 receipt 后 Release。来源、父 header/code 或批准内容漂移不能接着运行；reopen 只恢复数据，不自动派单。

## 写范围与兼容性

可信 Go Factory 配置新增 `TestingWritePaths []string`，在构造时深复制。它没有 HTTP、用户 JSON 或模型授权开关。只接受 128 项以内、每项最长 4096 字节的正常相对路径；拒绝绝对路径、穿越、重复、控制字符、反斜杠/冒号和认证目录。范围是字面文件/子树，**不解析 glob**。TestingWritePaths 不允许 `.`，可写 testing 缺少范围时拒绝，不给整个项目。

```go
// 保留原 independentlyVerifiedRegistry/currentAdmissionInspector 等可信输入。
config.TestingWritePaths = []string{"tests", "integration-tests"}
factory, err := bootstrap.NewGLMRuntimeFactory(config)
```

多角色 implementation 使用人工批准的 Design.Scope；testing 使用 TestingWritePaths 与 Design.Scope 的交集，空交集拒绝。比如 design scope 为 `src` 和 `tests/unit`、项目测试范围为 `tests`，testing 只得到 `tests/unit`。`tests` 不包括 `tests2`。源项目仍须显式允许 write，并有原 Scheduler writer key；方案批准和测试范围不能扩展项目权限。

可信 `runtime.Spec.WritePaths` 是可选内部字段，由 Host/Controller/GLMAdapter/Supervisor 逐层复制。nil 维持原可写诊断和独立 implementation 合同；readonly 不能携带可写范围，非法值在准备/启动前拒绝。其他 readonly Adapter 同样拒绝无效范围，不因此增加写权限。产品可写 testing 一律需要明确 TestingWritePaths。

macOS Seatbelt 只对实际 Workspace 下相应 subpath 授予 file-write，继续禁止原项目、其它账号状态、进程 fork 和额外网络。实际进程的正控制可写 tests，负控制不能写 src/tests2/根文件。跨范围的 Native Edit 工具错误不会验证为成功；写阶段失败停在 needs_review，不发布成功产物。

实际成功后还比较**父输入树与本阶段最终树**，而非与原项目比较，检查新增/修改/删除及执行位变化均在 scope。即使测试宿主直接改写副本、绕过 Native 内核边界，未批准变化仍不能 Publish/Index/Release。保留原预留和目录供 reconciliation；原实际 StopProof 并不因此变成假的停止证明。移除该最终检查的受控 mutation 会被真实 Native 测试抓到。

## 验证、故障与回退

使用 Go1.26.3，在 implementation 工作树根执行；原 runner 使用临时 HOME/XDG 和环境白名单：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/handoff-resolver-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...

PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/handoff-resolver-native.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=150s -run '^TestGLM(Factory|StageWritePaths)' \
  -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287

PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

固定真实 Claude Code 使用合成模型/key/准入。新增链验证未批准实施不启动、批准后实际 Edit、真实关闭/reopen 宿主、testing 继续实施代码、新测试生成新 revision、独立 sessions、累计预算及 review 消费者尚未实现时的启动阻断。实际硬失败/无效报告导致 needs_review、缺失验证器/标准不符在启动前拒绝，配置深复制及重启读取硬证据见 [WP-21](../work-items/WP-21/VERIFIED-TESTING-01/summary.md)。另验证 Native 越界 Edit 失败、父 header 漂移无新 intent/模型请求，以及宿主越界改码虽 Native 文本成功仍无法发布。真实供应商 key/模型/额度调用为 0；缺少固定 pin 明确 skip，不算通过。

[交付证据](../work-items/WP-20/HANDOFF-RESOLVER-01/summary.md)。本组件没有改 SQL001–010、public HTTP/OpenAPI DTO 或 UI。缺少 scope/parent/当前批准、坏 hash/来源漂移都应修正原对象或显式对账，不换 run/模型/来源绕过。回退先停止派单，核验 owned 进程、reservation、产物状态，再停用多阶段 resolver；不能回退为每阶段重新复制原项目，不回滚 schema10，不删未经核验目录，不自动 commit/merge 用户项目。
