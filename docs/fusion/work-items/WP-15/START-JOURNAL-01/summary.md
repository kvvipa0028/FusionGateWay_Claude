# WP-15-START-JOURNAL-01 · 原阶段启动持久记录基础

组件完成；父 WP-15/WP-16 和全工程目标继续 in_progress。BASE b3cae7b。新增 Store 的 PrepareStart/PendingStart/LookupStartJournal/ResolveStart、schema8 及 journal 与原 run/start_requests 的事务关联。没有 journal 的既有 Start/resume 语义保持；原 Magpie/Fusion UI、CSS、HTTP/OpenAPI、Controller/Runtime/Scheduler 不改。HTTP/Native/UI 恢复消费尚未接入，不宣称已交付跨窗口启动恢复。

准备从可信 Store 冻结原 Task/Plan/key/阶段/plan revision/generation；每项目一条 pending，另一项目独立。同原请求重读不重编译、不启动。原 StartReservedOnce 成功时在同事务提交 committed/run_id，失败整体回滚；明确封存拒绝晚到 key，确认只承认原 run 身份，不升级 unknown、不扣退预算、无新事件、不释放 held reservation 或伪造 StopProof。详见[合同及迁移/回退](../../../contracts/task-start-journal.md)。

有效RED→GREEN为未提供新Store方法，以及后续两种损坏关联：已存在journal但Task缺失被误当没有记录；prepared状态却已有start_requests映射仍能封存。均先复现失败，再补一致性检查，最终全套通过。初版竞争测试错误假定intent写入扣used_calls；依据原startReserved/ReserveCall合同修正为零调用计数，并新增精确0/1reservation与run断言。损坏JSON fixture最初只移除不可变trigger，合法状态trigger仍阻止raw同状态update；仅fixture移除两项守卫以注入损坏，生产保护未变。补测时PauseTask少传owner导致编译失败已修正。中间日志单独保留，不冒充最终通过。

最终完整Fusion race：600顶层PASS/31顶层SKIP/0FAIL，1097子PASS/10子SKIP；16个有测试包通过。Store116顶层PASS/32子PASS/1helperSKIP，新增12顶层全部PASS。显式Native/真实CN quota及test子进程helper按条件skip，列表见test-results；没有把skip算作真实运行成功。本组件零实际模型/额度调用、没有实际Native窗口操作，Jev off。

Go1.26.3 CLI/GUI/full fusion,nogui vet exit0；最终二进制及日志hash在build-results。GUI日志原有SDK链接警告保留，退出0不等于这些警告已修复。Graph刷新14116nodes/126141edges。没有改Go依赖、原Migration001–007、旧payload hash或新开产品执行准入。

schema8新增表/唯一pending/不可变与合法转移/原回执验证/封存/历史保留/原子committrigger，metadata checksum同事务保存。schema7真实fixture的旧任务、计划、预算、提交回执保持；008失败回滚schema7、可重试、漂移拒绝。现有迁移测试将最新目标7更新为8、未来拒绝8更新为9，原数据/回滚/错误断言保持；历史fixture版本未改。回退需关闭owned宿主并恢复一致性备份，旧schema≤7binary不支持8，不修改user_version降级。

复现使用现有隔离run-go.py执行test-results中的命令，再build-dev.py。临时HOME/XDG和环境白名单不继承日常认证，所有私有fixture由测试创建/关闭。原UI资源与BASE逐字一致，因此不重复无行为变化的浏览器测试。凭据字节排除、链接、JSON/gofmt/diff与source/packet hash验证见validation/artifacts。

下一步：管理API的原请求准备/回读/解决、精确Native路径、原详情区恢复消费者及实际窗口验证。真实Factory/账号/物理池/计费、工程阶段闭环和最终T01–T60继续待完成，未合入main或发布发行版。
