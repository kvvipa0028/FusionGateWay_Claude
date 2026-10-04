# WP-16-TASK-UI-01 · 冻结提交与任务回读

基线 `1c633b9230181dd7996ef68a53b4ec6a06c12b30`。本组件已实现并验证，父 WP-16 继续 in_progress，最终 T01–T60 保持 not_run，Jev off。

## 行为

在现有 Magpie 样式/列表/按钮基础上增加冻结提交、任务分页及详情。预览展示具体模型/批准候选、路线版本、冻结 effort/account/billing/lock、计划 hash、预算及有效期。提交只发送原 preview_id/plan_hash 和唯一 Idempotency-Key，任务保存与阶段启动分别显示。

- 有效 201 Task 回执确认保存；首次断线/5xx/异常 2xx 保留原 body/key，锁定编辑和新预览，提供原样重试。已经未确认时再遇 4xx 仍不能认定第一次没有入库；任务列表仍可读取。没有自动换 key 或猜测 ID。
- 修改选择/目标/阶段/范围/载入预设使旧预览失效，到期禁止新提交；服务端仍校验配置变化。明确首次 409 拒绝保留草稿供重新预览。
- 固定 32 项分页，32+1 实际任务验证；列表按项目与请求序号隔离，任务详情按选择与请求序号隔离。旧项目/旧任务迟到响应不能替换当前显示。
- 详情读取完整 Task、Plan 和 Budget，再次核对 Task 条件及 plan revision/ETag；最多重读一轮。预算是单独读取的计数，不冒充跨接口原子快照。异常详情清空旧内容。目标与字段纯文本显示。
- Native 桥仅增加精确冻结提交和三个 GET 详情路径，以及固定模块资源。Task 必须属于可信登记项目；未登记任务/不存在任务为 404，其它窗口拒绝 403，来源撤销为 503。只在提交路径透传 Idempotency-Key，缺少/重复值由原 API 拒绝。未开放执行、取消、事件或额度。

操作与复现见[任务工作台合同](../../../contracts/task-workbench.md)。普通产品 fusion-ui 仍是未准入草稿宿主；成功路径是明确的合成 Factory 验证，不代表真实用户已能执行模型任务。

## 验证

| 项目 | 结果 | 证据 |
| --- | --- | --- |
| Native 提交 RED / GREEN | 原路径 404；接线后 same key 返回同一 ready Task，零 Inspect/Resolve，仅 created 事件 | [RED](task-ui-native-red.log)、[GREEN](task-ui-native-green.log) |
| 浏览器 RED | 原页面无冻结提交入口 | [RED](task-ui-browser-red.log) |
| 全部页面流程 | 15 PASS、0 FAIL，含实际保存/重载、两类未知回执及随后 409 的原请求保留、到期、分页与迟到响应、异常详情、纯文本目标 | [最终浏览器](task-ui-browser-verified.log) |
| 模型逻辑 | 10 PASS、0 FAIL | [模型日志](task-ui-model-final.log) |
| bootstrap / API race | 134 PASS、198 subtest PASS、0 FAIL、3 SKIP | [Go 日志](task-ui-host-api-final.log) |
| Go1.26.3 CLI / GUI / full vet | 三项 exit0 | [构建报告](build-results.json) |

初轮浏览器发现实现将 FrozenEffort.requested_mode 错用为 mode；按真实 Go DTO 修正后成功提交/重试通过。第二轮到期用例在创建定时器后才安装虚拟时钟，不能推进已存在的浏览器计时器；修正测试安装顺序后到期检查通过，未修改或削弱生产到期保护。保留失败日志。3 项 SKIP 是两项需要真实 pinned Native 凭据的测试及普通 Go 套件不启动合成宿主；后者由浏览器显式启动。日志只去除行尾空白，原 SHA256 留在[机器报告](test-results.json)。

[桌面](desktop.png)和[窄窗口](mobile.png)为合成项目的浏览器可见区域截图；本轮没有实际 Native 窗口操作或视觉截图证据。目标 `<img...>` 是文本验证输入，没有执行 HTML，也没有真实 provider 调用。

## 尚未完成

未确认提交原请求仅在当前窗口内；beforeunload 提示不构成持久恢复。窗口重载/关闭后的恢复，以及服务端 preview receipt 清理/重启后的 HTTP 对账仍须实现，不能把任务列表猜测或新 key 重交当作恢复。阶段启动/推进、取消暂停、事件/额度、计划修订工作台、真实 Factory/账号准入及完整工程闭环继续实施。
