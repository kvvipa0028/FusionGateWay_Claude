# WP-13-CHANNEL-01：Grok 受管执行通道

基础 HEAD：`171ec6d7df448690bfed6bb753fefd7608e5317f`。本项 done，parent WP-13 in_progress。新增可信 GrokChannel 与私有配置，并复用现有独占双栈 lease、Supervisor/Store 生命周期。真实固定 Grok CLI 已在生产 default-deny profile 中完成合成文本/读取/拒绝/取消/停止证明；完整 Grok Adapter、真实 subscription NativeForwarder/准入、Resume/写权限和产品注册继续未完成。合同：[grok-managed-channel.md](../../../contracts/grok-managed-channel.md)。

先添加缺少构造器/Spec字段的测试并执行 [RED](wp13-channel-red-compile.log)。初次 TOML 测试引用未有依赖，[setup RED](wp13-channel-red.log) 保留，改为已有 go-toml/v2，go.mod/go.sum未改。channel 共享内部 lease，原Claude公开构造/Close和nil-safe Close行为保留；Spec明确拒绝双channel或空wrapper，GLM Adapter拒绝caller注入GrokChannel。模型/runtime/billing/身份/route与prepared scope不符拒绝；target深复制，grant不序列化。

[初次实际 Native](wp13-channel-native-red-exec.log) 在原强sandbox中成功完成2HTTP，但新 fixture 捕获外层e与Start赋值产生race。修为callback局部finishErr，未削弱生产保护。随后8场景中的7项通过，[retry RED](wp13-channel-native-green.log) 记录第8项计数断言错误：429的3次forward对应2次completed与1次retryable，修为独立期望3/2/1并保留实际预算3，未更改Gate行为。

[Native最终回归](wp13-channel-native-final.log) 4个top-level/16个场景通过，其中本项8个受管场景：

| 场景 | Native终态 | 真实持久化预算/合成HTTP | 停止与边界 |
| --- | --- | --- | --- |
| success | succeeded/exit0 | title+main=2 | StopProof核验与release |
| read | succeeded/exit0 | title+2main=3；end标签calls=2 | 13行项目文件、trace/history/read scope一致、项目未改 |
| private_config/outside_read/write_unsupported | failed/exit1 | 各2 | Gate响应前拒绝，未升级权限；文件未改 |
| budget_exhausted | failed/exit1 | forwarded/usedCalls=1，尝试Permit2 | 不补预算/不fallback |
| cancel_inflight | cancelled/exit143 | title+main=2 | 撤销、transport context退出、wait/reap/StopProof/release |
| sdk_retry | succeeded/exit0 | forwarded/usedCalls=3，completed2/retryable1；end标签calls=1 | 每HTTP包括retry计费 |

所有8项核验本Supervisor可信StopProof、terminal grant拒绝、无grant写入公开report/journal、reservation release、项目/外部fixture文件保持。Native标签/成功与StopProof分别解释，不提升StrictLock/Billing/Quota等独立flags。所有quota/route/inspection均合成，真实账号或额度请求0，Jev off。

[Grok kernel/Claude共享回归](wp13-channel-kernel.log) 4PASS，C fixture positive control在无sandbox可连两个loopback；Grok实际profile只允许owned双栈，拒绝其它端口/Mach/fork/项目写入/外部读写、允许private配置写入。[构造与私有配置targeted](wp13-channel-green.log)12PASS；[全Fusion race](wp13-channel-fusion-tagged-race.log)406PASS/14SKIP/0FAIL；[CLI/GUI/full tagged vet](build-results.json) exit0。没有重复提升旧历史证据，所有新文件hash见[artifacts.json](artifacts.json)、正式counts见[test-results.json](test-results.json)。

Claude实际兼容性首次使用日常launcher，[pin mismatch](wp13-channel-claude-native.log)正确拒绝更新后的版本；改用已保留2.1.287文件，[最终](wp13-channel-claude-native-final.log)2top/11场景PASS，包含成功/重试/预算/取消和Read/Edit/create/越界/只读写入拒绝。未修改launcher/协议pin/日常认证。标准全套测试的14SKIP属于显式Native/fixture诊断；本项已另跑上述Grok/Claude场景，其余诊断仍不记为本项覆盖。

复现前提：macOS/arm64、Go1.26.3、clang/SDK、sandbox-exec、公开模块缓存、两个固定版本文件/hash；不需要登录或API key。测试隔离HOME/XDG、白名单env、临时Store/项目/私有Root，自动wait与清理。测试helper保持前项文件，命令：

```sh
set -e
helper=docs/fusion/work-items/WP-13/CALLS-01/run-go.py
python3 "$helper" .fusion-dev/implementation/channel-native-recheck.log \
  test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/runtime/grok -run '^Test(Grok(CallGatePinnedNative|ControlledReadPinnedNative)|ManagedPinnedNativeGrokChannel)' \
  -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
python3 "$helper" .fusion-dev/implementation/channel-claude-recheck.log \
  test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/runtime/glm -run '^TestManagedPinnedNative(ClaudeChannel|BuiltinTools)$' \
  -fusion-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
python3 "$helper" .fusion-dev/implementation/channel-fusion-recheck.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/...
python3 scripts/fusion/build-dev.py
```

未传Native flag时测试SKIP；传错pin时必须失败。raw logs空格保留，diff-check排除*.log；JSON/gofmt/链接/hash和key exclusion分别验证。源码Store/schema/API、master执行开关与依赖未改，primary未提交DSStore保留。

范围限制：Grok Spec仍由可信控制器构造，未提供产品Adapter/请求argv入口。ROOT配置可供Native读写，只读项目不是私有grant不可读的保证，保护来自响应前ReadTools与公开输出限制；Native物理读取前的主机并发文件替换、Source/权限/敏感信息处理仍需独立闭环。生产Forwarder必须履行context/single-send/账号/端点/计费身份；本项无真实X登录、Heavy路线或pool证明。Resume、写/多工具/offset/binary与最终T01–T60 Gate继续未完成。

额外C语法检查首次直接调用ambient clang，拾取/usr/local/include头文件导致nullability警告失败：[环境RED](clang-ambient-red.log)。按原test helper的固定SDK与环境白名单重新执行-Wall/-Wextra/-Werror/-fsyntax-only：[隔离SDK结果](clang-private-sdk.log)exit0。未降级warnings或修改系统头文件；此前full/kernel fixture已在隔离SDK成功编译并实际执行。
