# 只读 review、真实证据与模型意见

本合同实现 design→人工批准→implementation→实际 testing→review。它不是最终 acceptance，也不改变人工批准、硬检查或 Task 完成的定义。UI 沿用原 Magpie，本次没有 GUI/CSS 修改；Jev off。

## 启动与权限

多角色 review 要求可信 Factory.Verification 的 Acceptance 与已批准设计一致。Store.StageArtifactInput 冻结完整当前 Task/Plan/Workflow 和前一 testing released receipt；Factory 用独立 source/ExecutionRoot 和当前 Spec 调用 VerifiedArtifact。failed/unverified/superseded、缺失配置或父包损坏都在 Native 启动意图前拒绝，不借用另一次相同 tree 的证据。

Prompt 提供冻结设计、实际父代码/变化和独立 testing summary：原 run ID、artifact/suite/spec/acceptance/report hash、实际版本/exit 和执行数量。不给验证器的私有 HOME/environment 或凭据。每个 review 用新 Native session，同一模型承担多个角色也不复用会话；任务预算累计。已批准项目的只读 review 没有 writer scope，原 Adapter 与 Seatbelt 禁止 Edit/Write，发布时再次要求 TreeHash 等于 testing 父树。

## 模型合同与私有登记

review 只返回 [review v1 schema](review.schema.json) 的单个 JSON：version=1、verdict 为 approve/changes_required/unverified、findings 为明确数组。每条 finding 有唯一 id、blocking/nonblocking severity 和具体 summary。approve 不可含 blocking 问题，changes_required 必须有问题。JSON 最大64 KiB、最多128条；拒绝未知字段、重复键/ID、错误大小写字段、尾随第二份 JSON 和不一致意见。schema 验结构，原始字节上限和唯一ID由 Go parser 验证。

Factory 在真实 owned Wait 与原 Adapter.VerifyStop 后读取同一 Adapter 的 Observation，核对 run/generation/session/Native 成功，从真实 Native result 文本建立私有 ArtifactRecord.Review。普通 RecordArtifact/RecordArtifactAuthorized 不接受调用者声明 Review，HTTP/模型没有登记权限。RecordReviewedArtifactAuthorized 仅用于可信 producer；调用前必须完成上述 actual Adapter 检查，其 raw 参数是模型意见，不是通用证据导入或任意 JSON 赋权入口。

私有 receipt 保存实际文本/hash、解析有效性/文档、exact testing run/tree/spec/设计/标准 hash。登记 transaction 复核 Task/Plan/Run/父 released receipt、真实硬 passed 和批准内容。要求修改、unverified 或无效报告同时写 needs_review 与 review_requires_review 事件；事件失败回滚全部变化，精确重试幂等。Native succeeded 表示进程协议成功，不会覆盖审查意见或硬检查。登记不释放 reservation。

ReviewedArtifact 只读取 exact released review receipt，独立恢复 review 包和原 testing 产物，用当前 Spec 重验硬证据。返回的硬 Verdict 与模型 Document 分开；模型 approve 不改变硬失败、不自动完结 Task，也不表示用户已经接受。review handoff 的普通 evidence 仍 unverified，模型意见在独立私有 receipt；不能把它当成另一次工程测试报告。

## 兼容、失败和边界

schema10 SQL001–010 不变，Review 是 omitempty 可选字段；旧 receipt canonical bytes 保持。旧二进制不能读取新 reviewed canonical receipt，回退必须先停止派单、确认所有 owned 进程真正停止，再恢复同一检查点的数据库与 artifact/ExecutionRoot 备份。不得删除 Review 伪装兼容或自动重放任务。

acceptance、最多一次自动返工、阶段首次+一次补救限制、项目强制独立性冲突策略及最终人工接受记录仍必须实现；当前不自动返工，不运行多角色 acceptance，不 merge/deploy/flash。需要子进程的工具链、真实账号/项目与最终 T01–T60 仍未验证。不能把本次真实工具配合合成上游的成功当作真实供应商准入。

[组件验证和复现](../work-items/WP-22/REVIEW-ASSESSMENT-01/summary.md)。
