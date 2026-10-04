# 任务提交回执的持久核对

组件 WP-15-SUBMISSION-RECEIPT-01。UI 沿用原 Magpie 的界面；本项没有修改页面、控件、样式或客户端存储。

POST `/agent/v1/tasks` 仍只接收 `preview_id`、`plan_hash` 和唯一 `Idempotency-Key` header。首次提交与已提交的原请求重读均为 201，返回同一个 Task 的当前状态、plan_revision 和 generation；其后计划修订不改变原提交身份。核对不启动 Runtime、不查询额度、不重新编译原计划、不重置预算、不新增 created/execution 事件。新建提交仍需有效未过期预览、配置版本与默认层 stamp；已提交回执的核对不重新评估新建条件。

## 持久关联

schema 6 的 `task_submissions` 将 preview_id 唯一绑定到原 key、project_id、plan_hash、payload_hash 和 task_id。关联与 Task/plan revision 1/预算/预设来源/idempotency/created event 在同一事务提交；关联写入失败则整项创建回滚。插入必须匹配原项目/key/payload/任务和 revision 1 的计划 hash，关联不能更新或删除。原项目级 key 语义保留，不变成全局 key。

预览仍只在进程内保存，未提交的预览在重启或清理后不可执行。只有确已提交的完整原身份可通过持久关联读回 Task；不能只凭 key、相同目标、任务列表或 plan_hash 猜测结果。错误 key/hash、未知预览同为 409。关联到的任务项目须仍登记，否则 404；管理认证、可信来源/文件夹/token 撤销及 Native 窗口鉴权继续生效。恢复回执不是执行准入。提交处理返回后重查 Management context；等待期间权限撤销或请求取消时不公开 Task/错误详情。此时创建可能已经提交，客户端应保留原请求，在重新建立有效认证后继续核对。

为兼容既有跨 Server 幂等合同，仍在内存中的另一真实预览可通过完整可信 CreateRequest 附加到相同 project/key/payload 的已提交任务。这条 existing-only 路径不会新建任务；payload 不同则冲突。相同 key 在另一项目仍可用于其独立任务，preview_id 不可重绑定到另一项目或 key。

## 迁移与恢复边界

006 自动迁移现有 schema 1–5，保留旧文件 checksum、Task、历史计划、预算及已用计数、预设、默认层、事件和旧 payload hash。旧任务没有原 preview_id，迁移不伪造 HTTP 关联。006/checksum 写入失败整步回滚，可修复阻塞后重试；历史 SUBMISSION-RECEIPT-01 使用 schema 6。后续 [SUBMISSION-JOURNAL-01](task-submission-journal.md) 新增 schema7 的私有提交记录，[START-JOURNAL-01](task-start-journal.md) 进一步迁移至schema8。当前 checksum drift 和版本9及以上拒绝打开；旧schema≤7二进制不支持8。回滚须在宿主停止后恢复对应一致性备份，不能改版本号降级。

本项已验证预览真实到期清理、新 Server、Store Close/Open 和 owned loopback 宿主重启，以及真实 HTTP→Native 桥原请求回读、未登记项目/Source 撤销拒绝。没有声称新增 SIGKILL/硬件断电测试或实际 Native 窗口视觉验证。

本回执组件独立实现不能恢复窗口丢失的原 body/key；后续[私有提交记录](task-submission-journal.md)、[恢复 API/Native 通路](task-submission-api.md)及[恢复 UI](task-submission-ui.md)已接入。页面先保存记录再发送 Task POST，浏览器关闭/宿主重开恢复已验证，实际 Native 恢复窗口和运行控制仍待完成。仍持有原请求的窗口可在宿主重启后用原请求重试，当前有效的私有宿主认证须重新建立；不复用已撤销的旧 token。

## 复现

Go1.26.3、macOS Xcode SDK 和 Python3，当前实施工作树中执行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-receipt-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/store ./internal/fusion/api ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

测试使用私有临时状态及合成路线，Inspect/Resolve 始终拒绝，真实模型/额度调用为 0。既有 15 项浏览器流程在本项构建的 CLI/test-only 宿主上回归。详细日志、迁移失败与修正、标准接口校验及源码/证据 hash 见 [SUBMISSION-RECEIPT-01](../work-items/WP-15/SUBMISSION-RECEIPT-01/summary.md)。真实供应商 Factory/账号/Forwarder/quota 与完整 WP-16/M4 验收仍未完成；Jev off。
