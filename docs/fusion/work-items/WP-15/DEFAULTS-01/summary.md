# WP-15-DEFAULTS-01 全局与项目阶段默认层

状态：`done`，父 WP-15：`in_progress`。BASE：`7a4e4dfa410be636a7e5677b7b1667ec8bdff4b2`。

完成 Management GET/PUT/历史版本、五角色 canonical 全绑定覆盖、显式 inherit、不可变 hash/版本与强 If-Match。用户保存层替代同 scope 的 bootstrap；注册草稿不授予生成准入，全局 RouteRef 歧义拒绝。配置读取/预览从 Store 加载当前 global/project 对，其他 Server 写入及服务重建均可见。新任务/新计划实际写事务重查 DefaultStamp，旧任务/已应用 receipt 保持冻结快照与预算/事件。

新增精确 project/key/payload 的只读 LookupCreation，修复 peer 已提交请求被后来的配置变化错误拒绝的问题；只读查询先于新建资格检查，不触发 Runtime、不退款或新写事件。默认层选择和可信路线登记分开计版本，quota reader/cache/观测时间不被选择变更刷新，可信 SetProject 仍要求重新登记来源。

schema 5 新增两个独立表和历史保护，001–004 未改，005 单独 checksum；schema 1–4 数据保留与迁移失败回滚、未来 6 拒绝均通过。旧 CreateRequest JSON/hash 不增加 stamp 字段；任务没有新增默认层来源 FK，具体 Target 仍由 Snapshot 冻结。原迁移最终版本断言改为 5，原历史数据断言保留。

新增 16 项 targeted race 全部通过；最终全量 Fusion race **308 PASS / 8 SKIP / 0 FAIL**。八项 skip 是五个显式 Native opt-in、一个真实额度 opt-in 和两个 helper；其中 Controller Native 已独立显式运行并通过。CLI/GUI build 与全仓 Fusion tagged vet 均 exit 0；GUI 保留已有 SDK/deployment warning，不影响本次 build 成功。

固定 Claude Code 2.1.287 在 schema 5 上通过既有 Controller/Adapter 合成回归：一次 HTTP/Permit、真正 wait/StopProof 后释放。此项没有新增真实模型/额度查询，不证明真实账号/计费/完整额度或三路线工程准入。private key 保持仓库外，不进入本项源码/日志/样本，Jev off。

OpenAPI 增加四条路径/六个操作，当前 **21 paths / 25 operations / 42 schemas**；36 份 Handler 样本覆盖全部成功操作，官方 schema/组件/引用/header 与五个越权字段反例校验通过。样本 capture 源码也参加最终全量回归；保存样本来自最终 peer 修复之前，修复没有改变 DTO 或 capture 行为。

保留初始 RED 和已修复的失败：未知项目额度 500→404；peer durable receipt 409→原 Task；新增 Store lookup 测试第一次引用了不存在的 helper，换回既有 plan helper后通过。没有削弱原测试断言或以 fixture 准入替代真实验证。

运行行为合同与复核边界见 [default-layer-api.md](../../../contracts/default-layer-api.md)。产品 listener/GUI、暂停/继续、真实路线准入与剩余工作包及最终 Gate 尚未完成。此提交仅完成本子项，不宣称整个 WP-15 或项目完成。
