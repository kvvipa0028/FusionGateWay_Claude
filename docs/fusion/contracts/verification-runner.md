# 真实测试执行器与 EvidenceGate

`internal/fusion/evidence.Run` 执行可信宿主冻结的直接可执行测试命令，返回私有 owned Result。`Evaluate` 只接受这个结果；导出的 Record、任意 JUnit 文件、hash 或模型“通过”意见不能构造执行授权。当前为 WP-21 的执行器组件，尚未登记持久 Store receipt 或接入 GLM 阶段发布；多角色 review/acceptance 继续阻断，完整 WP-21/22 和最终验收仍需完成。

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
- 来源身份也必须匹配 owned 的原冻结产物；另一个项目/目录即使代码 TreeHash 相同，也不能复用该执行证据。Task/Run 的持久绑定仍由后续私有 Store receipt 完成，导出 hash 不授予该权限。
- `passed`：owned 执行与停止、零 exit、完整解析以及明确数量/跳过规则均满足。零测试只在冻结项目规则 AllowZero 且 MinTests=0 时允许。

通过证明限于这份命令、工具、代码与标准；不能代表全部需求通过，也不自动完成 Task、派单、提交/合入项目或释放模型 reservation。现有 Store.EvidenceRef 仍只是不可变引用。

## 复现与回退

Go1.26.3，在 implementation 根执行隔离 runner，使用合成测试二进制和私有 artifact，不传真实 key：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/evidence-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=90s -v ./internal/fusion/evidence

PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

有效 mutation 分别移除退出码判定、为输入副本加入可写权限，实际进程负向测试均捕获错误；精确恢复生产代码后回归通过。初始 fixture 的 macOS `/var` 路径规范化和误改只读冻结产物问题均记录，不弱化生产 guard；新版本产物改由合法 Copy→修改→Freeze 生成。

[组件交付证据](../work-items/WP-21/VERIFICATION-RUNNER-01/summary.md)。停用此组件不更改 schema10、不回滚数据库或自动重放测试。执行根由宿主保留用于核验，清理须先确认所有 owned 命令停止。本组件不改 Magpie/Fusion UI、API、模型 Adapter 或 SQL，Jev off。
