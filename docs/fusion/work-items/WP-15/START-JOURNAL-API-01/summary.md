# WP-15-START-JOURNAL-API-01 · 原阶段启动记录 API 与 Native 通路

本组件完成；父 WP-15/WP-16 与整体目标保持 in_progress，最终 T01–T60 not_run。BASE cf2a8cc。新增 4 路径/5 操作的 Management API 和精确 Native allowlist，消费已有 Store 原记录；原 Magpie 与 Fusion UI/CSS、schema001–008、Controller/Runtime/policy 实现不变。

原 Task/Plan/key/role 在准备时冻结。项目 pending GET 发现待解决记录，Task GET/POST 必须使用原完整 If-Match 和 key；确认须匹配原 run，封存只允许尚未写入运行的请求。草稿宿主无需 Controller 即可读取/准备元数据，仍不能执行。任务变化、Server/宿主重开、unknown 执行及终态回执丢失不改原请求身份。Native 仅转送精确 Task 接口的原 key，继续校验窗口、登记项目与响应后的 Source。

新增 7 个 API 与 3 个 Native 顶层行为测试。有效 RED：未实现 API/Native 路径返回 404；首次实现的未登记项目被误判为空 200，现为 404。原草稿宿主 HTTP pause 503 是既有 Controller 边界，测试改用可信 Store 推进 idle Task 以验证原记录回读，未改 pause 行为。取消上下文在解析前增加授权检查，401 且没有持久准备。Native 重开通过真实宿主/Store；unknown 崩溃意图由可信 Store 合成，没有调用真实 Native Runtime。

最终 API 120 顶层/109 子测试 PASS、0 FAIL/0 SKIP；bootstrap/store/control/policy 234 顶层/309 子测试 PASS、12 顶层 SKIP、0 FAIL。SKIP 名称在 test-results.json 和原日志中逐项记录，opt-in 真实 Native/供应商与 helper 不计通过。Go1.26.3 CLI/GUI/full vet exit0。OpenAPI 37 路径/43 操作/106 实际 Handler 样本覆盖，10 项新响应反例通过。graph 已刷新至 14186 nodes/126546 edges。

证据：最终 API/回归日志、缺失路径 RED 和中间结果日志、handler-samples.json、openapi-results.json、build-results.json/构建日志、test-results.json 与 artifacts.json。权限测试在真实锁等待后调用生产 consumer；临时移除其四处权限重查时，读取/封存与撤销/取消四种场景全部失败，恢复源码后全部通过。mutation RED 为刻意反例，不是最终失败；源码逐字恢复，详见 mutation 日志与 test-results.json。历史日志仅规范化行尾空白及末尾空行，原 scratch hash 与规则保存在 test-results.json；构建日志原样保存。

复现和接口边界见 [合同](../../../contracts/task-start-api.md)。UI 恢复消费仍未接入，当前窗口内存的原启动请求没有因此自动改为跨窗口用户流程；实际 Native 点击/像素未验证。真实模型/额度调用为零、Jev off。下一步在原 Magpie 详情区接入持久准备与重开回读，再继续真实 Factory/账号/计费准入及工程闭环；本组件不发布发行版、不合入 main。
