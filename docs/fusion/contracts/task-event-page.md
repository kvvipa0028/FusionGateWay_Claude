# 任务事件：有界分页回读

组件 WP-16-EVENT-PAGE-01。沿用 Magpie 原 UI，在任务详情后增加默认折叠的“任务事件”区，复用原列表、按钮及文字样式，CSS 未改。事件是持久状态元数据，不是模型输出、实际停止证明或工程验收结论。

## 操作

选择任务并读取有效详情后，展开“任务事件（按序回读）”，点击“读取新事件”。每次最多读取 32 条；提示仍有更晚事件时继续点击。页面只保留最近 128 条显示；“从头读取事件”从序号 0 重新读取，不删除服务端历史。切换任务或项目会清空页面事件和游标；重新读取同一任务详情保留已读取历史。

读取失败、授权失效、异常成功回复均保留原事件和游标，用户可明确重读同页。没有自动轮询或重试，也不自动刷新 Task/Run、采纳运行身份或解除未确认启动请求。Task 状态、运行回执和预算仍须独立核对。当前产品入口未注册真实执行 Factory；测试合成任务不能当作真实供应商成功证据。

## HTTP 合同

`GET /agent/v1/tasks/{task_id}/events/page/{after}` 受 Management 授权、当前项目登记和现有来源约束保护，禁止 query 和 Last-Event-ID。after 是规范非负 int64 十进制字符串，无符号、前导零或溢出；0 表示从头读取。未知或已撤销项目任务 404，超前游标 409，错误游标 400，非 GET 405。授权/上下文在读取后和回复前重新核验。

成功 JSON 包含 task_id、after、next_after、has_more、events。events 为非 null 数组，最多 32 项；每项仅 task_id、seq、kind、run_id、generation。沿用 Store.EventsPage 的连续持久序号，额外读取 1 条判断 has_more；连同 lookahead 一起校验元数据。next_after 为本页末序号，空页保持 after。不追加事件、不编译计划、不登记额度、不调度或启动 Runtime，没有 Task ETag 更新承诺。

原 SSE `/agent/v1/tasks/{task_id}/events` 继续独立存在，重连、断线、权限撤销和容量合同不变。Native 桥的 8 秒期限和完整响应缓冲不适合无限流，因此本组件使用有界 JSON companion；实时 Native 推送需要另行实现 owned 订阅与取消合同。

Native 只新增精确规范 GET 路径，先核对本窗口及当前 owned 宿主，再检查 Task 所属项目。原窗口/Host/Origin/Source/token、请求响应 bounds 和缓冲完成后复核不变；流式 events、任意后缀、非 GET 和未知任务继续拒绝。不将管理 token 暴露到页面或 URL。

页面在提交游标前检查任务身份、原 after、最多 32 条、逐项连续序号、有限 kind、非负安全整数 generation、受限 run_id，以及 next_after/has_more 的关系。只将允许字段以 textContent 显示；未知字段不渲染。过大的 JS 整数拒绝，不舍入序号。请求带项目/任务/请求序号，迟到旧回复不能替换新任务；无效回复不能推进游标。

## 验证与复现

完整浏览器 49 PASS（原 44 项及新增 5 项），包括真实私有 HTTP/Store 的 141 条连续事件、32 项分页、128 条显示上限、从头回读、失败同页重试、恶意成功字段及跨任务迟到回复；desktop/390px 截图无横向溢出。API race 113 顶层/81 子测试、Native/Store targeted race 12 顶层/22 子测试均 PASS，包含 owned 宿主关闭重开后原事件一致，来源或窗口失效拒绝。CLI/GUI build 与 full fusion,nogui vet exit0，Go1.26.3。全部零 FAIL/SKIP，零真实模型/额度调用，Jev off。

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/event-fixture-build.log test -c -mod=readonly -tags fusion,nogui \
  -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  PATH="$HOME/.local/bin:$PATH" node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/event-api.log test -mod=readonly -tags fusion,nogui -race -count=1 -v ./internal/fusion/api
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/event-native.log test -mod=readonly -tags fusion,nogui -race -count=1 \
  -run 'TestNativeEventPage|TestNativeStageBridge|TestEventsPage' -v ./internal/fusion/bootstrap ./internal/fusion/store
```

需要已有 Node/Playwright/Chrome、Go/Xcode SDK 与离线构建缓存。fixture 只存在测试二进制，使用私有临时 HOME/XDG、合成 Factory 和独立浏览器 profile；测试关闭自己拥有的宿主并清理临时目录。原 Magpie 三资源逐字等于锁定源码，Fusion CSS 逐字等于 BASE。详细证据见 [EVENT-PAGE-01](../work-items/WP-16/EVENT-PAGE-01/summary.md)。

本组件未操作实际 Native 窗口；桥接、HTTP 和浏览器截图不代替 Native 点击/像素证据。真实供应商准入、原启动请求的跨窗口恢复、计划修订 UI、工程阶段闭环和最终 T01–T60 仍未完成；父 WP-15/WP-16 保持 in_progress。
