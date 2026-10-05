# 只读验收模型建议与人工接受边界

本合同在[只读 review](review-assessment.md)之后接入实际第五个 Native acceptance 阶段。UI 继续复用原 Magpie；本组件 GUI/CSS 零改动，Jev off。WP-22 整体仍为 in_progress。

## 启动与发布

宿主启动前独立核验 exact released review、原 testing receipt、可信当前 Verification Spec、已批准设计与 Acceptance 标准。实际硬证据必须 passed，review 必须有效且 approve；failed/unverified/superseded、父包漂移、缺失配置不写 Native 启动意图。复制 review 父代码到新只读工作副本，Prompt 只给冻结设计/条件、真实 testing summary 和独立 review 意见，不发验证器环境或凭据。

acceptance 使用新的 Native session、任务累计预算，不取得 writer scope。实际 Edit/Write 禁止，发布要求最终 TreeHash 等于父树。只有同一 Adapter 的实际 owned Wait/StopProof 和 succeeded Observation 可以登记输出；Native succeeded 仍只是协议事实。

## 模型意见

[acceptance.schema.json](acceptance.schema.json) 描述 version1 JSON 外形。ParseAcceptance 额外要求 UTF-8、单个 JSON、最多64 KiB、字段精确大小写且无重复、覆盖每个已批准条件一次。criteria 使用零起始 index，不允许改条件或新增 human_accepted。每项记录 met/not_met/unverified 及具体 reason；accepted 只能全部 met，rejected 必须包含 not_met。

模型逐项意见不提供硬证据。直接验证器只证明固定命令、实际代码与已冻结测试规则；没有实际 HIL/WCET/硬件证据的条件应保持 unverified，不能靠模型多数投票或建议绕过硬失败。

## 持久化与回读

私有 stage artifact receipt 新增 optional Acceptance，保存实际文本/hash、解析有效性、逐项建议，以及 exact review/test/tree/spec/design/criteria 身份。普通 artifact writer 拒绝自声明 Acceptance；HTTP/导出JSON/hash 都不构成授权。登记在事务中重复核验当前 Task/Run/parent/批准标准；与 Task 状态和事件原子提交，事件失败全部回滚，精确重试幂等。登记不释放 reservation。

有效 accepted 原子进入 advisory_only 并写 acceptance_awaiting_human；rejected/unverified/格式无效进入 needs_review 并写 acceptance_requires_review。模型没有人工接受权限，不进入 completed，不自动 merge/deploy/flash。

AcceptanceArtifact 仅消费 exact released receipt，独立恢复 acceptance/review/testing 真实包，并按当前 Spec 再评估硬证据；标准或代码变化返回 superseded。实际硬结果与模型意见分别返回，宿主重启后仍保持这一边界。

本模型组件基于 SQLite schema10，当前[人工决定组件](human-acceptance.md)升级 schema11 并保持001–010与旧 receipt canonical JSON 不变。新增字段不保证旧 binary 能读取新 receipt；回滚必须停派单并对账，恢复匹配的数据库和 artifact 备份，不能只换 binary。

## 证据与未完成项

[组件证据](../work-items/WP-22/ACCEPTANCE-DECISION-01/summary.md)区分：固定实际 Claude Code 2.1.287 + 合成上游/key/准入的五阶段链；实际直接验证器 + 合成 Native bookkeeping 的 Store fixture；离线 JSON schema 仅验证结构/hash。它们均不代表真实供应商或最终人工接受。

[持久人工决定 Store](human-acceptance.md)已接入；[人工控制 API/限定窗口/现有面板](human-acceptance.md)已接入；原 Magpie 完整主导航、最多一次自动返工及阶段首次+一次补救、项目强制独立性冲突策略、子进程工具链、真实账号/项目和 T01–T60 仍必须完成。当前不自动返工，不改写锁定模型，不缩减工作包。
