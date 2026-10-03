# 真实接入核查清单

已获用户授权接入本机三条路线。当前 GLM key 已私有录入，Claude Code 工具关闭的连接诊断已完成；不重复执行收费诊断。OpenAI/X 官方登录尚需本人在浏览器完成。以下项目在 WP-12–WP-14 的隔离 Adapter 准备后执行；当前不手动覆盖日常配置。

1. 固定 CLI executable/version/hash、官方来源、私有 HOME/XDG/auth root 与 workspace。版本变化先重新验证 Adapter，禁止未核验 latest 指针自动改变冻结任务。
2. OpenAI/X 由本人完成官方登录，将认证落在 Fusion 的私有 Runtime 目录。不得导入浏览器 Cookie、共享 Keychain、日常 CLI auth 或第三方插件凭据；读回实际账号/workspace、认证方式、权益和地区。API key 模式不能冒充 subscription。
3. GLM 只从已登记私有文件读 key，固定 CN Claude Code 路径。本人可在官方费用明细核对已有诊断的抵扣资源包；只需报告结果类型，不提交 key、Cookie 或完整账单。
4. 先分别核验额度查询的来源、用途和身份，再查询最小只读数据。空字段、unsupported、auth_required、unknown、stale 与真正零额度分别记录。不调用充值、通知邮件或额度 reset 消费接口。
5. 先用本地假上游/假 Runtime 核验所有请求的 model/account/effort、摘要/子 Agent、重试、MCP、插件与工具权限；任何无法控制的调用阻断 controlled_calls。用户未接受 primary_only，不自动降级。
6. 原生最小真实生成探针只发送固定非项目敏感文本，关闭工具，单次请求，不 fallback。执行会消耗该订阅额度；记录 requested/resolved/upstream、native session、trace 与账单路径。GLM 已有工具关闭诊断可复用，但尚缺路线准入证据。
7. 最后在临时合成工作区进行真实文件/测试闭环，记录批准的写入边界、命令、报告、取消和恢复。生产项目试点在 Gate C 与本人验收后进行，不先接入日常工程。

失败时停用该 exact route revision，保留脱敏证据；身份变化、新 Runtime 版本或费用不明确不得继续旧 run。其他离线工作继续。Probe 返回成功仅证明本次请求，不代表额度、所有内部调用、严格锁定或最终 Gate。

当前可复现的 GLM 连接诊断命令见 private-credentials.md / GLM-PROBE-01；本清单不要求立即重跑。OpenAI/X 隔离登录入口和完整 smoke 由后续工作包提供，当前只记录候选与缺口。
