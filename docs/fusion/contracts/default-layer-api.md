# 全局与项目默认阶段配置 API

此合同适用于 Management 保护的内部 Handler，产品 listener/GUI 尚未注册。配置优先级为本次任务 > 项目 > 全局；用户保存的层完整替代同 scope 的可信 bootstrap 层。HTTP 只能选择已登记的路线/模型，不能新增账号、凭据、workspace、Runtime、计费路径或任务预算。

| 方法/路径 | 行为 |
|---|---|
| GET `/control/v1/defaults/global` | 读取已保存的全局默认层 |
| PUT `/control/v1/defaults/global` | 按强 If-Match 写入下一版本 |
| GET `/control/v1/defaults/global/versions/{revision}` | 只读确切历史版本 |
| GET `/control/v1/projects/{project_id}/defaults` | 读取已登记项目的保存层 |
| PUT `/control/v1/projects/{project_id}/defaults` | 按强 If-Match 写入该项目下一版本 |
| GET `/control/v1/projects/{project_id}/defaults/versions/{revision}` | 只读项目历史版本 |

未保存时，最新 GET 返回 200、ETag `"0"`、revision 0、configured false，layer/hash/created_at 为 null。这不代表 bootstrap 已清空。不存在的历史版本及未知项目返回 404。已保存时 configured true，五角色 layer/hash/created_at 均有值，ETag 为带引号的整数 revision。

PUT body 必须有非 null 的 layer，完整示例：

```json
{"layer":{"roles":{"design":{"mode":"locked","route":{"id":"已登记的路线 ID","revision":1},"model":"已登记的模型","effort":{"mode":"default"}}}}}
```

示例标识必须替换成可信登记值后才能保存。三个 groups 可以作为输入简写，存储展开成 design、implementation、testing、review、acceptance 五角色；未提供角色填为 inherit。整层替换不做字段拼接，同一角色的模型、路线及 effort 一起覆盖。`{"layer":{}}` 保存显式 all-inherit 版本；项目由此回到全局，全局 all-inherit 则可能使必需角色没有可解析绑定，预览应拒绝，不能臆造实际模型。

首次 PUT 使用单个 If-Match `"0"`，后续用该 scope 最新版本。新提交为 201，响应 `{default_layer,created:true}`；相同 base 和规范化内容的重试为 200/created false，读回当时的不可变版本，不能把较新的 head 改回旧版。响应 ETag 与 Location 均指向返回版本。相同 base 不同内容为 412，缺失 If-Match 为 428；weak、通配、多值、前导零、溢出或非法值为 400。未授权/错误 Management 为 401，stage/query credential 为 403。项目 head 上限 128，满时创建新项目层为 429，现有项目与唯一 global 仍可增加版本。没有 DELETE；恢复继承需提交新版本。

路线引用和模型必须已登记，但草稿保存不要求路线已获生成准入，保存不授予准入。global 使用已登记项目的路线集合；相同 RouteRef 若对应不同完整元数据，则视为歧义并拒绝选用，不按 map 遍历顺序挑账号。历史重试不需要当前路线仍登记，Store 仍比较精确 canonical hash；新保存和新编译分别执行自己的当前校验。错误不回显输入、凭据或 Native 原始消息。

## 预览、冻结与并发

每次有效配置读取或预览都会取 Store 中当前 global/project 的 hash 对；其他 Server 共享同一 Store 时，变更同样会更新本实例配置 revision。服务重建并可信 SetProject 后重新加载保存层。此处验证的是同一进程的共享 Store/Server；OS flock 仍禁止两个控制进程同时打开数据库。

预览记录私有 DefaultStamp；新任务和新 plan revision 必须在实际 Store 写事务内重查该 stamp，避免 HTTP 预检查后另一 Server 保存默认层的竞态。旧的未提交预览失效返回 409，不创建任务/事件。已提交任务保存具体 Snapshot/Target，不随默认层、bootstrap 或预设更新而重编译；预算、generation、run 和历史事件不受默认层写入修改。

任务提交重试先按 project/key/完整请求 hash 只读查找已有持久 receipt，再检查创建新任务所需的当前配置。另一个 Server 已提交的相同请求，也可在默认层变更后读回同一 Task；不同 payload 返回 conflict，不同 key 不能用失效预览创建新任务。只读查找不会调用 Runtime、退款或写事件。Plan 已应用 receipt 继续只读原快照；没有新增 task-default 来源 FK 或公开版本来源接口，DefaultStamp 是提交条件，不是任务的动态 latest 指针。显式预设来源合同保持原样。

额度来源绑定独立的 trusted routeRevision：默认层选择变更可以更新 configuration revision，但不能重新登记 reader、丢弃/刷新观测或修改 ObservedAt/ReceivedAt。只有可信 SetProject 改变路线登记后需要重新 SetQuotaSources，额度 identity 与出站 Current 继续独立校验。

## 存储与验证

schema 5 新增不可变 default_layer_revisions 与带 FK 的 default_layer_heads，005 checksum 独立保存，001–004 原样保留。schema 1–4 按顺序升级；schema 6 和 checksum 异常拒绝。已验证 schema 4 任务/预设引用、原提交 payload hash、预算及事件保留；迁移 metadata 写入故障会回滚新增表和 user_version，修复故障后可重试。已有 schema 1–3 数据保留测试仍执行；最终版本断言改为 5，不更改历史数据断言。

部署前停止派单并备份一致性数据库、sidecar 与 artifact。回滚到 schema 4 或更早 binary 必须恢复该版本的一致性备份，不能只换 binary 或手改 user_version。当前没有自动备份/回滚产品入口。

本项新增 16 个 Store/API 测试，覆盖不可变/并发/故障回滚/容量/继承/草稿拒绝准入/全局歧义/重启/跨 Server receipt/stamp 与额度观测保留。验证包见 [DEFAULTS-01](../work-items/WP-15/DEFAULTS-01/summary.md)；OpenAPI 扩展为 21 路径/25 操作，36 份实际 Handler 样本。实际固定 Claude Code 2.1.287 的合成 Controller/Adapter 回归验证一次 HTTP/Permit、真实 wait/StopProof 后释放；真实模型与额度查询均为 0，不作为实际账号工程准入。暂停/继续、产品接线与 GUI 仍待实施，Jev off。
