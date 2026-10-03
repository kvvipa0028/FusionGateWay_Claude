# WP-07

已实现独立 strict Dispatcher：使用持久化 run 的冻结目标，不调用旧模型组、意图规则或 fallback。每次发送/重试检查阶段身份、路线准入、精确 effort/账号/版本/transport 与权限预算；gate 等待后再次检查。目标深复制，账号凭据只允许同身份续期。

离线验证覆盖移除路线、身份/版本变化、非法 effort、gate 中撤销、上游模型不一致、未知模型保留 null、redirect/503/截断/超限/重复 JSON，以及供应商错误不泄露 secret。上游失败和身份变化时替代账号/模型收到零请求。Fusion race、原版 30 个组/effort/rule 回归、CLI/GUI 构建与 vet 均通过，原始日志随包保存。

当前入口仍关闭；本包不是产品 API/GUI 或三订阅 Runtime 的真实准入。最终验收仍为 not_run。
