# WP-20-HANDOFF-RESOLVER-01

基线 `dae84761640f1cebbcc4bdfe10d19586e7b02913`。本组件完成 GLM design→人工批准→implementation→testing 的真实代码消费者；WP-20 和完整工程闭环继续 in_progress。UI 沿用原 Magpie 的约束不变，本组件没有修改任何 GUI/CSS。

## 行为与交付

- Store.StageArtifactInput 在完整 ready Task condition 下核验工作流顺序、冻结设计/批准和精确前角色 released artifact receipt。后续阶段恢复并复制父代码，不再复制原项目丢失实施修改；宿主重启后依然可以交接。
- Prompt 保留完整冻结设计/批准和父产物身份、变化、advisory evidence。当前阶段使用冻结目标，独立 Native session，不继承账号认证或上一阶段会话。
- implementation 使用批准的 Scope，testing 使用可信 Factory.TestingWritePaths 与 Scope 的交集。缺少测试范围、非法范围或空交集拒绝，不推断路径、不开放整个项目。
- 内部 Spec.WritePaths 逐层深复制，readonly/非法值在 launch 前拒绝。实际 macOS Seatbelt 限定写范围，停止后再次检查父树到最终树的变化；越界不能发布、索引或释放。
- child receipt 精确绑定 ParentRunID/InputTreeHash；父 header/source/context 漂移拒绝后续启动。SQL001–010、HTTP/OpenAPI、handoff Document schema 和 UI 均未改变。

范围、配置、复现命令与回退见 [GLM 阶段交接合同](../../../contracts/glm-stage-handoff.md)和 [Factory 合同](../../../contracts/glm-execution-factory.md)。新 TestingWritePaths 只在可信 Go 配置中登记，不能由 HTTP、模型或项目 JSON 赋权。独立 implementation 的 nil 内部范围保留既有合同，可写 testing 必须明确登记测试路径。

## 验证

Go1.26.3，环境白名单和临时 HOME/XDG。最终 Fusion race 回归：670 个顶层、1223 个子测试 PASS，33 个顶层、11 个子测试 SKIP，18 packages，0 FAIL。跳过项包括显式 opt-in Native/helper 和 APFS 不支持的非法 UTF-8 文件名 fixture，不计为通过。

显式固定 Claude Code 2.1.287 的最终测试：10 个顶层、36 个子测试 PASS，0 SKIP/FAIL，18 次实际受管理 Claude 进程执行、26 次合成上游请求。新增四个真实消费者场景验证成功链/重启、testing 越界 Edit、父 header 漂移，以及宿主绕过 Native 内核直接越界改码。原项目保持原内容，后续副本保留实施和测试文件，累计预算与独立 sessions 可回读。真实私有 key、供应商模型和 quota 调用均为 0；合成 Registry/Inspector 不能证明真实供应商准入。

真实 kernel 正控制写 tests 成功，负控制写 src/tests2/根文件被拒绝。Native 越界 Edit 失败进入 needs_review，不发布成功 artifact。宿主越界改码场景中 Native 的真实停止已确认，但最终变化核验失败，Controller 不验证发布/释放，reservation 保持 held，供显式 reconciliation。

CLI、GUI 编译与全仓 go vet 均 exit 0，当前二进制 hash 与构建报告一致。详细统计、原始与归档日志 hash、初始失败分类见 [test-results.json](test-results.json)；[artifacts.json](artifacts.json)记录本组件文件 hash 和 scope 检查。

## 初始失败与有效性

缺少新 API 的编译 RED、实际 worker 越界 exit92 和旧单阶段限制导致设计启动503均已保留。初次实现的新链通过，但旧独立 testing fixture 未登记新的显式测试路径；补上可信 tests 配置，保留原行为断言。最终范围 mutation 暂时放宽到 `.`，真实宿主越界场景检测到错误发布/释放并 FAIL；精确恢复生产代码后最终全部通过。第一次 mutation 仅触发 unused variable 编译失败，明确不计为有效行为证据。

## 未完成边界

testing 当前生成与交接测试产物，尚未执行真实 TestExecutor/EvidenceGate。Native 成功、artifact receipt 和 advisory evidence 均不能证明测试通过或验收完成。多角色 review/acceptance 保持 unsupported，硬证据、审查验收、有限返工、真实供应商 Gate A、原 Magpie 主界面整合、实际 Native UI 点击与最终 T01–T60 仍必须完成；本组件不缩减这些需求。Jev off，不自动提交或合入用户项目代码。
