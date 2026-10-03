# GLM-PROBE-02 本机连接复测

2026-10-03，按用户“key 已准备好，继续”的指令，读取已有仓库外私有凭据，复测 Claude Code → GLM CN Coding Plan 的无工具诊断。没有重新录入或更换 key。

冻结的 Claude Code `2.1.287` 可执行文件 hash 与已登记版本一致。临时 HOME/XDG/Claude 配置、空项目目录、环境变量白名单、关闭工具/MCP/会话保存，固定 CN Anthropic 地址与 `glm-5.3`，返回 `FUSION_GLM_OK`，退出码 0。日常 `~/.claude/settings.json`、`~/.claude/.credentials.json`、`~/.claude.json` 的前后内容一致；未输出文件内容或真实 key。

此项执行一次 Native 诊断。没有在 HTTP 层计数 SDK 的尝试次数，因此不能声称只发生一次上游 HTTP 请求。Native 自报输入 54、输出 42 tokens，不作为官方计费证据。`glm-5.3` 只用于此次诊断，不改变五角色绑定。

[脱敏结果](live-probe.json)证明本次连接可用。`billing_verified=false`、`quota=unknown`、`upstream_reported_model=unknown`、`strict_locked_verified=false` 继续保留。WP-14、WP-17 和 Gate A 尚未完成，不把本次诊断当作受管任务执行验收。Jev 保持 off。
