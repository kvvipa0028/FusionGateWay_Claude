# GLM / Claude Code 当前集成核验

WP-14 状态 `in_progress`；生成路线不因这些离线证据自动变成 admitted。用户已选择中国大陆 Coding Plan、API key 与 Claude Code，Jev off。

| 要求 | 当前证据 | 状态 |
| --- | --- | --- |
| 固定 Native 与独立 HOME/config/tmp | 2.1.287 executable SHA256、生产 Supervisor/profile、TEMP-01/CHANNEL-01 | 已验证本机固定 pin |
| 精确模型与独立目标/凭据身份 | 每 HTTP Gate 及 Native 协议；Adapter loader Identity 核对 | 合成路线通过，真实身份未核验 |
| 私有凭据加载 | FileCredential 的逐层无链接/仓库外/UID/权限检查、冻结 inode/content；Native 与真实额度诊断使用相同服务 | 内部服务已实现；只冻结本地身份，不证明上游账号 |
| 固定生成传输 | CNTransport 的固定 CN Messages 单次 HTTPS 与实际 Native/TLS fixture | 内部传输已实现；真实上游生成与准入证据未完成 |
| 全部宿主模型调用计数 | 实际 Native HTTP、SDK retry、CallGate/Scheduler/Store 预算 | 已验证本机固定场景，真实路线仍需准入 |
| Native 文件工具 | Read、Edit、Edit 创建；越界/只读负例与 is_error 修复 | 本机合成文件通过；Write/Bash 等未准入 |
| 进程取消/退出 | 真实 TERM/KILL/wait、kernel 出生身份、StopProof/Release | 本机受管场景通过 |
| Native 适配器 | glm.Adapter 的可信构造与公共 Runtime 接口、七个真实 CLI 场景 | 已实现内部 Adapter；产品控制器未注册 |
| 真实连接 | 早期独立诊断成功连接已保存的 CN key | 只证明当时连接，不证明工程准入 |
| 真实账号/地区/模型/effort/计费 | 尚无满足 Registry 完整 Evidence 的实时证据 | 未完成，不能借 Native costUSD/firstParty 推断 |
| 当前额度与共享 quota pool | WP-09 语义/Broker、内部 CN 个人套餐 Reader；一次真实 GET 观察模型窗口 0%/93% | Reader 与 quota-only 产品 CLI/API 已接入；物理池/完整性仍未证明，快照保持 unverified |
| 恢复、Handoff、完整项目阶段与 Gate A | 后续工作包与最终验收 | 未完成 |

这里的假路线明确使用 synthetic identity/quota 与本机 TLS 假上游，禁止把测试 Inspector 的 true 标志送进产品 Registry。真正执行前需完成适用的套餐/编码宿主/二次封装用途核验、真实上游的版本绑定证据、物理额度池与完整性核验，以及已实现 CNTransport/Credential service/QuotaReader 的产品 API/控制器接线。未达到要求时报告 blocked，不转普通按量 API，不继承其他 provider Key。

各轮原始日志与 hash 分别保留于 TEMP-01、PROTOCOL-01、SYSTEM-DATA-01、CALLS-01、GRANT-01、CHANNEL-01、TOOLS-01、ADAPTER-01、QUOTA-READER-01、PRIVATE-CREDENTIAL-01、CN-TRANSPORT-01。历史源码 hash 应按对应 owning commit 校验，不能用当前 HEAD 重解释旧证据。QUOTA-READER-01 与 PRIVATE-CREDENTIAL-01 各有一次显式真实额度 GET，模型调用数均为 0；不更改用户日常 Claude 配置。

[WP-15-GLM-QUOTA-HOST-01](../WP-15/GLM-QUOTA-HOST-01/summary.md)补齐独立产品额度注册和一次真实 CN GET：本机声明的账号/workspace 标签仍未证明上游归属，不把查询目的授权转换为生成准入；UI 和真实执行 Factory 继续未完成。
