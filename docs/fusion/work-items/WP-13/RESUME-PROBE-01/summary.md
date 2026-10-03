# WP-13-RESUME-PROBE-01：固定 Native 会话恢复调查

基础 HEAD：`b45d895ae4c34ca7fd75c0903c5e3f1e444c1f5f`。本诊断子项 done；Native Resume 实现和 WP-13 仍 in_progress。没有生产代码、准入或 API 变更，Adapter/Session/Supervisor 的 Resume 继续明确 unsupported。

[可复现脚本](characterize-resume.py)在固定 Grok `1.0.48/b94d5072c95f`、实际 SHA256 校验后，用私有临时 HOME/GROK_HOME、umask 0077、sandbox-exec 和唯一合成 loopback 服务执行 15 次 Native。15 个行为断言及实际 wait 通过，共 15 次合成 HTTP；没有真实模型、额度或日常认证。脚本会创建并销毁自己的项目、会话与配置，不保留 prompt/原始请求/原始 stdout/stderr，只记录类型、布尔值、字节数和合成 usage。这个 sandbox 允许整个诊断根写入和除 securityd 外的 Mach lookup，比生产 Supervisor profile 宽；不提供生产 StopProof，也不代替生产权限测试。

| 场景 | 固定 Native 的实际行为 | Fusion 的实现要求 |
| --- | --- | --- |
| A、B 两个会话，明确 `--resume A_UUID` | 原 UUID、A 对话及 assistant history 保持；不会拿 B；恢复请求为 1 次 HTTP | 只允许冻结准确 UUID，禁止自动选择 |
| 无 UUID 的 `--resume` | 选择最近的 B | 不作为恢复执行合同 |
| 缺 UUID、重复新 UUID、resume + 新 session-id 无 fork、坏 summary | exit 1，0 HTTP，无成功 end | 不改成重建会话或 fallback |
| 只复制 A 的会话目录，重新生成私有 config | 原 UUID/旧对话恢复，1 HTTP；无需复制 config、agent_id、全局日志或 SQLite | 独立会话归档；账号授权仍须另证 |
| 删掉 `chat_history.jsonl` | 从其余记录恢复 A 对话，仍成功 | 单文件不是完整性合同 |
| 同时删掉 `chat_history.jsonl` 和 `updates.jsonl` | exit 0、原 UUID、1 HTTP，但没有 A 或 assistant history | 所有归档文件及内容须校验完整，缺失就拒绝启动 |
| 从另一个 `--cwd` 发起 `--resume A_UUID` | 接受参数，请求仍含旧目录上下文 | 独立核对 canonical cwd/项目身份，不能只相信传入 argv |
| 恢复 A 时指定另一个 model alias | 实际 HTTP/end 使用另一个合成模型 | 在启动及每调用核对 frozen model/account/route/effort |
| 先读 owned fixture，再恢复该会话 | 原 tool result 进入下一次 HTTP，但 stdout 没有重放旧 tool_call | 持久历史授权与完整关联检查，不从缺少事件推断无历史工具 |

end.num_turns/modelUsage/usage 在该诊断按本次调用报告：文本新建与恢复均 1 turn/1 Native modelCalls，首次 Read 为 2 turn/2 Native modelCalls，恢复 Read 为 1/1；新建还另有标题 HTTP。实际 HTTP 数继续独立计量，不把 Native 标签当作全部调用或账单。

初始磁盘是 `GROK_HOME/sessions/<percent-encoded canonical cwd>/<UUID>/`，有 summary、chat_history、updates、prompt_context、system_prompt、tool_definitions、usage、events、signals、rewind_points 及 lock 文件；目录外还有 prompt_history/搜索 SQLite。结果只输出 `<encoded-cwd>`/`<session-a>`。本次临时会话文件均 0600，项目文件树和 owned 文本保持不变。配置和 logs 不属于迁移的会话目录；这不证明其他版本、真实认证、未完成工具、压缩/记忆/子 Agent 的归档语义。

[native-probe.log](native-probe.log)与[test-results.json](test-results.json)保存实际结果；[artifacts.json](artifacts.json)记录脚本/合同/证据哈希。JSON、Python AST、日志与结果计数、链接、凭据排除和 diff 检查通过；生产源码未变化，保留 b45d895 拥有的全量 race/CLI/GUI/vet 证据，不描述为本项重跑。Jev off，真实 model/quota calls 0。

复现命令（result 必须是尚不存在的文件；脚本拒绝覆盖）：

```sh
python3 docs/fusion/work-items/WP-13/RESUME-PROBE-01/characterize-resume.py \
  --cli /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 \
  --result .fusion-dev/implementation/resume-recheck-new.json
```

下一步实施合同见 [grok-resume.md](../../../contracts/grok-resume.md)：可信 stopped run 的完整归档、所有权和目标绑定、重新派生 grant/预算、历史工具授权、真实受管启动/停止与持久恢复。当前诊断不能关闭 T15/T31/T32/T35/T44/T56/T59 或完整最终 Gate。
