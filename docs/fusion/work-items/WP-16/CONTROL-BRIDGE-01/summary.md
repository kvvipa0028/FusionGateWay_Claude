# WP-16-CONTROL-BRIDGE-01 · 固定运行控制 Native 通路

状态：桥接组件完成并验证；运行控制页面、真实供应商 Factory 和实际 Native 窗口操作仍未完成。父 WP-16、整体实施目标保持 in_progress，最终 T01–T60 保持 not_run。

## 修改和依据

文档要求单阶段手动执行、暂停/取消及清楚显示 pausing/未知状态。原 API/Controller 已实现这些动作，Native 桥之前拒绝它们。新增精确 start/pause/continue/task cancel、run GET/run cancel 六种通路，先核验 owned 窗口、当前源与 Task 所属登记项目，再调用既有 Management HTTP。仅 start 转送原 Idempotency-Key；完整 If-Match、header 唯一性、预算/owner/generation 条件保持原 API/Store 合同。安全 X-Fusion-Task-ETag 可回读；Location/cookie/auth 继续剔除。详见[合同](../../../contracts/native-task-control.md)。

页面、CSS、Magpie 原三文件、API DTO、Store/schema/Controller 均未改；没有放宽真实路线准入。草稿 fusion-ui 的已有登记 Task 仍不能控制执行，返回 503；合成运行来自 test-only RuntimeFactory。后续按钮继续复用 Magpie 原任务详情区，不做重设计。

## 失败与修复证据

初始 fixture 编译遇 Generation 类型与 Store Plan 方法名错误，校正为真实接口后得到有效 RED：新增固定控制返回 404，缺条件/幂等 header 尚未到达 API。接入精确桥接后 GREEN。中间测试按 RunView 直接解码 run GET，而实际合同是 ExecutionReply 包装：依据 readRun 与公开 API 测试修正 oracle。另一 fixture 将 run cancel 误期望为 needs_review；核对 Store.Finish 与状态合同后修正：只有 pause 下 cancelled 收尾 needs_review，其余取消为 cancelled。均为测试编写修正，未削弱生产状态或权限处理。

原 task_ui_test 对 start/cancel 的“未开放 404”按本次授权的路径扩展改成“缺条件 428”；继续验证提交/读取不触发 Inspect/Resolve、仅 created 事件。合成 release 计数在 ReleaseReserved 成功后增加，避免在事务完成前误判停止收尾。

## 验证与边界

最终 targeted race：54 顶层/100 子测试 PASS，0 FAIL/0 SKIP，覆盖 bootstrap Native/提交/来源，API 条件/执行/暂停取消，以及 Controller owner/StopProof 失败保留/晚到 Handle/管理撤销。新增 bridge fixture 经过真实私有 loopback HTTP 与 Store/Controller，Runtime、Native session、quota 与 StopProof 全为合成；零真实模型或额度调用，Jev off。

Go1.26.3 CLI/GUI build、tagged 全包 vet exit0。graph 已刷新；源/packet hash、JSON/Markdown links、gofmt/diff、build hash 与私有 key byte exclusion 见 validation.json/artifacts.json。API DTO/OpenAPI 未变，未重复 OpenAPI capture 或完整 Fusion suite。页面源未改，未重跑浏览器；实际 Native 恢复窗口依旧待桌面解锁验证。本桥接证据不代替 Native 按钮或真实账号/Factory/Forwarder/quota/工程闭环。

## 复现

当前实施工作树，需要预装 Go1.26.3、Python3、Xcode SDK、module/cache。隔离 runner 只允许公开工具路径及临时 HOME/XDG，不读取日常认证。

```sh
mkdir -p .fusion-dev/implementation
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/native-task-control-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/bootstrap ./internal/fusion/api ./internal/fusion/control \
  -run '^Test(NativeStage|NativeTask|ControlHostStageUI|SubmissionJournal|SubmissionReceipt|TaskControlAPI|TaskCancelAPI|AuthenticatedExecution|ExecutionPreconditions|ExecutionReadAndCancel|ExecutionTaskETag|ControllerPause|ControllerCancelTask)'
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

测试清理本人 owned HTTP/Controller 和临时目录，不停止日常程序。下一项接运行控制页面、原启动未确认核对与状态回读；流式事件/额度、检查点恢复、真实准入和最终 Gate 继续实施。
