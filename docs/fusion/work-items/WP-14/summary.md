# WP-14 进度与私有临时目录修复

状态：`in_progress`。本次完成子工作项 `WP-14-TEMP-01`：隔离 Claude Code 内部临时目录，并捕获锁定 Native 版本的 tool-free stream-json fixture。完整 Runtime Adapter 与真实严格执行仍未完成。

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

仍需实现 GLM protocol state machine、受管执行出口、取消/恢复与全部模型调用控制，以及真实套餐/额度/计费核验。WP-14、WP-17、Gate A 和相关最终 T 场景保持未完成。

## WP-14-PROTOCOL-01

新增被动事件协议与两轮 Native Read fixture，子工作项已完成，详见 [协议交付记录](PROTOCOL-01/summary.md)。14 个 GLM tests、142 个 Fusion tagged race tests、CLI/GUI 编译及 vet 通过。协议成功与生产执行准入继续分别记录；原 TEMP-01 的 artifact manifest 是其历史交付快照。完整 WP-14 保持 in_progress。

## WP-14-SYSTEM-DATA-01

修复已定位的 Native 启动数据依赖：生产 profile 增加系统 ICU 与时区目录只读权限，真实系统 ICU 在 Supervisor 内完成枚举，既有隔离反例继续通过。固定 Native 的无工具 loopback 消融诊断证明两个数据目录共同消除请求前启动超时。详见 [系统数据交付记录](SYSTEM-DATA-01/summary.md)。该修复没有开放生产网络、fork 或实际凭据注入；完整 WP-14 与 Gate A 仍未通过。
