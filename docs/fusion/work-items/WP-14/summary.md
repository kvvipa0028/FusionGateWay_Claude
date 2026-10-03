# WP-14 进度与私有临时目录修复

状态：`in_progress`。最新完成子工作项 `WP-14-TOOLS-01`：验证固定 Claude Code 的受管 Read/Edit/文件创建，并修复 Native 工具失败仍被判成功的问题。此前 CHANNEL-01 已接入 Supervisor、受控双栈 loopback、启动后 grant 激活与真实 Store 预算。下文保留各轮证据边界；完整产品 Adapter、真实路线准入和 Gate A 仍未完成。

## 修复与验证

`scripts/fusion/glm-claude-probe.py` 新建运行私有的 `native-tmp`（0700），显式设置 `CLAUDE_CODE_TMPDIR`，不继承父进程的同名变量。`HOME`/`TMPDIR` 单独隔离不足：实际 Native 2.1.287 在受限 profile 中仍尝试打开 `/tmp/claude-501`。没有放开该共享路径，没有修改或清理日常 Claude 临时数据。

新增测试先 RED：未设置 Native temp override 时 fake CLI 拒绝，connection_verified=false。修复后 **9 个 GLM diagnostic tests 通过**，包括隔离环境、错误脱敏、模型错配和超时；本轮没有重复真实 GLM 请求。

[官方环境变量说明](https://code.claude.com/docs/en/env-vars)明确 `CLAUDE_CODE_TMPDIR` 控制内部临时路径，Unix 会在其下添加 `claude-{uid}`。这个修复针对诊断入口，不代表 WP-11 的生产 Worker 已能启动 Claude。

## Native stream fixture

`native-stream-characterization.json` 与 `native-stream-fixture.jsonl` 来自本机 **2.1.287** 的真实 CLI、空私有项目、synthetic key 和本地 fake Anthropic SSE server。固定 no tools/no slash commands/no Chrome/no session persistence；唯一假上游请求模型 glm-5.3、工具数 0，真实模型调用数 0。

这次是受信任的无工具协议诊断，**不是生产 Worker 隔离测试**。诊断 profile 允许普通本机启动操作和子进程，仅将 network-outbound 限定到 fake server 的 localhost 端口。保留 worker_isolation_verified=false、strict_lock_verified=false；Popen wait/reap 不能替代生产 Supervisor StopProof。先前 WP-11 单进程 profile 在设置私有 Native temp 后仍启动超时；本次没有将放宽的诊断 profile 合入生产代码。

fixture 仅替换随机私有项目根路径为 `<fixture-root>`；session/uuid 是本次假服务诊断生成的临时 Native ID，不是用户账号。报告 stdout_sha256 是替换前 Native bytes 的 hash，artifact manifest 保存发布后 fixture 的独立 hash。

Native stream 出现 system/ui_invalidate、init、status、stream_event、assistant、result/success。init 中仍列出 builtin agents/plugins，不能仅凭 bare 或 tools=[] 就断言所有子调用受到控制。result.modelUsage 的 firstParty/costUSD 是宿主标签/估算，不是 GLM 套餐计费证明；下一步协议层必须保留这一边界。

## 版本与剩余工作

本轮发现 `~/.local/bin/claude` 已指向 2.1.288。2.1.287 的冻结文件仍存在且 hash 与 WP-08 相同；characterization 直接使用该文件。未把 symlink 更新视为旧任务版本已更新，也没有把 2.1.288 标为已准入。

TEMP-01 当时仍缺 GLM protocol state machine 与受管执行出口，后续子项分别补充。当前仍需产品 Adapter 注册、工具执行能力、恢复合同和真实套餐/额度/计费核验。WP-14、WP-17、Gate A 和相关最终 T 场景保持未完成。

## WP-14-PROTOCOL-01

新增被动事件协议与两轮 Native Read fixture，子工作项已完成，详见 [协议交付记录](PROTOCOL-01/summary.md)。14 个 GLM tests、142 个 Fusion tagged race tests、CLI/GUI 编译及 vet 通过。协议成功与生产执行准入继续分别记录；原 TEMP-01 的 artifact manifest 是其历史交付快照。完整 WP-14 保持 in_progress。

## WP-14-SYSTEM-DATA-01

修复已定位的 Native 启动数据依赖：生产 profile 增加系统 ICU 与时区目录只读权限，真实系统 ICU 在 Supervisor 内完成枚举，既有隔离反例继续通过。固定 Native 的无工具 loopback 消融诊断证明两个数据目录共同消除请求前启动超时。详见 [系统数据交付记录](SYSTEM-DATA-01/summary.md)。该修复没有开放生产网络、fork 或实际凭据注入；完整 WP-14 与 Gate A 仍未通过。

## WP-14-CALLS-01

新增 authenticated Messages CallGate、共享 run 调用互斥、逐 HTTP Permit 与响应 model 核对。实际固定 Native 的成功和 429/SDK retry 诊断分别有 1/2 次 HTTP 与 Permit，协议完成；持久 Store 预算另经真实 loopback 验证。详情见 [调用出口交付记录](CALLS-01/summary.md)。该 Handler 尚未注册生产；Native 环境、启动后 grant 交付和实际 Transport/额度/计费仍待接线与验证。

## WP-14-GRANT-01

新增无授权的 Native 随机值准备与 ConfirmStarted 后一次性激活原语，原 StoreValidator 保持不变。真实 Store 测试证明 starting 拒绝、running 允许、finished 再拒绝。详见 [启动身份交付记录](GRANT-01/summary.md)。生产 Supervisor/Native 启动通道尚未接入。

## WP-14-CHANNEL-01

原生受管通道已接入 Supervisor。macOS 的 localhost 沙箱规则同时允许 IPv4/IPv6，因此控制器必须占有两边同一端口，Worker 存活时不能释放。真实固定 Native 无工具测试完成正常调用、SDK 重试、预算耗尽和调用中取消；使用 fake upstream 和 synthetic key，真实模型调用数 0。详见 [受管通道交付记录](CHANNEL-01/summary.md)。本项不注册产品任务 API，不升级实际账号/地区/额度/计费/effort 证据，父 WP-14 保持 in_progress。

## WP-14-TOOLS-01

固定 Native 在同一生产边界内完成 Read、Edit 和 Edit 创建新文件，实际工作区内容回读通过。越界读、越界创建和只读创建均不能成功；Native bare 模式的 Write 没有出现在实际 init tools 中，不能准入。发现只读创建失败后 Native 仍返回最终 success，补充 tool_result.is_error 检查后由 RED 转 GREEN，工具错误不再被成功文本掩盖。详见 [文件工具交付记录](TOOLS-01/summary.md)。完整 Adapter、真实准入、工程测试 Executor 与 Gate A 仍未完成。
