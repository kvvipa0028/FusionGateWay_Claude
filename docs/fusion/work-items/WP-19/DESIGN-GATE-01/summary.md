# WP-19-DESIGN-GATE-01 · 设计批准核心

本组件完成有限工作流定义、私有持久方案/批准与阶段启动约束；父 WP-19 为 in_progress。BASE feb78b89c6a34dcf65ab464ed3910d9b277b1dae。Management/Native/UI 消费和真实 Gate A 尚未完成，不启用自动闭环，不改原 Magpie UI/CSS/导航，不引入框架。

四种 kind 只允许固定版本角色序列。Task 执行前附加，所需绑定从可信冻结计划取得。设计必须包含目标、范围、约束、接口、验收标准，内容与标准分别 hash；原 completed/stopped/released design run 才能冻结。每个计划版本的人类批准不可变，新计划使旧批准失效。Controller 预检和 Store 原子启动事务都拒绝跳步、未批准实施、重复阶段、失败及未知前序。既有 run 回执仅回读，序列结束不声称整任务验收通过。

schema9 从真实 schema8 fixture 升级，旧 Task/Plan/预算/提交回执保留，不猜测旧工作流。迁移中失败无局部表/版本残留，可修复后重新打开；校验漂移和未来10拒绝。旧 migration001–008 字节未改。旧测试仅更新最新 schema8→9 和未来9→10 的边界预期，历史保存及回滚断言保留。

有效 RED→GREEN：未处理工作流错误时 HTTP500，现为409 workflow_requires_review；暂停/继续后冻结设计被误拒，现仍要求当前完整 Task 条件且保留原 design Run/计划。Scope 控制字符最初被接受，现拒绝。独立删除 Controller 预检及 Store 事务约束分别造成有效失败，恢复原字节后两项通过，证明两层约束各自被测试覆盖。

最终全部 Fusion：17 个包，629 顶层和1127子测试 PASS，0 FAIL；31顶层/10子测试 SKIP，属于 opt-in 原生诊断或 fixture/crash 子进程帮助入口。新增组件 19 个顶层测试通过，覆盖四类定义、方案/hash/范围、SQLite 生命周期、所有启动入口、批准并发/事件失败回滚、方案替换/损坏、计划修订、历史迁移和实际 Controller 调度；其中 Controller/Scheduler/SQLite 是真实逻辑，Runtime/额度/停止输入是合成 fixture。

首次完整回归两项新增测试 fixture 错误：回滚 Task 没配置启动所需预算；失败 Task 原有 readiness 会先返回 ErrConflict。补齐明确预算，按原状态合同检查 failed Task、拒绝重启及恰好一个 run；未放松生产保护。初次 vet 指出三处新测试的外包结构体未命名字段，已改命名字段。最终 Go1.26.3 CLI/GUI/full vet exit0。原编译器 deployment target/枚举转换 warning 如有，原日志保留，不推断其他平台通过。

现有原 Magpie 样式页面在新 schema9 fixture 下的完整浏览器回归 59 PASS、0 FAIL/0 SKIP。所有 UI/CSS 与 BASE 一致，原三个主资源与 pinned Magpie 一致；没有重新测试无改动的模型纯函数，也没有将浏览器结果当成实际 Native 窗口验证。源码/证据/二进制 hash、graph 和文件范围核验见 test-results.json 与 artifacts.json。核心新符号已刷新 graph：14210 nodes、127263 edges，排除 UI/SQL/docs 等非索引资源。

此轮没有实际 Native 窗口操作或真实供应商模型/额度查询，Jev off。设计 scope 只是冻结声明，实际 OS 文件权限/Handoff/证据与返工属于后续消费者和 WP-20–WP-22；批准记录不自动授予这些能力。五阶段成功也不代表最终 T01–T60，通过真实 Gate A、产品 API/Native/UI、工程闭环与最终验收仍为未完成。

复现、字段与回退步骤见[合同](../../../contracts/workflow-design-gate.md)。日志副本仅去 ANSI 控制码和行尾空白，test-results.json 保留原 scratch 与归档 hash；构建日志原样复制。每个组件独立 commit/push 到 origin fusion/development，不合入 main、不发布。
