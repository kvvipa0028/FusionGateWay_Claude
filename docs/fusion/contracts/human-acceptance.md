# 人工接受/退回 Store 与当前证据门

本组件接续[模型验收建议](acceptance-decision.md)。操作员的明确决定与 Native 成功、硬证据 passed、模型 accepted 分开存储。Store、可信 Runtime 当前配置读者、Management HTTP 和限定 Native window 已接入；现有 Fusion 工作流面板增加人工验收控件，复用 Magpie 原有类名和样式。原主界面及全部 CSS 不变，完整主导航接入仍待完成。WP-22 仍 in_progress，Jev off。

## 可信证据

PrepareFinalEvidence 使用宿主独立登记的 source/ExecutionRoot/当前 Verification Spec，调用 AcceptanceArtifact 恢复 exact released acceptance→review→testing，并重复绑定当前 Task 的 plan/generation。FinalEvidence 私有字段绑定原 Store，不可由 JSON、hash、模型结论或 FinalReport 构造。Report 返回深复制的展示数据，不能修改执行权；不同/重开的 Store 拒绝旧值，必须重新核验。

实际 testing passed、有效 review approve 与有效模型 accepted 是人工 accept 的必要条件。当前标准变化导致 superseded，或模型 unverified/无效，均不能人工 accept；操作员仍可 return 保留原因。Report 的 SpecHash 指原 receipt 身份，Hard 是按独立登记当前 Spec 计算的结果，两者不混为一谈。当前 API 从可信 Runtime 注册读取 Spec，拒绝客户端 Spec 或 passed 声明。

## 事务与决定

DecideHumanAcceptance 要求完整 TaskVersion、owned FinalEvidence、accept/return 与非空 UTF-8 原因（最多8192 bytes）。调用者为可信管理消费者，current 必须同时核验管理授权和当前验证配置身份，不能回调 Store；HTTP 入口仅允许当前管理请求，模型 Worker capability 无此入口。

事务内 CAS 重复核验 Task plan/generation/state、exact released receipt、完整已结束 workflow、当前批准设计与标准。实际 acceptance/review/testing 代码/header/来源证据在值生成时及事务前、提交前重复检查；文件或管理权限在最后检查时变化使全部回滚。这是提交时的证据检查，不承诺提交后外部文件永不变化。仍需独立核验后来变化，历史人工决定不会使新代码或新标准自动通过。

accept 只从 advisory_only 将 Task 置 completed，写 human_accepted；return 保持/进入 needs_review，写 human_returned。两者和不可变 human receipt 原子提交，事件失败无部分记录。实际 StageRun/Native succeeded、StopProof、预算计数不改，不启动 Runtime、返工、merge/deploy/flash。同任务条件并发只能一个决定提交；精确重试需重读最新 Task 条件，不增加事件，其他 action/reason 冲突。

HumanDecision 只回读历史操作员事实，保存 version、Task/plan/generation、run/text/tree/spec/design/criteria hash、action/reason、固定 management authority 与 UTC 时间。个人本机管理凭据代表获授权操作员，authority 标签不证明多人身份或企业审计。当前 API/NativeUI 先核对该请求的身份和项目范围，再展示和消费，不将普通模型 Worker capability 当作管理权限。

## schema11 与回退

011.sql 新增 human_acceptance_decisions、索引和插入/更新/删除保护，迁移 checksum 随表/version 原子提交。001–010 与旧 artifact canonical bytes 不改。schema10 升级保留实际模型 receipt 与历史 Task，不虚构人工批准；故障全部回滚至10，修复后可重新打开。任意 migration checksum 漂移或 future12 拒绝打开，不自动改写版本。

升级前停止派单、确认 owned Worker/验证器实际停止、reservation 对账，并关闭宿主；保留同一检查点的完整私有 stateRoot 与原 executionRoot 备份。旧 binary 拒绝 schema11；回退只能恢复匹配的旧数据库/artifact 备份，备份之后的新记录不会保留。不得删除表、降低 user_version、伪造 checksum、重放任务来兼容旧 binary。本组件没有自动 downgrade 或数据搬迁。

## 验证和未完成

[组件证据](../work-items/WP-22/HUMAN-DECISION-STORE-01/summary.md)区分：实际固定 Claude Code2.1.287+合成上游/key/准入链直接调用可信 Store 人工决定；实际验证器+合成 Native bookkeeping 的 Store fixture；离线 schema/hash 检查。它们不代表真实供应商准入、真实 GUI 点击或 HIL/WCET/硬件通过。

[人工控制产品消费证据](../work-items/WP-22/HUMAN-CONTROL-01/summary.md)另含真实 Native bridge API、浏览器交互与截图；浏览器正向展示使用明确标注的 presentation fixture，不伪装成真实人操作实际模型产物。接下来仍须实现原 Magpie 完整主导航接入、有限返工/次数约束/独立性策略、子进程工具链、真实账号/项目与 T01–T60。保留完整工作包要求，不将本组件通过等同整个工程闭环完成。

## 管理 API 与界面操作

`GET /control/v1/tasks/{task_id}/workflow/decision` 返回当前完整 Task/ETag、展示 report、单独的历史 human decision 和 unavailable code。未到最终阶段或未登记独立验证读者时 report 为 null，不构造通过结果。现有 Runtime reader 使用冻结的当前验证配置、精确路线/账号/凭据身份和宿主来源；历史决定不能覆盖当前 hard 状态。

`POST` 同一路径要求当前完整 `If-Match`，只收 `run_id/text_hash/tree_hash/spec_hash/design_hash/acceptance_hash/action/reason` 八个字段。前六项必须精确匹配本次独立读取；action 为 accept/return，reason 非空且最多8192 UTF-8 bytes。不收客户端 verifier/source/root、角色、passed 或 Idempotency-Key。坏请求400，无当前管理授权401/403，无 Task404，证据门/决定冲突409，旧条件412，缺条件428，可信读者失效503；HTTP DTO 检查不替代 Store owned 门。

管理权限在等待 Server 锁后、Store 事务前/提交前及响应前重查。读者 current 只检查当前独立配置与来源，不能回调 Store/Server；三份实际产物由 owned proof 单独重复核验。条件或凭据失效拒绝成功响应；若事务已经提交后才出现外部变化，历史决定仍是提交事实，客户端必须重新读取核对，不能据响应丢失自动提交新动作。

Native bridge 仅给绑定的实际窗口和已选登记项目开放这一精确 GET/POST 路径，沿用同源、当前私有 source、管理凭据隔离和响应后二次校验。服务端私有管理凭据不传给页面或模型。

在原有“工作流与设计检查点”折叠面板中点击“读取验收证据”，分别审阅真实测试结果与冻结标准的逐项模型意见，填写决定原因，再明确确认“接受当前交付”或“退回当前交付”。过期/失败/未验证证据禁用接受；退回只保存原因并进入 needs_review，不启动返工。响应丢失、身份/条件不匹配均停用决定，必须手动回读，不自动重试。切换任务或重新读取会清除旧证据；模型文本用 textContent，不能执行 HTML。控件复用 Magpie list/row/profiles/text/action/primary 和原 CSS，不新增框架或重新布局。
