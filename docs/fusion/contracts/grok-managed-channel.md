# Grok 受管 channel 合同

对应 [WP-13-CHANNEL-01](../work-items/WP-13/CHANNEL-01/summary.md)，base `171ec6d7df448690bfed6bb753fefd7608e5317f`。提供可信 GrokChannel 与公共 Supervisor 生命周期的连接；完整 Grok Adapter、真实 X subscription NativeForwarder/准入、Resume 和产品派单尚未交付。构造 channel 或成功执行合成 fixture 不产生真实账号、额度、计费或所有调用锁定证明。

`Spec.GrokChannel` 只由可信 controller 组装，禁止 HTTP DTO 提供 channel、handler、grant、endpoint、argv 或 validator。同一次执行最多一个 ClaudeChannel/GrokChannel，零值 wrapper 与双 channel 均拒绝。GLM Adapter 同样拒绝 caller 注入 GrokChannel。GrokChannel 验证 runtime1.0.48、subscription、ControlledCalls、requested==resolved、具体 model id、account/workspace/credential identity、route revision 与无 plugin，深拷贝 target。Supervisor 要求实际 binary SHA256 等于固定 `1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05`，从实际文件核对，不接受 hash 声明代替文件。

两个 typed channel 共享私有 modelChannel lease，保持原 Claude 生命周期。构造器独占 IPv4 `127.0.0.1` 与 IPv6 `::1` 的同一随机端口，无 arbitrary endpoint 或单栈 fallback。最多16连接；服务端时限与 header 上限保持现有合同。Grok 内部 endpoint 为 `/v1`；真正请求仍由 [CallGate](grok-call-gate.md) 验证 `/v1/chat/completions`。

Store 的 starting/owner/task/role/attempt/plan revision/generation/target/project 必须匹配，prepared ModelAudience grant 覆盖执行时限加20秒，才能 acquire。ConfirmStarted 后才 Activate。Activate失败仍返回已知 Handle 并停止；取消/超时/心跳失败/listener故障撤销 grant，TERM/KILL 指向出生身份。存活时 Close 拒绝释放端口；Wait/reap 后写终态与 journal、签发本 Supervisor 可信 StopProof、释放端口，由 Scheduler.Release 核验停止证据并释放 reservation。StopProof 表示受该 profile 限制的进程已停止，不代表模型/计费/Quota/工程验收。

macOS sandbox 保持 default-deny、禁止 fork/Mach/Keychain、禁止其它网络；只允许该 lease 独占的 localhost port。Root/Workspace 必须私有、canonical、互不重叠。HOME/XDG/TMP 固定到本次 Root，GROK_HOME=`Root/config/grok`，不继承日常 env/config/auth。新 config.toml 用 O_EXCL/0600 写入并 fsync；所有子目录0700。config 使用内部 alias `fusion` 指向具体冻结模型、controller endpoint 与 scoped stage grant；default/session_summary 均 fusion，max_retries=2、rate_limit_retry_threshold=2，turn_summary/auto_update=false。只传 stage grant，不传真实 X secret/API key/OAuth。固定 LANG、DO_NOT_TRACK、RUST_LOG；不能混用 fixture env。真实厂商 secret 属于尚待独立准入的 controller NativeForwarder。

只读 Spec 的 Workspace 不授予 file-write；Root 内 HOME/配置/缓存仍可写，Native 需要读自身配置，因此不能从只读项目推出 Native 自身无法读取 grant。响应前 [ReadTools](grok-read-tools.md) 必须拒绝私有配置、项目外路径、其他工具及未批准内容；raw stdout/stderr 不通过公共 Result 暴露。每份输出各64KiB，超限停止。项目快照由 Native 同一进程读取时仍须解决主机并发替换/源权限/敏感内容问题；本项不能宣称解决全部 Host TOCTOU 或准入真实目录。

实际1.0.48合成 Native 在生产 profile 通过文本、13行项目读取、429重试、私有配置/外部读取/写工具拒绝、预算耗尽、调用中取消。每个 HTTP，包括标题/重试，均通过真实 Manager/Scheduler/Store 预算；标题与重试不在 Native end.modelUsage 中，不能用该标签计费。取消时下游连接关闭使已准入 single-send Forwarder context结束；验证了测试 transport退出、真实进程 wait、StopProof、grant撤销、reservation release和项目文件未改。外部 transport必须遵守context；不能撤回已发送的外部请求。

独立 C kernel探针具有 unrestricted loopback正控：受同一 Grok profile 时 owned双栈可连接、其它端口/Mach/fork/项目写入/外部文件读写被拒绝，而 private配置目录写入成功。它验证 OS profile；实际 Native测试分别验证协议/生命周期，不能互相替代。原 Claude2.1.287受管成功/重试/预算/取消与Read/Edit工具场景另行回归，版本不随日常 launcher 自动升级。Resume、写工具、多工具/offset/binary读取、产品注册、X登录/实际路线/池/计费继续未验证，Jev off。
