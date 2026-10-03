# WP-14-TOOLS-01：Native 文件工具与失败判据

本子工作项 `done`，父 WP-14 保持 `in_progress`。固定 Claude Code 2.1.287 在真实生产 Supervisor/Seatbelt、dual-loopback CallGate 与真实 Store 预算中完成文件读取、修改和创建；同时修复工具执行失败仍被当作阶段成功的协议缺口。全程 synthetic upstream/key/project，真实模型调用数 0。

## 实际工具行为

Read 场景读取项目内 fixture.txt，第二个假模型请求的 tool_result 必须包含实际合成文件内容。Edit 场景使用 Read、Edit，在三个实际 HTTP/Permit/持久预算回合中先读取，再修改，最终回读内容准确。Create 场景只开放 Edit，使用 old_string="" 在已批准的项目目录中创建新文件，两次实际调用后回读准确。正常场景通过完整 Native 协议与实际 exit 0/EOF，全部验证标志仍为 false，不把这些 fixture 作为真实模型准入。

固定 pin 的 bare/restricted 模式下，指定 Write 后 init tools 为 []，模型仍可产生未知 Write tool_use，但验证器拒绝这个工具清单。该场景明确作为 unsupported 反例，不删除 bare/restricted 或放宽校验来启用 Write。文件创建使用实际验证的 Edit；未来 Adapter 必须登记这个具体 pin/mode 的可用集合，不能根据旧 schema 或其他版本推断。

官方 [CLI reference](https://code.claude.com/docs/en/cli-reference) 区分 tools（可用工具集合）与 allowedTools（免提示许可），[bare mode 说明](https://code.claude.com/docs/en/headless#start-faster-with-bare-mode)说明它保留 Bash、文件读取和编辑，同时跳过用户/项目自定义发现。此处保留 bare、restricted、strict-mcp-config、dontAsk、private Native config 与生产 OS 边界，只对明确工具配置 allowedTools；没有启用 bypassPermissions、Bash、MCP 或额外目录。

## 工具失败不能被最终 success 掩盖

真实 Native readonly_create 场景中，OS 拒绝在只读工作区创建文件，但 Native tool_result 带 is_error=true 后仍 emit result/success、exit 0。旧观察器只检查 tool ID 匹配，忽略 is_error，因此 stage 被判 succeeded；原始 RED 日志保留。

新增回归 test 使用实际 Read golden stream，将匹配 tool_result.is_error 改为 true、null 或字符串 "true"，原实现均 RED。修复后只允许省略或明确 boolean false；true、null 或非 boolean 拒绝并保留 execution_uncertain 观察结果，Supervisor 不能判阶段成功。没有替换原有工具 ID、顺序、模型、session、EOF/exit 校验；没有修改 Native 成功事件或削弱只读断言。

## 七个真实 Native 场景

| 场景 | 上游请求 / 持久预算 | 结果与文件证据 |
| --- | --- | --- |
| read | 2 / 2 | succeeded，实际文件内容进入第二个请求 |
| edit | 3 / 3 | succeeded，先读后修改，文件回读准确 |
| create | 2 / 2 | succeeded，Edit 创建新文件并回读准确 |
| outside_read | 2 / 2 | failed，Native permission_denied，禁止内容未进入上游 |
| outside_create | 2 / 2 | interrupted，禁止目录未创建文件 |
| readonly_create | 2 / 2 | failed，观察到真实 is_error=true，未创建文件 |
| write_unsupported | 2 / 2 | interrupted，init tools 不匹配，原文件未变化 |

阳性与只读创建反例都明确允许同一个 Edit 工具，OS workspace 写权限不同；后者不会借 Native 的最终成功文本绕过判据。越界反例由 restricted 工作区许可拒绝，生产 OS 同时不允许兄弟目录读写；不将它们单独声称为绕过 Native 权限后的 OS 反例。每个场景均检查不相关文件保留、禁止新文件不存在、真实 wait/reap/StopProof、终态 grant 失效与 Scheduler.Release。

新增工具 tests 与原受管通道四个场景合并回归，共 2 个 top-level/11 个 subcase PASS；完整 Fusion tagged race 209 个 top-level PASS、5 个 SKIP（两个父进程 helpers、三个显式 Native 入口），Go 1.26.3 CLI/GUI 编译及全仓 vet exit 0。错误值 unit regression 为 1 个 top-level/3 个 subcase PASS。日志、build report 与 owning-commit source hash 位于本目录。

本项没有注册产品 Adapter/API，也没有开展真实 GLM 请求、修改日常 Claude 配置或开放子进程。Bash/Grep/Glob、多进程工具、工程测试 Executor、Native 恢复、真实账号/地区/额度/计费/effort 和 Gate A 仍未完成。执行工具成功不代表工程验收通过，文件产物仍需后续 EvidenceGate 审查。
