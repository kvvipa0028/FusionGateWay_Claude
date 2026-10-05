# WP-22-ACCEPTANCE-DECISION-01 · 五阶段只读验收模型建议

本组件完成，BASE `666e1ad13f2ced6bae5f2764fee0f7e51671c456`；WP-22 整体仍 in_progress。原 Magpie GUI/CSS 零修改，Jev off，真实供应商调用0。以下实际 Claude Code 指固定2.1.287 executable、合成上游/key/准入，并非真实供应商准入。

## 行为

实际 design→人工批准→implementation→testing→review→acceptance 已接通。acceptance 在新只读副本和独立 Native session 运行，任务预算从6累计到7。启动前用宿主独立 source/ExecutionRoot 与可信当前 Spec/已批准标准核验 exact released review/testing；硬证据 failed/unverified/superseded、review 非 approve、父包漂移或配置缺失不产生 Native intent。

模型返回严格64 KiB version1 JSON，按零起始 index 完整覆盖已批准条件一次。未知/大小写错误/重复字段、尾随 JSON、null/重复/越界 index、缺条件、矛盾结论均拒绝。accepted 必须全 met，rejected 至少一项 not_met。模型意见不改变硬测试、冻结标准或人工权限；无 HIL/WCET/硬件依据的条件保持 unverified。

实际 owned Wait/StopProof 之后读取同一 Adapter/session 的 Observation，私有不可变 Acceptance receipt 分开保存实际文本/hash、解析有效性、逐项建议和 exact review/test/tree/spec/design/criteria 身份。普通 writer 拒绝自声明 Acceptance。登记在事务中重检并原子更新状态/事件，事件失败回滚，重试幂等，不自行释放 reservation。

模型 accepted 原子进入 advisory_only/acceptance_awaiting_human，等待人工接受；rejected/unverified/坏格式进入 needs_review。Native succeeded 仅为协议事实；没有 completed 或自动 merge/deploy/flash。实际 Edit 被只读权限拒绝，发布要求代码树不变。AcceptanceArtifact 仅回读实际 released receipt，重启恢复真实父链并重新核验当前硬证据；标准变更返回 superseded。

SQLite schema10、SQL/API/Controller/runtime/policy 与 handoff schema 未改；optional Acceptance 保留旧 canonical receipt，但不保证旧 binary 读取新字段。回滚须恢复匹配数据库/artifact 备份。当前审计使用存在的 `docs/fusion/openapi-fusion.yaml`；历史 packet 的来源/hash 不改写。

## 验证

Go1.26.3 Fusion race：693个顶层、1253个子测试 PASS；33个顶层、11个子测试 SKIP，19 packages，0 FAIL。缺少 Native pin 的 fixture skip 不计真实 Native 通过。

显式固定 Native 完整 Factory 回归：10个顶层、54个子测试 PASS，0 SKIP/FAIL。其中22种实际阶段链场景覆盖正常五阶段与重启、测试和审查失败、验收 rejected/unverified/坏JSON/缺条件、实际越界 Edit、当前标准变化与父包损坏；不放行失败或过期证据。同模型多角色使用不同 session，没有自动换模型。

Store targeted：3个顶层、9个子测试 PASS，覆盖 held回读阻断、模型/硬证据/人工权限分离、重启与标准变化、普通JSON伪造拒绝、错父/树/输入/role/current最后撤销、事件回滚、独立显式状态预期和精确重试。captured Store fixture 使用实际直接验证器，Native bookkeeping/文本为合成数据；实际 Native 来源另由上述阶段链验证。

实际行为 RED 为四阶段后 acceptance 返回503；集成后第五阶段实际202/停止/释放/receipt成功。有效 mutation 移除当前硬证据通过条件后，标准变化仍产生202/新generation，负向测试FAIL；生产bytes精确恢复，最终完整回归通过。初次 mutation regex 无匹配和相对 capture path 错误均保留，不算验证成功。

CLI/GUI build 与全仓 go vet exit0。离线 schema/text hash 检查不证明 Native 来源或人工接受；UI 实际点击未验证。

证据：[test-results.json](test-results.json)、[schema-results.json](schema-results.json)、[artifacts.json](artifacts.json)。[实现合同](../../../contracts/acceptance-decision.md)。

## 复现

在 implementation 根，现有 Go1.26.3/Python jsonschema 与固定 executable：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/acceptance-native-final.log test -mod=readonly \
  -tags fusion,nogui -race -count=1 -timeout=240s \
  -run '^TestGLM(Factory|StageWritePaths)' -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/acceptance-fusion-final.log test -mod=readonly \
  -tags fusion,nogui -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...
python3 docs/fusion/work-items/WP-22/ACCEPTANCE-DECISION-01/check-schema.py
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

## 未完成

人工最终 accept/return API与记录、原 Magpie 主界面消费、有限返工/阶段次数限制、项目强制独立性策略、受控子进程工具链、真实账号/项目与最终 T01–T60 仍须完成，不将本组件通过等同 WP-22 或整个任务完成。没有真实 HIL/WCET/硬件验证。UI 沿用原 Magpie，只做必要扩展。
