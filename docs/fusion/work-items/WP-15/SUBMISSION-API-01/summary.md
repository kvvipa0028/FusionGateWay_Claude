# WP-15-SUBMISSION-API-01

状态：恢复 API/Native 通路组件 `done`；UI 消费、完整 WP-15/WP-16/实施目标继续 `in_progress`。基线 814398a4fb136eaab5d3c681b5d93241e315a9fd。合同见 [task-submission-api.md](../../../contracts/task-submission-api.md)。Magpie 和 Fusion UI 资源均未修改，不声称已完成窗口恢复操作。

## 结果

新增已登记项目 submission GET/POST、acknowledge POST、abandon POST。只接收原 preview/hash 与单个 key，服务端从真实有效预览构造完整草稿；不接任意 CreateRequest/目录/凭据。读取未解决记录，精确准备/解决重试读回不可变历史；新 Server 或真实宿主/Store 重开不改原目标/计划/预算/key。丢失未提交预览不复活，原请求 409；明确封存才释放 pending，迟到原提交拒绝。committed 原请求返回同一 Task，确认仅核对保存，不算工程验收。

Store 增加精确原身份的 LookupSubmissionJournal；已有草稿/终态/hash 校验继续复用，schema 7 与 001–007 不变。API 不重置 Task/计数/事件或执行模型。没有显式预算的通用 Store 草稿不会被解释为 UI 预览，API 409 且保留记录。

Native 仅扩展准确已登记项目路径与方法及所需 Idempotency-Key 转送，旧认证/本窗口/URL 拒绝保留。发现原 Source 可在 HTTP 回复读取期间撤销而前置检查已结束：新增回复缓冲后的 Source/token/取消核对，失败 503，不发送已缓冲目标/key/Task。这也覆盖原允许的桥接请求；已提交写入不回滚，仍需有效权限核对原请求。

## 验证

| 范围 | 最终结果 | 证据 |
| --- | --- | --- |
| Store race | 104 PASS、1 helper SKIP、32 子测试 PASS、0 FAIL | [首次最终后端日志](submission-api-final.log) |
| API race | 110 PASS、79 子测试 PASS、0 FAIL | [同日志](submission-api-final.log) |
| bootstrap race | 35 PASS、3 显式 SKIP、135 子测试 PASS、0 FAIL | [最终 bootstrap 日志](submission-api-bootstrap-final.log) |
| Go1.26.3 CLI/GUI/full vet | 各 exit 0，末端权限核对后重建 | [结果](build-results.json) |
| OpenAPI | 32 paths、37 operations、78 实际响应，9 输入/5 项目/7 任务/7 提交负例 | [样本](submission-api-samples.json)、[校验](submission-api-openapi.json) |
| graph | 14043 nodes / 125036 edges | [结果](graph.json) |

最终受影响后端按包合计 249 顶层 PASS、246 子测试 PASS、4 显式 SKIP、0 FAIL，新增 8 顶层测试。Store/API 源码在首次最终回归后未再变；之后只改 Native 末端权限检查/对应测试，bootstrap 全包和构建重新通过。两日志不能叠加旧 bootstrap 项数计算。

测试覆盖原 request 准备无 Task、到期/配置变化/新 Server/终态精确重试、未登记项目、丢失未提交预览拒绝、封存与迟到提交、确认及不改历史、输入/身份/新准备资格、等待中取消/撤销不修改不披露。实际 owned HTTP/Store 关闭重开→Native 恢复 prepared 与 committed、原 Task 重读、封存/确认、严格路径拒绝、Source 变化拒绝和 0 Inspect/Resolve 已验证。末端 Source 撤销由真实 HTTP 已返回成功回复后精确修改私有来源注入，真实生产桥接受 503。

## RED 与修正

1. [API RED](submission-api-red.log) 观察新接口实际 404，未存原请求；其中等待体测试因尚无路由也没有读取。新增 API 后 [首次 GREEN 尝试](submission-api-green.log) 有 fixture 失败：Store 将同一到期时刻标准化为 UTC，fixture 用 reflect 比较原 +0800 时区；阻塞 body fixture 缺 application/json；query key 既有权限边界应是 403 而不是 400。按明确合同修正 fixture，不降低生产检查。[调试](submission-api-debug.log) 保留纯合成信息和时区依据。
2. API 四项/11 子项通过后，Native 新通路仍 [404 RED](submission-api-bridge-red.log)。增加严格路径/key 转送后 [GREEN](submission-api-bridge-green.log) 通过实际宿主/Store 重开场景。
3. [晚到 Source RED](submission-api-source-red.log) 证明旧前置核对会发送 200 原记录。新增末端核对，最终 bootstrap 全包通过，无目标/key 披露。没有放宽状态断言或禁用生产保护。

日志只去行尾空白，原日志与保存 hash/变换见 [log-provenance.json](log-provenance.json)。源码/证据 manifest 及 JSON/本地链接/gofmt/diff/私有 key byte 排除检查记录在 artifacts.json / validation.json；不输出 key 或其 hash。主仓库原两个 `.DS_Store` 保留。

## 未完成与复现

UI 资源、布局和浏览器消费者没有变化，因此未重新执行相同浏览器流程或实际 Native 窗口操作。任务窗口仍只在内存保存请求，下一项应在原 Magpie 工作台接新 API，再验证窗口关闭/重开操作；本项服务端/桥接证明不替代它。运行控制、真实账号/Factory/Forwarder/quota、工程闭环和最终 T01–T60/整分支终审继续未完成，Jev off。

本项未重新运行完整 Fusion suite、SIGKILL/硬件断电或真实账号/模型/额度测试；采用受影响 Store/API/bootstrap 全包 race、独立 CLI/GUI/full vet 和离线标准合同检查。复现前提/精确命令见 [接口合同](../../../contracts/task-submission-api.md)；测试隔离临时 HOME/XDG/白名单，不读取日常认证/共享 Keychain。
