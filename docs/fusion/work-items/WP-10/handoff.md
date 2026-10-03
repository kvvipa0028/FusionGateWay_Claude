# WP-10 交接

WP-11 必须用 Scheduler.Prepare 在外部启动前提交预留，实际监督器提供不可由请求伪造的 StopProof/VerifyStop。PID 与协议 done 单独均不足以释放写锁。WP-15 只暴露服务器判定后的接口，不允许请求填写 Inspection、quota、proof verdict。预算在任务首次执行前冻结，跨阶段共享，失败/取消不退款。

参见 contracts/blocked-reason-catalog.md；迁移新增 002.sql，schema 2 数据不能直接由 schema 1 binary 打开。
