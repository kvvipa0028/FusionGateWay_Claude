# WP-12 · WRITE-PROJECTION-01

完成 Codex 写入路线的协议投影与受管开放（协议层全验证、真实 Native 端到端如实现决）；父 WP-12 与整体目标仍未完成。基线 `6cbddfb66e37b0805992cc1636f404d475ff4665`，工作区干净起步；未新增 Agent。

官方 0.160.0 wire 形态由公开源码确认：apply_patch 是 freeform 工具（Responses API 序列化为 `type:"custom"` + lark grammar format），模型工具调用以 `function_call`/`custom_tool_call` 输出项到达，Native 以 `function_call_output`/`custom_tool_call_output` 回传。

## 已实现并验证

- **CallGate 请求投影**：`tools` 从必须空数组开放为受限非空集——每项 function/custom 形态、字段白名单、名称限受控面（apply_patch/view_image/read_file/list_dir/grep，作为 channel 已禁工具的深度防御）、数量 ≤32、schema ≤32KiB；shell 等 API 内建类型与未知字段仍拒绝。
- **输入/输出项**：`inputItem` 接受官方四类工具调用/回传项（有界 call_id/name/payload ≤64KiB）；`outputItem` 与流校验接受 `function_call`/`custom_tool_call` 输出项；`response.completed` 允许纯工具调用应答（空响应仍拒绝）。
- **受管写入开放**：codexadapter `validateSpec` 不再拒绝 Writable；reservation 校验改为双向一致（readonly run 不得带 WriteKey、writer run 必须带）；gateway Client 写入角色（implementation/testing）使用既有 workspace-write sandbox 参数；gateway 事件为 writer 放行 `fileChange` 项；channel 播种的 config.toml `sandbox_mode` 跟随 run 写入意图（strict-config 下压制 thread 参数）。

合成验证：新增 `TestCodexWriteTool*`（请求投影 9 场景含官方形态与拒绝矩阵、响应项 4 场景含完整流）、既有 28 场景 Codex 工厂矩阵（writers 断言翻转为合法启动）、gateway 写角色测试。有效 mutation：删除工具名白名单 → shell 工具被捕获 exit1。三包（codex/codexadapter/bootstrap）race 通过，CLI/GUI/vet 通过。

## 未决（如实声明）

**固定 Native 0.160.0 的 writable turn 端到端未通过**（`TestCodexFactoryPinnedNativeProductLifecycle` 的 implementation/writer_launch 两场景显式 Skip）。外层沙箱差分已完成并定位为**机制性互斥**：同一 Native 在无外层 Seatbelt 时接受 workspace-write thread/turn 并发出模型请求（body 全部字段通过 CallGate 白名单）；在外层 Seatbelt 内启动即以 `Error: Operation not permitted` 退出（stderr：PATH aliases 警告后致命错）。逐块放开实验证明与权限无关——`mach-lookup` 全开、`process-info`、全局 `file-write*`、`ipc*`、`file-ioctl`/`vnode-attribute`、`process-exec`/`process-fork` 全开乃至 **`(allow default)` 全允许**下依然失败；`sandbox_backend = "none"` 亦被 strict-config 拒绝。结论：codex 0.160 的 workspace-write 会话依赖其自身的 Seatbelt 子沙箱机制（seatbelt daemon/helper，见官方 `sandboxing/src/seatbelt_daemon.rs`），**不能在另一个 Seatbelt 实例内运行**。

**修正与突破**：此前基于 sandbox-exec 复刻的差分结论（"系统级嵌套禁止"）**被证伪**——sandbox-exec 包裹下 readonly 同样启动即死，与生产行为不符，该工具不能用于本差分。改用生产链路内的证据（driver 阶段错误日志）：真实失败是 `StartThread` 的**响应校验 ErrProtocol**——官方 0.160.0 thread/start 的 workspace-write 回显 `writableRoots` 为**空数组**（roots 按 turn 级 sandboxPolicy 携带），而校验要求恰好 `[cwd]`。修复为接受空或 `[cwd]` 两种形态（sandbox type 降级仍拒绝）。修复后 **writer_launch 端到端 PASS**（固定 Native 真实 writable turn 完整生命周期：202 启动、workspace-write 线程、模型往返、durable artifact、停止释放）。implementation 场景（实际执行补丁）仍 Skip，原因已精确定位：channel 播种的 features 全关使 Native **不注册任何模型工具**（探查证明 `code_mode=true` 注册 custom:exec+wait，apply_patch 是 exec 内的官方 PATH 别名而非独立工具）；下一步是给 writable 播种 code_mode 并把 custom `exec`/`wait` 加入受控工具名白名单。此前对 daemon 外置路线的需求判断随之作废：真实障碍是校验 bug 与工具注册 gate，不是嵌套禁止。**readonly 路线不受影响（全回归绿）**。