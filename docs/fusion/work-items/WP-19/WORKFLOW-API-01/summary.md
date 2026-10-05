# WP-19-WORKFLOW-API-01 · 工作流管理接口

本组件完成当前任务的流程读取/附加、完整设计提交及明确人类批准，固定 Native 桥同步开放。BASE eca928285aec261d2d9a4049b0e5f9eeed5f5491；父 WP-19 和整体目标仍在实施中。原 Magpie 的 475 个 UI 文件与 BASE 一致，三个主资源与 pinned Magpie 一致，没有修改布局、样式或导航。UI 消费下一步仅在原任务详情小范围扩展。

成功响应携带可信 Task、可为空的工作流及完整 ETag；POST 校验完整 Task 条件与当前管理权限。重复不可变文档或批准只回读原决定，不增加事件/预算。普通 continue 不批准，批准不派单；新计划须重新明确批准。未知字段、Worker/stage bearer、非法/重复条件、空或非空额外 key、未登记项目、其他 Task/run、内容/hash/标准冲突均拒绝。

权限在 Server 等待前后及 Store 写事务内复核；撤销导致未提交的 metadata/event 一并回滚。新并发测试复现了 Store 锁→Manager 锁与 Worker 校验→Store 的锁反转；将唯一管理权限标记改为 atomic.Bool，使 ManagementCurrent 无锁，原 grant map 与 StoreValidator 仍沿用原保护。已提交后才撤销时保留真实决定，但拒绝披露失效回执。Native 延续窗口、来源、私有认证、项目、关闭/取消及缓冲响应边界，不开放通用控制代理。

有效 RED→GREEN：HTTP 缺少路由、approval 漏用事务权限检查、Native 缺少通路及静默丢弃 key、HTTP 空 key 被接受、管理权限复核等待 Worker validator 均被测试检出并修复。新增 11 项顶层测试。最终全 Fusion race：17 个包，640 顶层/1151 子测试 PASS，0 FAIL；31 顶层/10 子测试 SKIP，属于原 opt-in 原生诊断或 fixture/crash 子进程入口。Go1.26.3 CLI、GUI 与全仓 vet exit0；原 GUI compiler warning 保留在构建日志，不声称其他平台通过。

OpenAPI 共 40 路径/47 操作，122 条实际 Handler 响应样本覆盖全部 47 个成功操作；官方 3.1 schema 离线校验、13 项输入权限反例及 10 项工作流响应反例通过，原项目/任务/提交/事件/启动记录反例保留。API 生命周期使用真实 Controller/Scheduler/SQLite 和合成 Runtime；Native metadata 使用可信合成停止完成输入，没有执行供应商进程。schema9 与 migrations001–009 字节保持不变。图索引刷新到 14249 nodes、127786 edges；证据与当前源码/二进制 hash 见 artifacts.json。

真实三路线 Gate A、实际 Wails 窗口点击、模型生成/额度、OS 写范围约束、Handoff/测试审查产物与有限返工、最终 T01–T60 均未由本组件完成。Jev off。复现和回退见[接口合同](../../../contracts/workflow-api.md)及[设计合同](../../../contracts/workflow-design-gate.md)。独立提交/推送到 origin fusion/development，不合入 main 或发布；日志归档只去除 ANSI 与行尾空白，原 scratch hash 记录在 test-results.json。
