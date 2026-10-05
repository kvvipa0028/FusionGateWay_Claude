# WP-21-VERIFICATION-RUNNER-01

基线 `3043eb5bcd4df6ead34e11a85377f5ef3a5ac75b`。完成真正的本地直接测试执行器与 owned EvidenceGate，WP-21 继续 in_progress。

## 交付行为

可信冻结命令/工具 hash 与版本查询在私有产物副本上实际执行；代码、测试子树、命令、规则和固定环境分别绑定。只读输入、scratch 正控制、网络/子进程/外部私有文件负控制由实际 sandboxed 进程验证。取消、超时和原来源撤销停止 owned 进程并核对真实 reap/starttime；末端重新冻结工作副本，避免改码沿用旧证据。

结果记录真实 exit code、UTC 时间、完整有界 stdout JUnit/stderr 的 hash 与状态，导出 getter 深复制。严格报告解析和明确项目数量规则决定 passed/failed/unverified；修改代码/测试集合/标准后 superseded。任意 JSON、文件或 LLM 文字无法构造 owned Result。合同、配置与复现见 [verification-runner.md](../../../contracts/verification-runner.md)；[evidence.schema.json](../../../contracts/evidence.schema.json)只定义导出形状，不是导入授权。

## 验证

Go1.26.3，最终 targeted race：9 个顶层、15 个子测试 PASS，0 SKIP/FAIL；实际执行版本查询和各报告/硬失败/边界/中断场景。Fusion 回归：679 个顶层、1238 个子测试 PASS，33 个顶层、11 个子测试 SKIP，19 packages，0 FAIL。最后增加 record capture 和可达 loopback 的 outbound 正/负控制，再次运行完整 evidence package 通过；生产代码与前述 Fusion 回归一致。

CLI、GUI build 与全仓 go vet 均 exit0。真实 captured Record 通过 Draft202012 schema，五个错误类型/身份输入拒绝。源文件和 artifact hash、日志原始/归档 hash、检查边界见 [artifacts.json](artifacts.json)与 [test-results.json](test-results.json)。原 Magpie UI/CSS、现有 API、Store/SQL、Controller 与模型 Runtime 均未改动，Jev off，真实供应商/key/quota 调用为0。

## 失败与有效证据

- 缺少 Spec/Run/Evaluate 的编译 RED 保留。
- 首轮 fixture 传入 macOS `/var` 非规范路径，真实 workspace guard 拒绝；fixture 改为 canonical 路径，不削弱 guard。
- 随后 fixture 试图直接改写只读冻结文件而被权限拒绝；修正为合法 Copy→修改→Freeze 的新产物，旧证据明确 superseded。
- 独立移除 exit code gate 后，真实 exit7 加成功报告错误 passed，被测试捕获 FAIL；精确恢复后通过。
- 独立赋予输入可写权限后，实际 boundary worker 写入并 exit85，被测试捕获 FAIL；精确恢复后通过。该场景另有可读输入/可写 scratch 正控制，不能用全面执行失败冒充隔离。
- 最后来源身份审计发现异源但等内容的 artifact 能复用原证据，新增实际进程测试确认 RED。Evaluate 补上 owned 冻结产物精确路径和双方 provenance Current 核验，随后 targeted、Fusion 回归及构建/vet 全部重跑通过；不依赖 hash 等同来源。

## 剩余工作与回退

本 backend 禁止 fork，支持直接可执行 harness；需要子进程的 shell/Go/compiler pipeline 不受支持。受控工具链、持久 receipt 和产品阶段接线仍是必须完成的后续任务；当前尚未允许多角色 review/acceptance，也没有修改任何 Task 完成状态。真实供应商/项目、HIL/WCET 和最终 T01–T60 未验证，不扩大完成口径。

回退停用执行器，确认 owned 命令真实停止后按宿主政策保留/清理其私有执行根；不回滚 schema10、不重放命令、不替换模型/账号，不提交合入用户项目。完整测试、审查验收与有限返工目标保持不变。
