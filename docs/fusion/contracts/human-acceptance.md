# 人工接受/退回 Store 与当前证据门

本组件接续[模型验收建议](acceptance-decision.md)。操作员的明确决定与 Native 成功、硬证据 passed、模型 accepted 分开存储。这里完成 Store 和实际固定 Native 链的消费验证；Management HTTP、Native window 与 Magpie 原界面控件尚未接入，不能将库层交付描述为可操作产品。WP-22 仍 in_progress，原 Magpie GUI/CSS 零改动，Jev off。

## 可信证据

PrepareFinalEvidence 使用宿主独立登记的 source/ExecutionRoot/当前 Verification Spec，调用 AcceptanceArtifact 恢复 exact released acceptance→review→testing，并重复绑定当前 Task 的 plan/generation。FinalEvidence 私有字段绑定原 Store，不可由 JSON、hash、模型结论或 FinalReport 构造。Report 返回深复制的展示数据，不能修改执行权；不同/重开的 Store 拒绝旧值，必须重新核验。

实际 testing passed、有效 review approve 与有效模型 accepted 是人工 accept 的必要条件。当前标准变化导致 superseded，或模型 unverified/无效，均不能人工 accept；操作员仍可 return 保留原因。Report 的 SpecHash 指原 receipt 身份，Hard 是按独立登记当前 Spec 计算的结果，两者不混为一谈。未来 API 必须从可信 Runtime 注册读取 Spec，不能收客户端的 Spec 或 passed 声明。

## 事务与决定

DecideHumanAcceptance 要求完整 TaskVersion、owned FinalEvidence、accept/return 与非空 UTF-8 原因（最多8192 bytes）。调用者为可信管理消费者，current 必须同时核验管理授权和当前验证配置身份，不能回调 Store；HTTP/模型还没有到该方法的入口。

事务内 CAS 重复核验 Task plan/generation/state、exact released receipt、完整已结束 workflow、当前批准设计与标准。实际 acceptance/review/testing 代码/header/来源证据在值生成时及事务前、提交前重复检查；文件或管理权限在最后检查时变化使全部回滚。这是提交时的证据检查，不承诺提交后外部文件永不变化。仍需独立核验后来变化，历史人工决定不会使新代码或新标准自动通过。

accept 只从 advisory_only 将 Task 置 completed，写 human_accepted；return 保持/进入 needs_review，写 human_returned。两者和不可变 human receipt 原子提交，事件失败无部分记录。实际 StageRun/Native succeeded、StopProof、预算计数不改，不启动 Runtime、返工、merge/deploy/flash。同任务条件并发只能一个决定提交；精确重试需重读最新 Task 条件，不增加事件，其他 action/reason 冲突。

HumanDecision 只回读历史操作员事实，保存 version、Task/plan/generation、run/text/tree/spec/design/criteria hash、action/reason、固定 management authority 与 UTC 时间。个人本机管理凭据代表获授权操作员，authority 标签不证明多人身份或企业审计。后续 API/NativeUI 必须先核对该请求的身份和项目范围，再展示和消费，不将普通模型 Worker capability 当作管理权限。

## schema11 与回退

011.sql 新增 human_acceptance_decisions、索引和插入/更新/删除保护，迁移 checksum 随表/version 原子提交。001–010 与旧 artifact canonical bytes 不改。schema10 升级保留实际模型 receipt 与历史 Task，不虚构人工批准；故障全部回滚至10，修复后可重新打开。任意 migration checksum 漂移或 future12 拒绝打开，不自动改写版本。

升级前停止派单、确认 owned Worker/验证器实际停止、reservation 对账，并关闭宿主；保留同一检查点的完整私有 stateRoot 与原 executionRoot 备份。旧 binary 拒绝 schema11；回退只能恢复匹配的旧数据库/artifact 备份，备份之后的新记录不会保留。不得删除表、降低 user_version、伪造 checksum、重放任务来兼容旧 binary。本组件没有自动 downgrade 或数据搬迁。

## 验证和未完成

[组件证据](../work-items/WP-22/HUMAN-DECISION-STORE-01/summary.md)区分：实际固定 Claude Code2.1.287+合成上游/key/准入链直接调用可信 Store 人工决定；实际验证器+合成 Native bookkeeping 的 Store fixture；离线 schema/hash 检查。它们不代表真实供应商准入、真实 GUI 点击或 HIL/WCET/硬件通过。

接下来仍须实现 Management API 当前配置读者、限定 Native 窗口的调用、Magpie 原界面最少控件、有限返工/次数约束/独立性策略、子进程工具链、真实账号/项目与 T01–T60。保留完整工作包要求，不将本组件通过等同整个工程闭环完成。
