# WP-15-CANCEL-API-01 整项任务取消 Controller / HTTP

状态：`done`，父 WP-15：`in_progress`。BASE：`cdb62e04348ed19c24342c1a03114bc41757d8d0`。

Controller.CancelTask/CancelTaskAuthorized 重查本地 Management，在完整 TaskVersion Store 事务提交后只取消精确 owned lifetime/Handle。空闲/paused 取消不调用 Runtime；晚到 Handle 仍收到已失效 lifetime 的 Cancel。不能接管不同 owner/unknown；没有可信 proof 或 Release 失败保留 receipt/held，不退款或重放。

新增 POST `/control/v1/tasks/{task_id}/cancel`，与现有 `/runs/{run_id}/cancel` 阶段取消明确区分。请求只允许 `{}` 和单个 strong Task If-Match；无/错管理、stage/query credential、Origin/encoding/越权 body 与陈旧条件拒绝。空闲可靠取消/已 cancelled 重读200；已提交 cancelling 意图202。后续停止不确定时409保留安全 TaskControlReply/RunView/固定 error；X-Fusion-Task-ETag 与同一 task DTO 读取一致，Location 指向 GET Task。不暴露 owner/session/原始错误。

新增 **12 项测试**：6 Controller、5 API、1实际 Native。空闲/paused 无执行、owned 停止及旧 start 只读、proof/释放失败 held、版本/owner/管理重查与晚到 Handle；真实 loopback HTTP/body/header/撤销/unknown与安全409 receipt。原 API 未实现返回400的 RED、缺 Controller 的编译 RED 保留；实现 GREEN。采集任务路径变量原与阶段 cancelPath 重名导致编译失败，改新变量为 taskCancelPath，未改生产/断言。先记录新方法缺失的 OpenAPI RED，再补齐。

固定 Claude Code 2.1.287 三个 Controller Native 场景显式 **3 PASS**：正常成功、暂停→needs_review、新整项取消→cancelled。新场景在实际 Native 的合成上游 HTTP 等待中发 CancelTask，真正 wait/StopProof/release；每例一次 HTTP/持久 Permit，继续/旧请求不重跑，调用不退款。Native 真实，Transport/account/route/准入为 fixtures；真实模型和额度查询0，不提升为真实账号或计费准入。

最终 Controller/Native targeted **9 PASS**，API/capture **6 PASS**；全量 Fusion race **352 PASS / 10 SKIP / 0 FAIL**。CLI/GUI build 和全仓 tagged vet exit0。OpenAPI **24 paths / 28 operations / 43 schemas**，49 actualHandler样本覆盖28成功操作，6越权输入schema反例及任务取消200/202/安全409，官方3.1结构/refs/DTO/header全部离线通过。更新隔离复核脚本 zsh -n 通过。

schema5/Store/001–005与旧启动/提交payload不变，Jev off，私有key不入源码或证据。合同见 [task-control-api.md](../../../contracts/task-control-api.md)。产品 bootstrap/listener/GUI 尚未注册，真实路线/权限/额度/计费准入、Native检查点恢复、工程交接闭环及其余30工作包/60类最终Gate继续实施。此组件完成不等于WP-15或整体目标完成。
