# 一次受控返工策略

已批准 change/bugfix 在显式任务预算 MaxReworks=1 下，可由一次当前有效 changes_required 审查触发补救。原固定 Definition/计划/模型绑定不变；内部序列为 design → implementation → testing → review → implementation → testing → review → acceptance。普通路径仍逐阶段明确启动，只有受控补救尾段自动继续。

## 当前证据与权限

Store.PrepareReworkEvidence 从独立登记 source、executionRoot 和可信当前 Spec 核验原 released review 与 testing 包，要求真实硬检查 passed、结构化审查有效且要求修改。ReworkEvidence 保留私有 Store ownership 与实际文件 guard，JSON、模型文字和公开 hash 不能创建该权限。旧测试标准、文件/父包漂移、approve 或无效意见不能触发返工。

BeginReworkAuthorized 使用完整 TaskVersion CAS，并在事务前后重核当前权限。只接受原第一轮 review、精确原四个阶段、当前批准方案和计划；需要 MaxReworks=1、UsedReworks=0 且调用预算尚未耗尽。记录、计数、ready 状态和事件原子提交；撤销、并发过期、事件失败不留半个返工。MaxReworks=0 保持 needs_review。人工退回不会由此生成新的返工轮。

## 实际执行

可信 GLM Factory 在原实际 Wait/StopProof/Release 后启动受控继续。补救 implementation 接收原 review 的 exact run/text hash 和完整 findings，仅能写原批准 Scope。testing 使用原冻结标准执行新代码树；review/acceptance 只读，逐次新建独立 Native session，仍使用原角色的冻结模型、账号、路线和 effort。最新 released 产物树改变后，历史 testing 的执行记录保留，但不能充当当前树的 passed 证据。

继续仍经过原 Controller 当前来源、路线/准入、Scheduler、Permit 和 writer 预留。每个 task/role 最多两次 intent，原预算按 task 累计，所有补救模型请求计入调用预算；不以改阶段名称、重启或新 key 重置。最多一个自动回合。二次审查仍有问题、复测硬失败、预算耗尽或权限撤销后保留现场并停在 needs_review。模型 accepted 仍只是 advisory_only；最后由[人工明确接受](human-acceptance.md)完成，不自动 merge/deploy/flash。

Controller 继续任务使用原 lifetime 和受跟踪 WaitGroup，容量1可推进。Close 会取消并等待 callback/实际运行；未证实停止释放时不能调用继续 hook。中断后的 ready receipt 不在重启时重放，可明确核对后启动合法下一阶段。后台没有扫描式派单循环。

## schema12 与回退

012.sql 新增不可变 workflow_reworks 及插入核验、禁止 update/delete 的 triggers；原001–011不变，migration_012_sha256 与迁移同一事务安装。公开五角色 Definition hash 不变，八阶段序列从受保护 receipt、实际 released review 和当前预算派生。升级失败完整保留 schema11；checksum 漂移与未来 schema13 拒绝。已有真实模型/人工接受记录不重写，不给旧任务凭空补轮次。

升级前停止派单、等待 owned 进程实际停止并关闭宿主，备份完整私有 stateRoot 和 executionRoot。回退使用匹配旧 binary 的停机备份，并明确接受备份后记录不会随旧备份恢复；不能删除返工表或修改 user_version/checksum。详情见[持久产物与回退](durable-stage-artifact.md)。

## 复现与证据边界

使用 Go1.26.3，仓库根执行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/rework-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/rework-native-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=1 -run '^TestGLM(Factory|StageWritePaths)' \
  -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
```

[组件证据](../work-items/WP-22/REWORK-LOOP-01/summary.md)区分真实 Native 进程/代码/测试执行与合成供应商/准入输入。未带 pin 的 opt-in Native 测试明确 skip，不能计为真实验证。7个新增场景中的 rework_cancel 是生成继续记录后撤销管理权限；Controller Close 生命周期另有单元验证，不冒充实际 Native UI 取消。项目强制独立性策略、真实供应商 Gate A、子进程工具链、原主导航完整整合和最终60类验收仍待完成。UI 沿用 Magpie 原布局/组件/变量/交互，本工作项无 UI/CSS 修改，Jev off。
