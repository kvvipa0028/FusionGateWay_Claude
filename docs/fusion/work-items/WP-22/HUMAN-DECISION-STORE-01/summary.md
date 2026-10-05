# WP-22-HUMAN-DECISION-STORE-01 · 当前证据的人工接受/退回存储

本组件完成，BASE `875771c073fb0ee9289778444e31ebe7498b6cc6`。这里只交付 Store 人工决定与实际固定 Native 链的消费验证；Management HTTP、Native window 和 Magpie 控件尚未接入，不是可操作产品完成。WP-22 仍 in_progress；原 Magpie GUI/CSS 零改动，Jev off，真实供应商调用0。

## 行为

PrepareFinalEvidence 从独立登记 source/ExecutionRoot/当前 Spec 消费 exact released acceptance→review→testing，产出绑定原 Store 的私有 owned 值。展示 Report 深复制，不授予权限。JSON、模型 passed/hash、零值、已关闭或不同 Store 的值不能构成人工接受。

DecideHumanAcceptance 要求完整 TaskVersion、accept/return、非空原因和可信管理/配置 current。事务中重复核验当前 Task/plan/generation、精确 released receipt、当前批准设计/标准与完整结束的流程；三份真实代码/header/来源在生成证据、事务前与提交前重复核验。权限、文件或条件变化都不留部分记录。提交后的外部变化仍须重新核验，历史人工事实不是新的工程通过证明。

人工 accept 要求当前硬证据 passed 与有效模型 accepted，从 advisory_only 将 Task 置 completed；return 保持/进入 needs_review，保留原因，不能自动返工。新不可变表、状态和 human_accepted/human_returned 事件原子提交，失败全部回滚；完整条件并发只有一项决定，重读条件后的精确重试不重复事件，不可更换 action/reason。Native succeeded/StopProof/预算和实际调用数量不改变。

schema11 以011.sql新增人工决定表/索引/保护及checksum；001–010不变。schema10升级保留实际模型 receipt 与历史任务，不虚构人工批准；故障回滚至10，修复后可重试。checksum 漂移和未来12拒绝。旧 binary 不支持schema11；回退须停派单、确认实际停止与对账、恢复同一检查点的 stateRoot/原 executionRoot 备份，不删表或伪造版本/checksum。

## 验证

Go1.26.3 Fusion race：700个顶层、1265个子测试 PASS；33个顶层、11个子测试 SKIP，19 packages，0 FAIL。没有 Native pin 的 fixture skip 不算实际 Native 通过。

完整固定 Native Factory：10个顶层、54个子测试 PASS，0 SKIP/FAIL，含22种实际五阶段链场景。正常与配置复制场景在 actual released 第五阶段后，由测试代码显式调用 Store accept、观察 completed 与宿主重启后独立人工记录；rejected/unverified/坏JSON/缺条件场景显式 return。全部不增加 Native 调用或预算，Actual Native 来源与人工测试动作分开记录；不是用户真实点击。

Store targeted：7个顶层、12个子测试 PASS，覆盖伪造/展示副本、模型未知/当前标准变化拒绝accept但允许return、整个TaskVersion CAS、最终权限撤销/事件回滚、header/来源漂移、最后提交检查、非法action/空原因、并发、幂等、不可变表与重启、真实旧receipt迁移/迁移故障重试/checksum/future12。

缺少 Store APIs 的 compile RED 保留；初次测试不可比较StageRun与Native缺少reflect导入均按实际类型修复，未放宽断言。有效 mutation 去掉 current hard passed 条件后，过期标准仍被人工accept，负向测试FAIL；生产bytes精确恢复，最终完整回归通过。

CLI/GUI build exit0。首次全仓 go vet 报两处新 Native 测试的跨包位置字段literal；改为语义相同的具名字段，独立全仓 vet exit0。已通过且不受影响的CLI/GUI构建保留，不重复执行；原失败与最终日志均归档。离线 schema 的日期检查使用显式 stdlib UTC checker，避免 jsonschema 缺少可选 RFC3339 依赖时静默放行；9个无效文档实际拒绝。schema 仅验证历史决定外形与模型/硬证据/人工事实分离，不能证明 Native 来源或真实用户操作。

证据：[test-results.json](test-results.json)、[schema-results.json](schema-results.json)、[build-results.json](build-results.json)、[artifacts.json](artifacts.json)。[合同](../../../contracts/human-acceptance.md)。

## 复现

在 implementation 根、已安装 Go1.26.3/Python jsonschema 与固定 executable：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/implementation/human-store-final.log test -mod=readonly \
 -tags fusion,nogui -race -count=1 -timeout=180s -run '^TestHuman' -v ./internal/fusion/store
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/implementation/human-native-final.log test -mod=readonly \
 -tags fusion,nogui -race -count=1 -timeout=240s -run '^TestGLM(Factory|StageWritePaths)' \
 -v ./internal/fusion/bootstrap -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/implementation/human-fusion-final.log test -mod=readonly \
 -tags fusion,nogui -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...
python3 docs/fusion/work-items/WP-22/HUMAN-DECISION-STORE-01/check-schema.py
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

## 仍须完成

可信 Runtime 当前配置读者与 Management accept/return API、限定 Native window、原 Magpie 主界面最少控件、有界返工/次数限制/独立性、子进程工具链、真实账号/项目及最终 T01–T60。实际 UI 点击、真实 HIL/WCET/硬件均未验证，不将本组件等同整个工作包或完整目标完成。
