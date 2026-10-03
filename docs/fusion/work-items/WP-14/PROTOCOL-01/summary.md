# WP-14-PROTOCOL-01：Claude Code 事件协议

状态：本子工作项 `done`，父工作包 WP-14 仍为 `in_progress`。新增 `internal/fusion/runtime/glm` 的被动协议观察器，未接入生产启动、凭据注入或调度执行。

## 已实现

- 固定 Claude Code 2.1.287、CN Anthropic endpoint、literal GLM model、账号/凭据/工作区和阶段 generation；每帧重新检查控制器提供的当前绑定，旧 generation 不污染当前会话。绑定复制可变字段，宿主标签不覆盖冻结的 `coding_plan` 路线。
- 按实际 Native stream-json 处理 init、message/block、assistant、Read 等已批准工具及匹配的 tool_result。只有完整消息、全部工具结果、最终 Native success 和一致的退出码共同成立才输出协议成功。调用方必须在真实管道结束和进程 wait 后调用 Finish；此接口本身不证明 OS 停止。
- 拒绝模型、版本、工作目录和 session 漂移，未知控制/事件、子 Agent、MCP、额外模型、权限拒绝、重复/孤立/缺失消息、超出 MaxTurns、重复 JSON 字段、Unicode case alias、无效 UTF-8、深度或字节溢出。权限请求仅产生脱敏 deny 回包；实际发送与终止由后续 Adapter 完成。
- 原始 prompt、工具内容和 Native 错误不进入 Outcome。取消仅记录意图；没有最终结果的 SIGTERM/丢失管道保持 `execution_uncertain`。Resume 明确 unsupported，等待持久 Native 恢复合同。

观察器的 MaxTurns 和 ObservedMessages 是 Native 事件数量边界，**不能替代全部实际 HTTP 请求的计数和共享任务预算**。代理重试等请求可能没有对应 message_start。Bash/Write/Edit 的配置也不会授予 OS 权限；生产 Adapter 仍需完成隔离和执行出口准入。StrictLockVerified、BillingVerified、QuotaVerified、UpstreamVerified 始终保持 false。

## 验证证据

新增测试先 RED：模块缺失；Unicode 字段别名与未匹配工具结果被错误接受；缺失 message_stop/assistant/block_stop、重复 assistant 和孤立流转换被错误接受。修复后 14 个 GLM top-level tests 通过；完整 Fusion tagged race 回归 142 个通过、2 个父进程 helper skip（helper 由原生测试执行）。Go 1.26.3 的 Fusion CLI、GUI 编译和全仓 Fusion/nogui vet 均 exit 0。

`testdata/native-2.1.287/tool-free-stream.jsonl` 复用 WP-14-TEMP-01 实际 Native fixture；`read-tool-stream.jsonl` 来自本机同一 hash 的 CLI、synthetic key 和本地 fake Anthropic SSE server。真实 Native Read 工具读取一个自有合成文件，完成两次假上游请求和最终结果；本子工作项真实套餐调用数为 0。

`native-read-characterization.json` 保留 Native 原始 stdout hash；artifact manifest 单独保存仅替换随机根路径后的 fixture hash。两轮请求的 model 均为 glm-5.3、工具数均为 1，Native NumTurns=2。诊断 profile 允许普通启动与 fork，只将 outbound network 限制到假上游端口；worker_isolation_verified=false，不能作为生产 Supervisor StopProof、文件访问隔离或严格锁定证明。

重现命令（仓库根目录、已安装冻结版本、本地假服务、无真实凭据）：

```sh
python3 docs/fusion/work-items/WP-14/PROTOCOL-01/characterize-native-read.py
```

生产 WP-11 profile 的 Native 启动限制、受管进程/子进程停止、全部模型调用出口、真实套餐计费和额度仍需完成。已有的单次真实连接诊断不替代这些门槛，WP-14/WP-17/Gate A 未通过。
