# WP-22-REVIEW-ASSESSMENT-01 · 真实证据的只读审查与模型意见

本组件完成，BASE `e180b4f2c739f58dc719d78569a2a337db3ca6d0`；WP-22 整体 in_progress，完整目标不缩减。原 Magpie GUI/CSS 零修改，Jev off；真实供应商调用为0。真实固定 Claude Code 2.1.287 与合成上游/key/准入用于本机链验证。

## 行为与依据

实际 GLM design→人工批准→implementation→testing→review 已接通，review 从 exact released testing 产物创建新只读工作副本。启动前用独立 source/ExecutionRoot 与当前可信 Spec/已批准 Acceptance 核验硬证据；缺失、failed/unverified/superseded 或父包漂移不写 Native 启动意图。Prompt 给出冻结设计、父代码/变化与真实版本/exit/执行数量/hash；不发私有验证器环境或凭据。

review 仅给出严格64 KiB version1 JSON advisory verdict/findings，拒绝未知/错误大小写/重复字段、尾随JSON、重复ID和矛盾结论。approve 不含 blocking，changes_required 必须保留具体问题。Adapter 实际 owned Wait/StopProof 后读取同一 Native session 的 Observation，私有不可变 Review receipt 分开保存真实文本/hash、模型意见和所依据的 testing/tree/spec/设计/标准身份。普通 artifact 写入拒绝自声明 Review；HTTP/模型不能自行赋权。

真正的 Native Edit 越界被拒绝，最终发布再次要求代码树不变。要求修改、unverified/格式无效意见原子写 Task needs_review 和独立事件；事件失败全部回滚，精确重试幂等。Native succeeded 只表示协议成功，不覆盖意见或硬结果。注册不释放 reservation；只有实际 released run 可回读。

ReviewedArtifact 重启后恢复 review/原 testing 包并重新核验当前硬证据。模型 approve、测试 passed 和人工已批准设计分别记录；模型没有最终人工接受权限，不自动完结 Task/派单/merge/deploy/flash。同模型允许多角色，每个实际阶段独立 Native session，预算按任务累计。

## 验证

Go1.26.3：Fusion race 689个顶层、1244个子测试 PASS；33个顶层、11个子测试 SKIP，19 packages，0 FAIL。未提供 Native pin 的 fixture 明确 skip，不算 Native 通过。

显式固定 Native 的完整 Factory 回归10个顶层、47个子测试 PASS，0 SKIP/FAIL。15种实际链场景覆盖正常与重启、测试和review越界写、父 header 漂移、宿主越界修改、真实 exit7/坏报告、缺失验证器、错误标准、配置深复制、review blocking/坏JSON、review标准变化/缺少当前配置/父包损坏；budget 从5累计到6，review不是 writer，最终 acceptance 仍阻断。

Store targeted3个顶层、6个子测试 PASS，验证 held回读阻断、独立意见/硬结果、重启和标准变更、伪造普通写入拒绝、错父/树/输入/current最后撤销不留副作用、事件失败回滚与精确重试。parser实际负向用例及全部 workflow tests 通过。CLI/GUI build 和全仓 go vet exit0；UI实际点击未验证。

- 缺失 parser 的编译 RED 保留。
- 真实三阶段链在修复前启动review返回503，随后第四阶段实际202/停止/释放/登记通过；不是只用编译失败证明行为。
- 有效 mutation 移除 current hard passed 条件后，标准已变化仍返回202/产生新generation，负向用例FAIL；生产bytes精确恢复，最终完整Native/Fusion回归通过。
- captured receipt 来自 Store fixture：实际只读验证器进程，Native bookkeeping/文本为合成数据。真实 Native 来源边界另由上述实际阶段链验证。离线schema验证实际文本hash、独立意见/硬结果/人工设计批准及7个无效文档，不证明Native来源或最终验收权限。

详细证据：[test-results.json](test-results.json)、[schema-results.json](schema-results.json)、[artifacts.json](artifacts.json)。[合同](../../../contracts/review-assessment.md)。

## 复现

在 implementation 根，现有 Go1.26.3/Python jsonschema，固定 executable 不可换成未准入版本：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/review-native-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=180s -run '^TestGLM(Factory|StageWritePaths)' -v \
  ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287

PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/review-fusion-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...

PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
python3 docs/fusion/work-items/WP-22/REVIEW-ASSESSMENT-01/check-schema.py
```

Store targeted 可用 `-fusion-review-capture /absolute/private/path.json` 导出合成receipt，仅给 Store package测试，不能用于生产导入。不覆盖已归档证据，不传真实key。

## 继续要求和回退

最终 acceptance/人工接受、有界返工/首次+一次补救限制、项目强制独立性策略、原 Magpie UI 状态消费、受控子进程工具链、真实账号/项目 Gate A、Native UI 点击与最终 T01–T60 均须完成。当前多角色 acceptance unsupported，不用四个绿色阶段代替五阶段验收。

schema10 SQL001–010 未改，Review optional omitempty 保留旧canonical receipt。旧二进制不能安全读取新 reviewed receipt；回退必须停止派单、确认所有 owned 进程真正停止，并恢复同一检查点的数据库/artifact/ExecutionRoot与对应二进制，不删字段伪装兼容，不重放任务。
