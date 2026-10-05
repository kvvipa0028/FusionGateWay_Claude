# WP-12 · WRITE-PROJECTION-01

完成 Codex 写入路线的协议投影与受管开放（协议层全验证、真实 Native 端到端如实现决）；父 WP-12 与整体目标仍未完成。基线 `6cbddfb66e37b0805992cc1636f404d475ff4665`，工作区干净起步；未新增 Agent。

官方 0.160.0 wire 形态由公开源码确认：apply_patch 是 freeform 工具（Responses API 序列化为 `type:"custom"` + lark grammar format），模型工具调用以 `function_call`/`custom_tool_call` 输出项到达，Native 以 `function_call_output`/`custom_tool_call_output` 回传。

## 已实现并验证

- **CallGate 请求投影**：`tools` 从必须空数组开放为受限非空集——每项 function/custom 形态、字段白名单、名称限受控面（apply_patch/view_image/read_file/list_dir/grep，作为 channel 已禁工具的深度防御）、数量 ≤32、schema ≤32KiB；shell 等 API 内建类型与未知字段仍拒绝。
- **输入/输出项**：`inputItem` 接受官方四类工具调用/回传项（有界 call_id/name/payload ≤64KiB）；`outputItem` 与流校验接受 `function_call`/`custom_tool_call` 输出项；`response.completed` 允许纯工具调用应答（空响应仍拒绝）。
- **受管写入开放**：codexadapter `validateSpec` 不再拒绝 Writable；reservation 校验改为双向一致（readonly run 不得带 WriteKey、writer run 必须带）；gateway Client 写入角色（implementation/testing）使用既有 workspace-write sandbox 参数；gateway 事件为 writer 放行 `fileChange` 项；channel 播种的 config.toml `sandbox_mode` 跟随 run 写入意图（strict-config 下压制 thread 参数）。

合成验证：新增 `TestCodexWriteTool*`（请求投影 9 场景含官方形态与拒绝矩阵、响应项 4 场景含完整流）、既有 28 场景 Codex 工厂矩阵（writers 断言翻转为合法启动）、gateway 写角色测试。有效 mutation：删除工具名白名单 → shell 工具被捕获 exit1。三包（codex/codexadapter/bootstrap）race 通过，CLI/GUI/vet 通过。

## 未决（如实声明）

**固定 Native 0.160.0 的 writable turn 端到端未通过**（`TestCodexFactoryPinnedNativeProductLifecycle` 的 implementation/writer_launch 两场景显式 Skip）。独立探查证明：同一 Native 在**无外层 Seatbelt** 时接受 workspace-write thread/turn 并发出模型请求（body 全部字段通过 CallGate 白名单）；在外层 Seatbelt 内 turn failed。已排除：config 播种、fileChange 事件白名单、process-fork、process-exec、CallGate 校验（模型端口未收到请求）。下一步是对 Native 开 debug 日志做外层沙箱差分，定位所需的具体 seatbelt 资源后再放开。**在差分完成前不宣称 Codex 写入端到端可用**；上述协议层开放不影响 readonly 路线（全回归绿）。

调查过程记录（假设-排除链、探查脚本、四层 debug 位置）见 ledger；探查脚本不入库（临时件）。
