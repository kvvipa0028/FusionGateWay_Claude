# WP-14-QUOTA-READER-01：大陆个人套餐额度 Reader

子工作项 `done`，父 WP-14 保持 `in_progress`。新增可信服务构造的 `glm.QuotaReader`，固定 `https://open.bigmodel.cn/api/monitor/usage/quota/limit`，复用 WP-09 `AdaptGLM` 与 Broker 的数据合同。端点与 Authorization 形式来自当前固定 Magpie `1a50db1` 的 `internal/provider/planquota.go`，不是公开稳定 API 承诺。不使用旧 provider 全局配置、浏览器 Cookie、Global 站点、团队自动探测或按量 API fallback。

`QuotaReaderConfig` 必须提供精确 CN/bigmodel account/workspace/generation、coding_plan target/credential identity、独立且有时限的可信 QueryAllowed 与 LoadCredential。构造复制 target；查询前拒绝 scope/授权漂移，加载 key 后、返回 header 后与读取完成后再次检查。每次请求最多十秒，body 64 KiB，header 16 KiB，单 host 两个连接。只执行一次 GET，使用独立 HTTP/1 Transport，不继承代理、不复用连接、不跟重定向、不透明解压、不隐藏重试。Go 1.26.3 Transport 源码与本机真实 TLS 断连/redirect 负例同时核验此行为。

错误只返回静态 QueryError：401/403 auth_required，404/405 unsupported，其余未知/身份变化拒绝；不会输出上游错误文本或 key。严格 JSON 拒绝重复及大小写冲突字段、非法 UTF-8、超过 32 层、尾随值、缺少 success/data/limits、超过 32 个窗口或回显 key。Date 保留来源年龄，没有 Date 时保守采用本次请求开始时间；Age 非零拒绝为 stale，不将来源缓存变成新观察。额度缺字段不会成为零；MCP 窗口与模型窗口分开。

返回快照保留原始窗口，并始终 `Complete=false` / `Pool.Verified=false`。成功查询不会证明真实账号拥有者、共享物理池、模型/effort/计费或生成准入；不能直接供 Scheduler 启动。还需独立可信 collector 与 Registry 额度用途证据、真实 Credential service 和产品注册。

## 验证

- 缺少 Reader API 先 RED；五个 Reader 单测包含精确构造、固定查询、七种 secret/network 前拒绝、十六种响应/漂移/错误、两个真实 TLS 单次发送负例，最终 PASS。
- `TestGLMQuotaReaderLiveCN` 默认 SKIP，仅显式传本机私有 key 文件才运行；检查仓库外、目录/文件 owner 和权限、canonical/no-symlink/O_NOFOLLOW 与文件身份，诊断 loader 不充当生产凭据服务。
- 本轮仅执行一次真实 CN quota GET，成功观察两个 Coding Plan 模型窗口，used_percent 为 0、93。其标签只是接口观察，不推断剩余 tokens 或与早期模型调用的账单关系；snapshot status=unverified。真实模型调用为 0。
- 完整 Fusion tagged race：217 个 top-level PASS、7 个 SKIP、0 FAIL；SKIP 包括父进程 helper、显式 Native 场景和新真实额度场景。Go 1.26.3 CLI/GUI 编译、Fusion/nogui 全仓 vet exit 0。
- 原始日志、来源 hash、脱敏检查及构建结果见同目录 JSON。真实 key 未进入源码、日志、文档或提交。

官方 [接入工具](https://docs.bigmodel.cn/cn/coding-plan/tool/others) 将 Claude Code 列为支持工具，并给出 CN Anthropic Messages Base URL；[使用须知](https://docs.bigmodel.cn/cn/coding-plan/usage-notes) 限定订阅人及指定工具环境。本项只做额度读取，不据此自行确认 Fusion 二次封装用途或真实工程计费。Jev 仍 off。
