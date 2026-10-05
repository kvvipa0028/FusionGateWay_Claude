# 项目角色模型独立性

默认允许一个模型承担多个角色，实际 Native 阶段仍使用独立 session 与证据。项目若有硬约束，可在操作者私有 projects.json 的单个项目内声明 independence；任务请求、Layer/预设和计划修订不能新增或削弱这项项目规则。

```json
"independence": [
  {"first": "design", "second": "acceptance"},
  {"first": "implementation", "second": "review"}
]
```

这只是项目对象中的可选字段，不是可独立加载的完整配置。省略表示没有模型独立性硬约束；配置来源仍遵循[私有登记合同](local-project-source.md)。不改动当前真实私有配置，不填写 key。

## 语义

每对角色使用不同的、可信 Registry 冻结的 ResolvedModel。改变账号、路线、凭据标识或 effort 不能把同一模型算作独立；不猜测模型族、别名或不同版本是否等价。真实模型身份仍必须由准入证据验证。模型不同与 session 独立分别判断；模型相同默认合法时也不复用上一角色的会话。

角色对最多10个，first/second 必须是五角色中的不同项。反向重复、自身配对和未知角色拒绝；服务端按固定角色顺序 canonical 排列，复制并冻结到 Snapshot 的可选 independence 字段，纳入原 plan hash。无规则时字段省略，旧任务的序列化/hash 保持。单角色任务保留项目规则但不虚构缺失角色或新模型。

Preview 对两边都已存在、但没有任何不同模型组合的绑定返回422 independence_conflict。锁定冲突直接拒绝。自动阶段候选存在可行组合时保留全部原批准候选，不偷偷过滤或替换；具体选中模型与对方冻结候选及任何历史 intent 冲突时拒绝。多个约束的当前选择必须全部满足；不提供额外模型、不提高权限、不使用多数投票替代硬约束。

## 实际派单阻断

Controller 在 Runtime Resolve、工作区和凭据准备前，按完整 TaskVersion 和当前管理/lifetime 权限核对具体 frozen Target。Scheduler 在真实 Inspection 前核对，Store 在原子启动意图事务内重复核对，避免 preflight 后的并发变化绕过。受约束对方任意历史 intent 的模型都参与比较，包括旧计划版本、失败/中断/补救记录；重启、新 key、改账号和计费渠道不清除历史。

违反规则返回409 project_independence_conflict，保留原 task/事件/预算/产物，不写新的 run/reservation/key。合法的已批准不同模型选择仍走原准入与停止核验，不因满足独立性就获得执行权。精确旧 key 只读原 receipt，不能重放。规则与任务冻结，当前计划修订必须原样保留，不能改 hash 后删除。更改私有来源仍触发原 Current 失效与明确重载，不在运行中自动采用新规则或换模型。

## 接口与兼容

ProjectConfiguration 与 Snapshot 增加可选 independence 数组。公开 PreviewRequest/RevisionRequest/Layer/预设输入没有该字段，夹带覆盖请求返回400。结构见 [OpenAPI](../openapi-fusion.yaml) 的 RolePair/Independence 与 Snapshot。JSON schema 只检验结构，不证明真实模型准入或独立性执行。

数据库保持schema12，原SQL001–012无改动。旧二进制不认识新字段，会无法验证带规则的新 plan hash；不得忽略字段强行运行。回退先停派单并等待真实进程停止，使用匹配旧 binary 的停机完整状态/产物备份；没有规则的旧快照兼容性保留。不提供 downgrade 或删历史绕过。

## 验证与复现

使用 Go1.26.3，在仓库根运行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/independence-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/independence-native-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=1 -run '^TestGLM(Factory|StageWritePaths)' \
  -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
```

[组件证据](../work-items/WP-22/PROJECT-INDEPENDENCE-01/summary.md)含原行为RED、规则冻结/深复制、不同账号的同模型拒绝、可行自动选择、历史/重启、修订不能删规则、Runtime/Inspection前阻断及变异验证。实际固定Native默认同模型多角色仍独立session；项目硬约束冲突在真实产品预览路径阻断，未启动模型。供应商/准入输入为合成，不宣称真实不同供应商模型端到端。

UI沿用原Magpie，本组件没有修改UI/CSS或引入新框架。完整原主导航、子进程测试工具链、真实供应商Gate A、实际Native点击和最终60类验收仍待完成，WP-22继续in_progress；Jev off。
