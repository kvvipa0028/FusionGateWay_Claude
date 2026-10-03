# WP-15-START-ONCE-01 启动意图幂等事务

状态：子项 done；WP-15 in_progress。新 `StartReservedOnce` 返回 Run/Created，持久化 task、role、plan revision、expected generation 的请求 hash 与唯一 run 映射。并发相同请求只有一次新建，其他请求只读原 run。Target 来自可信冻结计划；重试不能重新指定目标、改写原预留、刷新租约或退还已消耗预算。

新 key 必须匹配当前 task generation。即使前一阶段成功并释放容量，旧 generation 仍不能新建 attempt。控制器重启后映射保留，unknown 的重读不授予执行权限。旧 StartIntent/StartReserved 拒绝带 key 的请求，避免调用方把重读当作新启动。

schema 003 与映射 checksum 独立保存，001/002 没有改变。已验证 schema 2 的任务、政策、消耗预算和历史事件保留、checksum 异常拒绝。旧 schema 1 升级测试的最终版本断言由 2 更新至当前 3，原迁移校验仍保留；这是新 schema 的要求，不以当前输出替代需求。向旧 binary 回退必须恢复对应 schema 的数据库备份。

8 个新增定向测试覆盖并发、终态重试与已消耗预算、键/Target 冲突、unknown 重读、终态后的旧 generation、不可修改映射与 closed store、映射失败的 intent/reservation/event 整体回滚，以及旧数据迁移。最初缺少类型/API 的 RED 和实现期间 import/测试 oracle 错误的失败日志保留。Reservation 只返回 held 预留，测试改为检查实际持久 released 行和 API 的 ErrNotFound，未更改此生产合同。最终 9 个定向测试（含原 schema 1 回归）通过，Fusion tagged race 231 PASS、7 SKIP；CLI/GUI 编译和全仓 vet 通过。跳过的是平台/原生 fixture 条件，不代表实际三路验收。

此子项没有真实模型调用。当前轮另有用户授权的 GLM-PROBE-02 连接诊断，证据单独记录。尚未实现产品启动 endpoint、可信控制器、暂停/恢复或阶段返工政策；全体 60 类最终验收保持原状态，Jev off。
