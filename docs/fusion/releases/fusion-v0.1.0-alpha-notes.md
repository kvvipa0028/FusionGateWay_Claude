# Fusion Gateway v0.1.0-alpha

手动可用版。可以保存五角色预设，执行指定的单阶段工作；阶段之间由用户明确推进。只供本人内测，不承诺生产可靠性。

## 功能

- **三路 Runtime Adapter**：Codex（ChatGPT 订阅）、Grok（xAI 订阅）、GLM（中国大陆 Coding Plan）的完整执行链——凭据读取、订阅 Forwarder、生产执行 Factory、受管生命周期。
- **五角色工作台**：设计→实施→测试→审查→验收，通过原 Magpie 界面操作。
- **受控执行**：启动、暂停、取消、恢复、停止证明释放、durable artifact 发布。
- **写入路线**：GLM 和 Grok 进程内写入；Codex 委托沙箱写入。
- **安全边界**：可执行文件 SHA256 pin、私有执行根、环境白名单、localhost 模型通道、三层鉴权、持久预算。

## 限制

- 仅 macOS (darwin/arm64)。
- 需要本人完成官方设备授权登录后才能执行真实模型调用。
- Codex 写入的外层 supervisor profile 对 writer 不做 Seatbelt 隔离（由 native 内层沙箱接管命令隔离）。
- Grok 暂不支持 effort 档位。
- Jev 旁路、自动工作流、插件自动更新全部 off。
- 原版 Magpie 的聊天、Sessions、Gateway 功能保留但不与 Fusion 互通。
- 草稿 CLI 不执行任务。

## 环境要求

- Go 1.26.3（固定版本， SHA256 锁定）
- macOS Sequoia (darwin/arm64)
- 固定 Codex 0.160.0、Grok 1.0.48、Claude Code 2.1.287（Apple Developer ID 签名验证）
- 官方设备授权登录的私有凭据缓存（Git 外 0600 文件）
