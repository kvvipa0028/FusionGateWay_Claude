# 真实测试执行器与 EvidenceGate

`internal/fusion/evidence.Run` 执行可信宿主冻结的直接可执行测试命令，返回私有 owned Result。`Evaluate` 只接受这个结果；导出的 Record、任意 JUnit 文件、hash 或模型“通过”意见不能构造执行授权。GLM testing 已接入 owned 执行和持久 Store receipt，详见下文；只读 review 已接入当前证据，acceptance 继续阻断，完整 WP-21/22 和最终验收仍需完成。

## 输入和隔离

输入必须是受控 `workspace.FrozenArtifact`、仓库外独占私有执行根，以及可信 Go `Spec`。Spec 冻结完整 argv、精确可执行文件 SHA256、实际版本查询 argv/预期版本、测试子树、零测试/跳过/最少执行数量规则和整个执行超时。没有自由环境、网络、shell command string 或用户 HTTP 赋权开关。

执行器从真正的冻结产物复制只读输入；测试子树 hash 来自真实 manifest 条目，路径缺失拒绝。每次先在相同沙箱独立执行冻结版本查询，实际退出、完整输出与预期版本不符都拒绝测试命令。启动前与运行中核验 executable、冻结产物、原来源和私有根；结束后再次冻结真实工作副本，与输入 TreeHash 比较。代码或来源漂移不能保留通过结论。

macOS Seatbelt deny default，读取限于输入、独立 scratch、精确 executable 和必要系统动态库；只写 scratch。无供应商 key、Management Key 或继承父环境；HOME/XDG/TMPDIR 指向独立私有目录，LANG=C、TZ=UTC，GORACE 固定为 `atexit_sleep_ms=0`。禁用网络、子进程创建、Keychain/Mach 服务和设备访问。实际 argv 与完整固定环境各有独立 hash 并可私有回读。

首个 backend 支持直接运行的测试 harness，**不支持需要子进程的 shell、Go test 或编译器流水线**。不通过放宽沙箱伪装支持；这些工具链的受控 backend、许可证与停止边界仍须实现/验证。非 macOS 环境返回不可用，不以 host 结果声称 HIL/WCET 或硬件通过。

## 记录和判定

实际进程使用专用 process group；由于内核禁止 fork，实际 wait/reap 与 PID starttime 核验是本 backend 的停止依据。超时、请求取消或来源撤销触发 owned PID 的 SIGKILL，并等待实际终态；没有真实停止就不能判通过。版本查询及测试共用整个超时，最长十分钟。

Record version1 记录命令/工具 pin、实际版本、规则、环境、产物/测试集合/spec/environment/report/stderr SHA256、UTC 起止时间、实际 exit code、执行/停止/截断/中断/输入变化状态。Record/Logs getter 都深复制；JSON schema 只定义导出记录形状，不是可信导入通路。stdout 是本次进程直接捕获的 JUnit，stderr 独立保留；每条流最多 1 MiB，版本查询最多 4096 bytes，截断不能通过。

JUnit 严格解析 testsuite/testsuites 与实际 testcase，核对声明计数、重复案例、失败/错误/跳过；拒绝未知隐藏节点、重复属性、DTD/实体、foreign namespace、尾随第二份报告、负计数和截断 XML。根 testsuites 可以省略汇总属性；每个 testsuite 必须声明与实际案例一致的 tests。支持 properties/system-out/system-err；不静默忽略未知报告结构。

- `failed`：实际非零 exit，或报告中真实 failure/error；模型意见无法覆盖。
- `unverified`：未执行/未停止、中断、截断、解析失败或执行数量不足；默认零测试和全跳过不算通过。
- `superseded`：当前代码、测试子树、冻结命令/工具/规则变化，或原产物/来源失效。
- 来源身份也必须匹配 owned 的原冻结产物；另一个项目/目录即使代码 TreeHash 相同，也不能复用该执行证据。Task/Run 的持久绑定由私有 Store receipt 完成，导出 hash 不授予该权限。
- `passed`：owned 执行与停止、零 exit、完整解析以及明确数量/跳过规则均满足。零测试只在冻结项目规则 AllowZero 且 MinTests=0 时允许。

