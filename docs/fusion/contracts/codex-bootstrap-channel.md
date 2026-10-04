# Codex 私有启动通道

`WP-12-NATIVE-ISOLATION-01` 将 Codex 0.160.0 的真实 App Server 启动及双向 stdio 接入现有 Supervisor。它是只读、无账号、无网络的启动组件；未提供生产 Codex Adapter、ChatGPT 登录/续期、模型调用准入或工程阶段验收。证据见 [工作项](../work-items/WP-12/NATIVE-ISOLATION-01/summary.md)。

`NewCodexChannel` 只接收可信进程内 StageRun、私有 Root/Cwd、启动 session ID、协议 driver 和 Current。冻结完整 target 与 run/task/owner/role/attempt/plan revision/generation，独占 acquire，一次使用后关闭；零值、身份漂移、不同路径/target、混用 Claude/Grok、fixture env、stdin 或 writable 都拒绝。Controller 的 Start/Restore 与 Grok/GLM Adapter 输入不得携带该字段，且在 intent 前拒绝。它不能序列化为 JSON，格式化输出不含私有路径或身份。

仅允许固定 SHA256 `112fae7a5a1223e673c8a1791d32338f37df8b527ff1159bb8adac6c4dbf1b4b` 和精确 argv：

```text
app-server --listen stdio:// --strict-config
```

不允许 `-c`、忽略 host-managed requirements、任意配置覆盖或其他入口。实际文件 hash、权限、canonical path、持久 Store/held reservation、来源 Guard 与出生身份仍由 Supervisor 校验。SHA pin 是文件身份与版本约束，不能代替 publisher trust、权益、计费或模型证明。

新建独立 `Root/config/codex`（0700）作为 CODEX_HOME；HOME/XDG/TMP 仍为新建私有目录，CFFIXED_USER_HOME 等于私有 HOME。不继承日常 HOME、环境、账号、Keychain、OAuth/API key；不从现有 `.codex` 导入 auth.json。Root/Cwd 为仓库外私有目录，复制项目来源另由 SourceGuard 观测。

macOS 默认 deny 保持。专用通道仅增加：

```scheme
(allow mach-lookup
  (global-name "com.apple.cfprefsd.agent")
  (global-name "com.apple.cfprefsd.daemon")
  (local-name "com.apple.cfprefsd.agent"))
(allow user-preference-read (preference-domain "com.openai.codex"))
(allow ipc-posix-shm-read* (ipc-posix-name-prefix "apple.cfprefs."))
```

这是 domain 与 IPC 名称受限的只读许可，保留原生 MDM 校验。它允许读取 Codex 偏好域，并非只由 kernel 区分其中的 forced 键；固定 Native 的 [实现](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/config/src/loader/macos.rs) 在同步后只将 forced 值作为受管配置。不能借此宣称所有共享内存内容已逐字节审计。其他偏好域、偏好写、shared-memory 写、Keychain/launchd 服务、network 和 fork 继续拒绝。通用 Worker/Grok/Claude 不获得这些额外权限。

内核 probe 使用 Apple WebKit [SandboxSPI 声明](https://github.com/apple-oss-distributions/WebKit/blob/main/Source/WTF/wtf/spi/darwin/SandboxSPI.h) 核对的 sandbox_check filter，验证正对照和受限结果，不读取任何 Keychain 项或写系统偏好。这个 private SPI 只在测试中使用；其他 OS/Native pin 必须重新验证，不以字符串检查当作内核证明。

双向 transport 使用 OS pipe，子端在父进程 Start 后立即关闭。driver 仅在 ConfirmStarted 后运行；读写均复核 Store 当前执行、SourceGuard 与 Current。输入累计及已读取输出各有64KiB限额，输入超限即使被 driver 忽略，也不能成功；原有 stderr64KiB/timeout/heartbeat/TERM→KILL/Wait/reap/journal/StopProof/release 保持。metadata 通道的64KiB不是完整生成任务的输出容量证明。

driver 必须遵守取消并核验协议；driver 返回 nil、实际 exit0、有界 transport 未失败、可信 outcome validator、来源/Current 复核同时满足，才可记录组件成功。取消或当前性撤销关闭管道并取消 driver。Wait 与最后响应竞争时最多等待 driver200ms；超时不判成功，关闭管道并取消，晚到结果不能修改终态。该等待不是强制终止任意不合作的 Go callback；生产 driver 必须另有协作退出与服务生命周期合同。StopProof 只证明已 wait/reap 的 Native 与 kernel 禁止 fork 的进程边界。

真实0.160.0通知使用可选 `emittedAtMs`。StdioPeer 接受通知上的 signed int64；null/类型错误/越界、response/server request 上的该字段、重复/case aliases 和未知字段仍拒绝。字段仅作观察，不参与 generation、响应配对、账单或权限判断。冻结 [定义](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/app-server-protocol/src/protocol/common.rs) 为 Option<i64>，None 被省略，本地 Native schema 的属性为 integer/int64。

真实验证只执行 initialize/initialized/account/read(refreshToken=false)，确认私有 home、空账号、requiresOpenaiAuth=true；StartThread 在 generationAdmitted=nil 下本地拒绝，预算0，未发送登录/模型/额度请求。测试 Store 的 synthetic admitted route 不能提升真实注册路线。接下来仍须实现独立登录 home 与 registry、stage grant/全部模型调用控制、生产 Adapter/Factory、原生 thread/turn/续期/resume 与三路线 Gate。
