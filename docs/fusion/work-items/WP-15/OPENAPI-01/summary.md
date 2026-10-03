# WP-15-OPENAPI-01 已实现 Handler 接口合同

状态：`done`，父 WP-15：`in_progress`。

补齐 OpenAPI 中已实现的 submit、budget、plan GET/PUT、plan preview 和 SSE，完整列出当前 17 paths/19 operations。补全 Snapshot、ExecutionTarget、registry route 与 quota wire DTO，保留真实 nil/null 和纯文本错误形式；normalized quota reader 合同没有被削弱。运行期源码和准入规则未改。

新增实际 Handler 样本 capture，27 份响应覆盖所有 19 个成功操作，包含真正本机 loopback SSE；账号、模型、执行与额度来源为合成 fixture，上游真实调用/查询 0。新增 checker 只读取本地官方 OpenAPI 3.1 schema、合同和样本，校验标准结构、组件 schema、引用、参数、操作清单、请求/返回/header 与四项越权字段反例。最初 RED 为遗漏已实现 path，补全后 GREEN；三种临时合同破坏均被拒绝。

官方 schema 原样快照、来源与 Apache-2.0 许可随证据保存。27 样本中账户/credential identity 都是固定 fixture 标识；本机真实 key 不进入样本/源码/日志。

API race 67 PASS、0 SKIP、0 FAIL，API vet 通过。生产源码未变化，不重复上一项已通过的 CLI/GUI/full vet/Native 验证。不把标准文档校验宣称为全部协议语义、所有边界或真实账户验收。

接口合同与离线复核步骤见 [openapi-verification.md](../../../contracts/openapi-verification.md)。暂停/继续、默认层写入和产品 bootstrap/listener/GUI 未实现，不占用可调用合同路径；这些工作实施后仍须同步扩展合同和覆盖。Jev off。