通过证明限于这份命令、工具、代码与标准；不能代表全部需求通过，也不自动完成 Task、派单、提交/合入项目或释放模型 reservation。现有 Store.EvidenceRef 仍只是不可变引用。

## GLM 阶段接线与持久来源

可信宿主通过 `GLMRuntimeConfig.Verification` 注册独立的命令 Spec 和 Acceptance 列表，Factory 构造时深复制并检查工具 pin。多角色 testing 的 Acceptance 必须与已批准设计逐项一致；缺少配置或不一致时在 Native 启动意图前拒绝。配置没有 HTTP 或模型赋权入口。原独立单角色未注册验证器的模式继续发布 unverified，不宣称测试通过。

Native 实际成功停止后，Factory 冻结测试阶段产物，再运行本执行器；撤销、Task 暂停/取消及来源变化取消实际 owned 命令。只有真实执行并停止的 owned Result 才能用于 `PublishVerified` 和 `RecordVerifiedArtifactAuthorized`。handoff 摘要显示真实 passed/failed/unverified 和 tests_executed，保留 Native output/StopProof；模型文字不能设置这些字段。实际报告/stderr 与完整 Record 保存在私有 immutable stage_artifacts receipt，而非模型输入。

普通 RecordArtifact/RecordArtifactAuthorized 拒绝调用者声明 Verification。可信写入从 owned Result 导出数据，在原 Task/Plan/Run/target/parent/current transaction 内登记 DesignHash/AcceptanceHash；failed/unverified 同时写 needs_review 与独立事件，事件失败回滚全部修改。Native succeeded 仍表示协议执行成功，不改写进程历史。登记不释放预留，只有实际停止、登记和原 Adapter Release 完成后才允许受控读取。

`VerifiedArtifact` 先读取 exact released-run receipt，再用宿主独立登记的 source/ExecutionRoot 恢复真实 frozen artifact，核验 header 与实际判定、当前批准设计/标准和产物/测试/spec。代码或标准变化返回 superseded。导出的 Stored JSON、ValidStored 的结构检查及 EvaluateStored 的数据检查本身均不能证明来源，也不能授权启动后续阶段；可信来源只由这个私有 Store 消费路径建立。[只读 review](review-assessment.md)已接入当前证据；acceptance 与有限返工消费者仍需实现。

## 复现与回退

Go1.26.3，在 implementation 根执行隔离 runner，使用合成测试二进制和私有 artifact，不传真实 key：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/evidence-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=90s -v ./internal/fusion/evidence

PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

有效 mutation 分别移除退出码判定、为输入副本加入可写权限，实际进程负向测试均捕获错误；精确恢复生产代码后回归通过。初始 fixture 的 macOS `/var` 路径规范化和误改只读冻结产物问题均记录，不弱化生产 guard；新版本产物改由合法 Copy→修改→Freeze 生成。

[组件交付证据](../work-items/WP-21/VERIFICATION-RUNNER-01/summary.md)。停用此组件不更改 schema10、不回滚数据库或自动重放测试。执行根由宿主保留用于核验，清理须先确认所有 owned 命令停止。本次接线不改 Magpie/Fusion UI、HTTP API、模型 Adapter 或 SQL，Jev off。

新 Verification 是 ArtifactRecord 的 optional omitempty 字段，schema10 的 SQL001–010 不变；旧 receipt 的 canonical bytes 不变。新增 verified receipt 和 tests_executed=true 的 handoff 需要本版本消费者，旧二进制/旧严格 schema 会安全拒绝，不能视为完整向后读取兼容。回退须在 owned 进程真实停止后恢复同一检查点的数据库与 artifact/ExecutionRoot 备份；仅替换旧二进制不支持读取新证据，也不得删掉 Verification 伪装兼容。[本次接线证据](../work-items/WP-21/VERIFIED-TESTING-01/summary.md)。
