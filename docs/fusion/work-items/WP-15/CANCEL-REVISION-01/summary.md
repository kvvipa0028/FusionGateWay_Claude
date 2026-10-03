# WP-15-CANCEL-REVISION-01 取消请求的事务版本检查

状态：子项 done；WP-15 in_progress。

合法的未来角色计划修订保留活动 run 的冻结 PlanRevision 与 generation。原取消 Handler 在 HTTP 预检查后只按 owner/generation/lease 写入 cancel_intent，存在版本在两次操作之间更新时仍接受旧 If-Match 的竞态。

新增 Store.CancelIntentAtRevision，在写入取消状态/事件的同一事务重查当前 Task revision；Controller.CancelAtRevision 传递条件并保留版本冲突，API 映射为 412。cancelling 重读同样受检查，终态只读核对当前 Task revision/generation。无外部条件的内部 owned shutdown 保留原 Cancel 语义，不改数据库 schema、预算或 Native 停止证明。

两项新增回归覆盖合法计划修订保持 generation、旧/未来/非法 revision 无运行与事件副作用、同版本幂等取消、cancelling 后再次修订、预检查与事务之间的旧版本调用、HTTP 412、用当前版本取消历史冻结 run、实际等待合成执行退出并释放，以及终态条件。RED 首先因方法不存在无法编译；临时直通旧 CancelIntent 的兼容方法在非法 revision 上接受取消而失败，保留日志。最终实现通过，不削弱断言。

完整 Fusion tagged race 260 PASS、8 SKIP；定向回归 7 PASS；CLI/GUI 编译与全仓 vet 通过。新测试执行器与 StopProof 为 fixture，真实账号/Native 证据未扩展；本项真实模型/额度调用均为 0，Jev off。产品 listener/GUI、完整 OpenAPI、其余工作包和最终验收继续实施。
