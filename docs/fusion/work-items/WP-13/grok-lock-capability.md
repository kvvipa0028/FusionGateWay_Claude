# 固定 Grok 1.0.48 的锁定能力与缺口

2026-10-03 在独立临时 HOME/XDG/GROK_HOME、空项目、合成 key 和本机假上游中运行固定 Native。真实账号、真实模型调用和真实额度查询均为零。诊断 profile 只允许连接假上游端口，禁止 fork、securityd 和日常配置文件读取，但允许较宽的 Mach lookup；它不是生产 Worker 隔离证明。Native stderr 中 git spawn 被拒绝，三种正常诊断仍退出 0，不能据此宣称工具子进程或工程闭环已通过。

| 检查 | 本机观察 | 能说明什么 |
| --- | --- | --- |
| Native 版本/hash | `1.0.48/b94d5072c95f`，hash 与合同一致 | 精确 fixture 基线 |
| publisher | codesign strict verify 退出 0；Apple Root CA → Developer ID → X.AI Corporation，TeamIdentifier `5Y6N3AJ54S` | 此文件的本机签名核验；未检查 notarization/安装/更新，也不等于账号或路线准入 |
| headless | streaming-json 返回 sessionId/requestId/end/usage | 可解析已观察到的终态 |
| `--no-auto-update` | help 没显示；实际 headless 参数解析接受 | 不能仅用 help 中缺少参数判定 unsupported；未通过网络观察证明所有更新行为 |
| 无效模型 | error 帧、退出 1、假上游请求 0 | 不把未知模型 silently 改为默认模型 |
| `--tools ''` | 主请求 25 工具，包含 spawn_subagent；虽然传了 no-subagents | 空字符串不能表示无工具；这里只证明工具被公布，未实际测试子 Agent 执行 |
| `--tools Read` | 主请求公布 read_file/search_tool/use_tool | Read 单项仍含通用发现/调用工具 |
| 明确 disallow | 加 `--disallowed-tools search_tool,use_tool` 后只公布 read_file | 该冻结版本的声明与请求 tool set 可限定；未验证实际 tool execution 权限 |
| 默认辅助调用 | title 使用 grok-4.6；dashboard summary 使用 fixture-model | 主模型参数不能覆盖全部调用 |
| 固定辅助配置 | models.session_summary=fixture，features.turn_summary=false | 去掉 dashboard 调用；title 和主请求均使用 fixture-model，但实际仍有 2 HTTP |
| 终态调用数 | 上述 2 HTTP 的 end.modelUsage 仍只有 modelCalls=1 | 终态计数不能代替 Scheduler.Permit 的完整调用审计 |
| session/effort | 观察到精确新 session UUID；effort 没有协议级证明 | Resume、effort、跨会话隔离须继续独立验证 |
| 取消/停止 | 此包仅 Cancel 意图与 Finish 状态测试 | 没有 Grok 受管进程树停止/取消后不写证明 |

因此 WP-13 仍 in_progress，真实 Grok 生成路线保持未准入。后续必须在真实获准订阅路线验证辅助请求、模型/effort/账号/计费、完整调用许可、权限与取消、准确会话恢复。不能通过普通 API key、网页 Cookie 反代、primary-only 解释或 fixture true 标记代替这些证据。

官方 [headless 文档](https://docs.x.ai/build/cli/headless-scripting) 说明结构化输出和关闭更新；[settings 文档](https://docs.x.ai/build/settings) 说明 GROK_HOME。其会话参数概述不替代本机 help：当前 session-id 仅创建新 UUID 会话，恢复应使用准确 resume UUID，不能使用标题或 continue。参考源码独立固定于 [xai-org/grok-build@2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8](https://github.com/xai-org/grok-build/tree/2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8)，SOURCE_REV=`559751fdcec02d413e4c57c8832ab275e4f44980`，与本机 b94d5072 不同，只用于定位调查，不能当成 Native 等价源码。

证据：[PROTOCOL-01](PROTOCOL-01/summary.md)。旧 WP-08 未检查 publisher 的记录保持历史事实，本次签名报告不回写旧报告、也不修改准入 flags。

2026-10-04 [CALLS-01](CALLS-01/summary.md) 新增每 HTTP Gate：实际固定 Native + 真实 Store/Scheduler + 合成 inspection 证明正常 title/main 两次记账、SDK retry 三次记账；默认 title 模型、第二次预算不足和隐式 tools 被拒绝。这里只完成本机组件诊断，不回写任何真实准入 flag，未支持生产 Native transport、受管取消、工具或 Resume。

[TOOLS-01](TOOLS-01/summary.md) 实测项目内读取成功、OS 越界拒绝后 end_turn/exit0，以及 readonly工具可读取自己私有 HOME 中的合成 config key。Read/dontAsk 与私有 HOME 本身不是配置保密证明。现有 Gate 实测在工具响应进入 Native 前拒绝，未启用真实工具。

[READ-01](READ-01/summary.md) 在显式只读scope中实际完成项目文本读取，title+两轮main三次持久化预算/endcalls2；相同scope在工具开始前拒绝私有config读取。范围限合同中的单个文本Read，Unix path重查不是生产Worker隔离或并发物理读取证明。未注册真实路线。

[CHANNEL-01](CHANNEL-01/summary.md) 增加实际1.0.48在生产Supervisor/profile中的停止证据。该证明只覆盖本控制器的进程/端口/grant/reservation生命周期；冻结模型HTTP Gate与StopProof不能替代真实X上游身份、subscription计费、quota/pool、准确Resume和工程验收。未注册真实Grok Worker，Jev off。

[ADAPTER-01](ADAPTER-01/summary.md) 将上述组件组成公共readonly Runtime Adapter，固定argv/session/prompt与真实预算/停止链。它只接受effort=none，显式其它档位拒绝，Resume/write仍unsupported；真实NativeForwarder/账号/Heavy/计费/Quota证明和产品注册未完成。不能将Observer flags自动升级为独立严格锁定结论。

[RESUME-PROBE-01](RESUME-PROBE-01/summary.md) 调查固定 Native 的明确 UUID 恢复、私有会话目录复制、缺文件、模型/cwd 变化和历史 Read。15 个实际行为断言通过：准确 UUID 能保留旧对话，但 transcript 缺失可空历史 exit0，model alias 可覆盖原模型，旧工具输出不在 stdout 重放而进入 HTTP。因此产品 Resume 继续 unsupported；完整归档/冻结绑定/历史授权及真实受管恢复按 [grok-resume.md](../../contracts/grok-resume.md) 实施，不能把诊断成功升级为 capability 或准入。

[RESUME-01](RESUME-01/summary.md) 已接通可信内部 ResumeCheckpoint：完整私有归档核验、新 prepared run/Root/grant/端口、准确原 UUID、旧/新 Read 及逐 HTTP 预算、HMAC 准备 mapping。12 个新增场景覆盖 6 个成功恢复、取消、Quota 漂移失败、4 个新进程启动前拒绝，其中成功场景包括中文 cwd 和完整 Store/Archives/Manager/Adapter 重开。联合兼容性验证为 43 个场景、50 次实际 Native 启动；另有 7 个控制器生命周期场景。上述历史段落描述各工作项当时的边界；当前可信内部恢复已完成，通用无 Spec/ref 的 Resume、产品 API/Worker、真实 X upstream/subscription/Quota 与整个 Source 准入仍待完成。合成诊断不升级真实 capability 或准入 flags，Jev off。
