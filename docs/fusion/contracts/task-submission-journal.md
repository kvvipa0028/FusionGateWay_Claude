# 窗口恢复所需的私有提交记录

组件 WP-15-SUBMISSION-JOURNAL-01 完成 Store 基础。后续 [SUBMISSION-API-01](task-submission-api.md) 已由 API 和 Native 桥消费；后续[恢复 UI](task-submission-ui.md)已接入，浏览器关闭/宿主重开恢复通过，实际 Native 恢复窗口验证仍未完成。界面继续复用 Magpie，本项未修改页面或客户端存储。

## 原请求与生命周期

可信预览 owner 通过 `PrepareSubmission` 保存 preview_id、项目、原提交 key、完整原 CreateRequest、预览到期时间和配置版本，得到 Created/Submission 回执。服务器从已验证的预览构造它；不得把客户端传来的完整请求、任意 workspace path 或认证 Token 当作可信数据。提交 key 仅是请求幂等身份，不授予认证或执行权限。记录位于原私有 0700 状态根的 0600 SQLite 文件，未增加认证存储，也不将目标/key 放进浏览器 storage。

每个项目最多一条未解决的 UI 提交记录，其他窗口须先核对同一记录。保存记录不创建 Task、计划历史、预算、幂等创建回执、事件或 Runtime。新保存检查当前默认层 stamp；完全相同的保存重试读回原记录及状态，不重新解释旧配置。原 preview_id、项目、key、计划/创建 payload hash、JSON 草稿及 hash 都不可改写。不同项目仍可使用相同请求 key。

| 状态 | 转移条件 | 含义 |
| --- | --- | --- |
| prepared | 原 Task 创建及 task_submissions 回执原子提交 → committed | 原请求已保存，尚无该 preview 的任务回执 |
| prepared | 精确身份、该 preview 尚无任务回执；原子封存 → abandoned | 拒绝此后迟到的原提交或 peer receipt 绑定 |
| committed | 精确身份、关联的 Task/原任务回执已证明；确认读取 → acknowledged | 已核对任务保存回执 |
| acknowledged / abandoned | 仅相同身份重复读取；不能重新激活 | 终态历史保留，项目可准备下一条记录 |

`ResolveSubmission(...,"acknowledge")` 不能确认 prepared 记录；`abandon` 不能丢弃已 committed/acknowledged 的任务。创建与放弃共享一个 SQLite 事务锁：先封存则晚到 CreateSubmission/LookupCreationSubmission 为 ErrConflict，先提交则放弃为 ErrConflict。失败的状态写入不会释放未解决记录或承认封存。终态不能删除；确认保存回执不等于阶段运行成功或工程验收。

Task、计划、预算、预设来源、幂等和 created 事件、原提交回执、journal committed/task_id 同事务提交；journal 更新失败，创建整项回滚，prepared 原请求仍保留。已有完整 payload 的 peer 创建回执可以绑定原 preview 并同步 journal；它不创建第二个 Task。没有 journal 的既有任务提交语义不变。

`PendingSubmission(projectID)` 只读当前 prepared/committed 记录。读回、解决、创建前校验完整冻结 JSON、draft hash 与索引身份/payload，不依损坏的原请求继续提交。计划/目标/预算修改不会覆盖记录；Task 后续修订、执行计数和 Store 恢复仍独立遵守原合同。确认 journal 不重置计数、不改 Task 状态或事件。

## 迁移与验证边界

schema 7 的 007 新增表、每项目未解决唯一索引、不可变草稿、合法状态转移、committed Task 回执关联、保留历史和拒绝 abandoned 原回执的约束。001–006 文件/checksum、已有 payload hash 和 HTTP 回执不变；迁移不猜造旧任务/旧窗口的原请求。007/checksum 写入失败整步回滚到 schema 6，可重试；checksum drift 和未来版本 8 及以后拒绝。旧二进制不支持 7，回退须恢复对应一致性备份，不能改 user_version 降级。

实际测试覆盖准备后无 Task、持久重开、身份/项目冲突、两种确定提交/封存顺序与并发、整项创建回滚、终态重复、解决失败、已用预算/计划修订、旧 schema 6 数据保留、007 回滚/漂移/未来拒绝、损坏冻结记录拒绝。没有新增真实账号调用或实际 Native 窗口操作。

## 后续消费合同

恢复 API 必须限定已登记项目和当前 Management/Source/Native 窗口权限。prepare 只能接受原预览身份及请求 key，由服务端构造完整草稿；不得恢复或编译一个已丢失且从未提交的预览来自动运行。

UI 应在任务 POST 前确认原请求已持久保存；prepare 自身丢失回执仍须保留原身份并核对，不能换 key。重开窗口读取未解决记录，明确显示原项目/目标/计划与提交身份后再允许用户核对同一提交；已提交结果只读回原 Task。未提交且预览已丢失时，可明确请求原子封存，成功证明后才允许新预览；封存冲突须核对已提交 Task。acknowledge 的未知回执不能当作已经清除记录。

API/Native 消费已由 [SUBMISSION-API-01](task-submission-api.md) 实现；[UI 消费](task-submission-ui.md)已接入并通过浏览器验证，本存储组件独立结果不构成实际 Native 窗口恢复证明，也不扩大执行准入。来源/认证/Factory/quota 的真实验证、运行控制、工程闭环与最终 Gate 继续未完成，Jev off。详细日志与证据见 [SUBMISSION-JOURNAL-01](../work-items/WP-15/SUBMISSION-JOURNAL-01/summary.md)。

## 复现

Go1.26.3、macOS Xcode SDK 和 Python3，当前实施工作树执行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-journal-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/store ./internal/fusion/api ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

使用私有临时 HOME/XDG 和合成数据，不继承日常账号/Keychain；未进行模型或额度外部调用。仅确认 Store/旧 API/HTTP/桥接兼容及编译，不以此替代新恢复 API/UI 和最终验收。
