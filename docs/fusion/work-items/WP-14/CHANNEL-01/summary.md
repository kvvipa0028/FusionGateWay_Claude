# WP-14-CHANNEL-01：受管 Claude Code 通道

本子工作项 `done`，父 WP-14 保持 `in_progress`。固定 Claude Code 2.1.287 现已在真实生产 Supervisor/default-deny Seatbelt 内运行，通过 controller-owned loopback、启动后 model capability 和真实 Store 预算访问假上游。产品任务 Adapter/API 尚未注册，真实账号、地区、额度、计费与 GLM effort 执行效果均未准入。

## 通道与授权

NewClaudeChannel 只接受可信 authenticated Handler、PendingModelGrant 与冻结 target，不能传入任意 URL。构造器同时独占 127.0.0.1 和 ::1 的同一随机端口；任一绑定失败时关闭已打开 listener，最多重试八次，不能降级为单栈。两边使用同一个 Handler，连接总槽数 16，HTTP header/read/write/idle deadline 均固定，原始服务端错误日志丢弃。

Supervisor 从真实 Store 重核 task/project/role/attempt/revision/generation 与完整 target，并独占 acquire 通道。model 随机值在启动前仅为未激活的 entropy；PreparedFor 要求 grant TTL 覆盖运行时限加 20 秒，通道最长四分钟。fsync intent、真实 spawn、kernel PID 出生身份、spawn journal、ConfirmStarted 完成后才 Activate，StoreValidator 保持原合同。激活失败保留已知 Handle 并返回错误，真实 TERM/KILL/wait 之后生成 StopProof。

Native 环境只有 scoped 随机值、冻结模型、唯一 controller URL、私有 HOME/XDG/CLAUDE_CONFIG_DIR/CLAUDE_CODE_TMPDIR 和固定关闭遥测/更新的选项；真实 key 仅保留在控制器 CallGate。通道不接受 caller env，也不能与 fixture env 混合。取消、lease 失败、超时、listener 失败与终态均撤销身份。Worker 存活时 Close 拒绝释放两个端口；实际 wait/reap 后才关闭服务，防止允许的地址被另一服务接管。原启动错误路径也必须确认未启动或已 wait 才释放通道。

## 实际 OS 与 Native 证据

最初 numeric IPv4 SBPL 规则被本机 sandbox-exec 拒绝，原始日志保留：`host must be * or localhost in network address`。单独的 network-only diagnostic 实测 localhost:port 同时放行两个 loopback 地址，对有阳性对照的邻端口拒绝。127.0.0.2 无法在本机绑定，不计作沙箱负例。该 diagnostic 的 allow-default profile 只用于匹配语义，未合入生产 profile。源码与报告在本目录，可以从仓库根运行 characterization script；其 alias 字段记录本次宿主观察，环境变化需重测。

生产 C fixture 则经过真实 Supervisor/profile：两边受控端口可达，相邻端口仍拒绝；SecurityServer bootstrap lookup 与 fork 仍被拒绝。没有查询 Keychain item，也没有放开 network-bind、任意 localhost、Mach 或派生进程。真实 Store grant 在确认前不可鉴权，确认后有效；scope 错误不生成 launch journal，激活失败与 listener 失败均真实回收进程。

显式固定 Native 的四个测试场景均通过，executable SHA256 为 `6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea`：

| 场景 | 假上游请求 / 持久预算 | 结果 |
| --- | --- | --- |
| success | 1 / 1 | 协议完整、实际 exit 0、succeeded、StopProof |
| sdk_retry | 2 / 2 | 首次 429，SDK 第二次请求再次 Permit，完整成功 |
| budget_exhausted | 1 / 1 | 第二次请求预算拒绝，不到上游，Native exit 1，failed、StopProof |
| cancel_inflight | 1 / 1 | 调用中取消，授权撤销，Native exit 143，cancelled、StopProof |

成功的两个场景在 Supervisor 确认 EOF 和实际 exit 0 后解析固定 stream-json，观察一个 assistant message；所有 StrictLockVerified/BillingVerified/QuotaVerified/UpstreamVerified 保持 false。四个场景均核验本控制器真实退出证明、终态鉴权拒绝和 Scheduler.Release。HTTP/身份/Transport 均为 synthetic fixtures，真实模型调用数为 0。

## 验证与边界

缺失 API 的 RED、旧 Supervisor 不支持通道的 RED、实际 numeric rule 的 RED 均保留，最终完整 Fusion tagged race 为 208 个 top-level PASS、4 个 SKIP（两个父进程 helper、两个需显式 Native 的入口）。显式 managed Native 单独为 1 个 top-level/4 个 subcase PASS；Go 1.26.3 CLI/GUI 编译和全仓 Fusion/nogui vet exit 0。

可复跑的 Native 命令如下；必须先通过 Go 1.26.3、私有 HOME/XDG、环境白名单和固定 binary hash 核验，不能从日常认证环境直接运行：

```text
go test -race -v -count=1 -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/glm -run '^TestManagedPinnedNativeClaudeChannel$' -args -fusion-native-claude /absolute/path/to/pinned/2.1.287
```

本项仅覆盖当前 macOS/arm64 宿主与固定 Native 的无工具模式。fork/Bash/Grep 等需要子进程的工具、完整工程执行、恢复合同、真实套餐/额度/计费、产品入口和 Gate A 仍未完成。没有对真实 key 发起新请求，没有修改用户日常 Claude 配置，没有声明最终 60 类验收通过。artifact manifest 的源 hash 属于本项 owning commit，后续代码变化应按该 commit 校验历史证据。
