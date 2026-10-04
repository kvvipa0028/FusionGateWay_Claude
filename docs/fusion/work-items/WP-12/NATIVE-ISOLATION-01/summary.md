# WP-12-NATIVE-ISOLATION-01：真实 Codex 私有启动与双向管道

基线 `e5e3e71e572b8d0f9649f2009fbb8a6ad47603d9`。完成固定 Codex0.160.0 的受限 CFPreferences 启动、Store-bound 双向 stdio、共享 Supervisor 生命周期接线和通知时间戳兼容；合同见 [codex-bootstrap-channel.md](../../../contracts/codex-bootstrap-channel.md)。父 WP-12保持 in_progress；生产 Adapter/Factory、独立 ChatGPT 登录/续期、所有模型调用与额度/计费控制、原生 thread/turn/resume 和最终三路线 Gate尚未完成。Jev off。

改动与边界：

- NewCodexChannel 冻结持久 StageRun 的 target/role/owner/task/attempt/revision/generation 与私有路径/session，仅固定 Native hash与四个固定 argv。只读、无网络、无 fork、无账号导入；与其他 channel/fixture/env/input/writable 混用拒绝，零值和漂移拒绝，不能成为 JSON 授权字段。Controller Start/Restore、Grok/GLM caller 输入提前拒绝 CodexChannel。
- 仅专用 Codex profile 开放精确 cfprefsd global/local Mach、com.openai.codex preference-domain 读取和 apple.cfprefs 前缀 shared-memory 读取；保留原生 forced MDM 检查。未允许其他偏好域/偏好写、SHM写、Keychain/launchd、网络/fork；通用 Worker/Grok/Claude 权限不增加。kernel 许可是 domain 级，固定 Native 只采纳 forced 值；不是共享内存逐字节审计。
- OS pipe 双向读写在 ConfirmStarted 后交可信 driver；每次核对 Store/current/source。输入/已读取输出各64KiB；输入超限具有不可忽略的 failure latch。driver完成、实际exit0、有界IO、outcome validator 与前后当前性共同决定组件成功；取消/timeout/Source或身份变化撤销IO/driver并真实wait。EOF最后响应最多等待driver200ms，超时失败且晚到不能改终态；不声称能强制结束任意不合作callback。实际 StopProof/pool release 保留。
- 原生通知 emittedAtMs 被旧 StdioPeer 当未知字段，导致初始化后提前断开。按冻结源码 Option<i64>与本地integer/int64 schema，只接受通知上的signed int64；省略兼容，null/type/overflow、reply/server-request使用、重复/case aliases/其他未知字段仍拒绝，不将时间戳作为身份或账单依据。

验证：

- 原 default-deny profile 真实 Native exit1，`Failed to synchronize managed preferences`，wait/reap，模型/登录0；历史原证据保留。private HOME、CFPREFERENCES_AVOID_DAEMON、domain-only、Mach-only等变体未解决。补齐 local/global服务及SHM-read后真实初始化通过，修正诊断RPC配对后metadata通过；variant表只作诊断，不升级为生产准入。
- 编译RED（缺新接口；初始测试误将 FrozenEffort 当指针，随后按真实类型修正）；Controller的新增注入测试真实RED→GREEN。C probe初始局部变量名遮蔽write造成编译失败，已修正，最终完整测试通过。
- 真实Native复现 emittedAtMs 关闭连接，先有Native结构投影失败日志，再有通知字段RED→GREEN。最初 timestamp 测试猜测unsigned，冻结common.rs及schema确认signed后按其修正，最终覆盖int64边界、null/类型/溢出/其他envelope/重复/未知字段。没有放开通用未知字段。
- 真实Native的ignored_overflow场景：临时去掉failure-latch判据返回succeeded而RED；恢复判据后最终GREEN，不能以driver nil掩盖64KiB拒绝。Native fixture首次缺必需Budget而record-not-found；补fixture MaxCalls2，真实budget used一直0。
- 最终固定Native通过15场景：success、driver/semantic failure、validator期间source/identity变化、cancel、parent cancel、timeout、source/identity idle变化、transport overflow与忽略错误、3类preintent拒绝。共12次Native进程、24次initialize/account-read RPC；每次确认私有home和空账号/requiresOpenaiAuth=true，StartThread在nil generationAdmitted下本地拒绝。实际Wait/StopProof/release全部通过，重复Start不产生第二进程，副本文件保持，无auth.json导入，模型/登录/额度请求0、持久模型预算0。
- CFPreferences C kernel正对照与生产profile：Codex域读取/实际AppSynchronize通过，其他偏好读、Codex写、Keychain/launchd/SHM写被sandbox_check拒绝；实际loopback connect、fork、越界读写被拒绝，私有CODEX_HOME写入通过，项目写拒绝。probe未读取Keychain项、未写系统偏好，private SPI声明以Apple WebKit为依据；不宣称允许网络/子进程路线已准入。
- 完整Fusion race **470 PASS/24 SKIP/0 FAIL，742个PASS子测试**。SKIP中的真实Native单独运行；真实Codex另1顶层/15子测试，不能混算默认已执行。
- 共享Supervisor回归：固定Grok/Claude控制器7顶层/17子测试，20场景/25次Native；authenticated Host→固定Grok5场景/5次Native/10次合成模型HTTP，全部actualStop/release。这些上游/账号/额度仍为synthetic，不是实际账号准入。
- Go1.26.3 CLI/GUI/full vet exit0；graph13706 nodes/120742 edges。最终行为测试后仅更新文档及packet，未再次更改源码。source/packet SHA应在所属提交回读，不用后续树代替。

复现（仅macOS；测试私有HOME/XDG，无真实账号）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-isolation-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-isolation-native.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime -run '^TestManagedPinnedCodexBootstrap$' -fusion-native-codex /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-isolation-regression.log test -race -v -count=1 -timeout=240s -mod=readonly -tags fusion,nogui ./internal/fusion/control -run '^TestControllerPinned(Grok|Native)' -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 -fusion-control-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-isolation-host.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/bootstrap -run '^TestExecutionHostPinnedNativeHTTP$' -fusion-host-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

当前只能交付bootstrap组件与隔离/停止证据，不能把Store fixture的stage succeeded当作工程设计/生成完成。T01–T60最终Gate仍not_run；继续原实施计划的真实ProviderFactory、账号/模型调用控制、产品入口与工程闭环，不缩减总体目标。
