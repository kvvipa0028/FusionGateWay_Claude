# 有限工作流与设计批准合同

本合同对应 WP-19-DESIGN-GATE-01，提供版本 1 的核心定义、私有 Store 持久记录与启动约束。后续[Management API 与 Native 通路](workflow-api.md)已接入附加、方案提交与明确批准；WP-19 仍为 in_progress，[工作流页面](workflow-ui.md)已在原 Magpie 样式任务详情区接入，实际 Native 工作流操作仍未验证。生产入口不会自动附加工作流，也不会自动推进下一阶段。三路线真实准入 Gate A 尚未通过，不能据此启用自动工程闭环。

## 固定角色与条件

| kind | 唯一允许序列 |
| --- | --- |
| investigate | design |
| review | review |
| change / bugfix | design → implementation → testing → review → acceptance |

`DefinitionFor` 返回带 schema_version=1 和内容 hash 的固定定义；未知种类、版本、角色增删和重排均拒绝。定义不授予 Runtime、网络、源码外发、发布或合并权限，不导入 pstack 规则。五角色仍从可信 `stageplan.Compile` 的冻结绑定取得模型、路线与 effort；Jev off。

`Store.AttachWorkflow` 仅由可信宿主调用，在 Task ready、generation=0、没有任何历史 run、没有该 Task 的 prepared/committed 启动记录时附加；冻结计划必须恰好包含所需角色。所有写操作都使用完整 `TaskVersion {plan_revision,generation,state}`，错误身份或过期条件拒绝。附加和设计冻结不修改 Task generation，重复相同请求只读已有记录；不同定义或方案不能替换原记录。没有附加工作流的现有手动任务保留原合同，迁移不会根据目标文本猜测工作流。

## 方案冻结与人类批准

`DesignDocument` 必须包含非空目标、范围、约束、接口及验收标准。每个列表 1–128 项，文档 JSON 最多 64 KiB，拒绝无效 UTF-8、NUL 与空文字。范围使用规范的相对 POSIX 路径，拒绝绝对路径、逃逸、重复、Windows 分隔符、控制字符和 `.git`；`.` 表示整个项目文件夹。此处只验证范围声明；实际文件访问、symlink 防护和工作副本交接仍由 Runtime/workspace 与后续 WP-20 执行，范围声明不是写权限证明。

`FreezeDesign` 深拷贝列表，分别冻结整份方案与验收标准 hash。目标须等于原 Task 目标。`SaveWorkflowDesign` 只接受同 Task 的 design run：真实状态记录为 succeeded、有启动意图和确认、租约已清除、reservation 已释放且有合法停止证明摘要。存储层消费受信 Controller/Runtime 留下的停止记录，本身不证明外部进程已退出。未释放、失败、unknown 或其他角色都不能冻结成功设计。暂停/继续只改变 Task 条件，不丢弃原已完成的设计 run；提交仍必须用当前 Task 条件。

`ApproveWorkflowDesign` 是可信人类决策操作，必须同时匹配当前 Task 条件、当前计划及原方案/验收标准 hash。模型输出、Worker grant、任意一句“继续”和普通 Task continue 均不能替代批准，也不能自动调用该方法。批准记入不可变的当前计划版本记录；修订未来阶段形成新计划后，旧批准仍保留但不授权新计划，需要重新明确批准。Management/Native 当前使用带事务内权限复核的 Authorized 入口；没有向 Worker/模型暴露批准入口，原样式任务详情的[明确批准操作](workflow-ui.md)已接入。

## 阶段启动与终止

Controller 在选择目标或准备 Runtime 前做只读预检；Store 在写启动意图的同一事务再次检查。`StartIntent`、`StartReserved`、`StartReservedOnce` 共用该约束，预检到落库之间的配置变化不能绕过约束。只有固定序列的下一角色可创建新 run，前序必须 succeeded 且停止释放完成；设计输出缺失或尚未批准都阻止 implementation。阻断不消费调用/返工预算、不写阶段意图、不启动 Runtime。既有 key 的重复回执仍只读原 run，不创建第二次执行。

| blocker | 含义 |
| --- | --- |
| design_required | 已完成 design，等待冻结完整方案 |
| approval_required | 等待当前计划的明确人类批准 |
| stage_active / stop_unverified | 前序仍活动或尚未证明停止释放 |
| stage_unverified / stage_failed | 未知或失败，版本 1 不自动重试/返工 |
| task_not_ready | Task 尚不可启动 |
| workflow_complete | 固定序列已结束，禁止再建阶段 |

工作流序列结束不会把 Task 改为 completed，也不等于验收证据完整或整项需求通过。真实测试/审查证据及有限返工分别属于 WP-21/WP-22；版本 1 不提供隐藏循环或自动权限提升。现有 HTTP 启动对 `ErrWorkflowGate` 返回 409 `workflow_requires_review`，不给出私有目标、凭据或 Runtime 信息；其他 readiness、管理鉴权和预算拒绝语义保持原样。核心组件本身未加 UI/CSS；后续 API/Native 固定路径另按工作流接口合同验证。

## 存储升级与回退

schema 9 新增 `task_workflows`、`workflow_designs`、`workflow_approvals`，附带不可变和保留触发器。001–008 不修改；009 与 `migration_009_sha256` 在同一事务安装。失败回滚到完整 schema 8，可消除故障后重新 Open；checksum 漂移或未来 schema 10 拒绝打开，不能降级覆盖。历史 Task、Plan、预算、提交回执和启动记录保留，不生成虚构批准。

运行前保留私有状态备份。回退时先停派单，等待可信进程退出并对账，然后使用已停机的 schema 8 备份和对应旧 binary；旧 binary 不支持直接打开 schema 9。不要删除新表、改 user_version 或用旧程序覆盖当前状态。当前没有迁移降级器，也没有自动工作流产品开关。

## 复现与证据

要求 Go 1.26.3。在仓库目录运行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/wp19-design-full-fusion-final.log \
  test -mod=readonly -tags fusion,nogui -race -count=1 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

测试使用隔离 HOME/XDG、真实 SQLite、真实 Controller/Scheduler 状态逻辑和合成 Runtime/额度/停止输入。真实供应商模型与额度调用为零；opt-in 原生诊断没有环境时明确 SKIP，不计通过。实际窗口、供应商生成、物理池归属、工程 Handoff 和最终 T01–T60 不由本组件证明。日志与 hash 清单见[组件证据](../work-items/WP-19/DESIGN-GATE-01/summary.md)。
