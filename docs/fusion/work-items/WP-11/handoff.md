# WP-11 交接

Adapter 必须先 Probe capability，并独立验证 generation/quota/model/all-calls；Supervisor 成功不构成路线准入。Spec 由可信服务端构造，不接收客户端 argv/env/paths/ValidateOutcome。启动要求 Store 中 starting + held reservation；实际控制器通过 Scheduler.Prepare 取得该预留，写阶段需要已核验 WriteKey。

生命周期 Events 是有界进度流，可能丢弃心跳；持久化 Store.Events 是任务事件回放依据。Native 原始输出只给可信 Adapter validator，公共 Result 只有 hash/长度/状态。不得将 exit=0/text marker 作为真实 Native 成功判据；fixture marker 仅测试启动原语。

Supervisor.VerifyStop 配给 Scheduler.Release。重新打开 controller 后，旧 generation、会话和 proof 都不能复用；禁止根据 launch.json 未记录 PID 推断进程没启动。GLM key 不在任何本包 worker 输入或日志中。
