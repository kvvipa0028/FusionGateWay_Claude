# WP-15-GROK-01：Grok Adapter 的可信控制器生命周期

基础 HEAD：`20d39f144960d85c4da97f1f125333665aac0cd3`。本子项 done；WP-15/WP-13 继续 in_progress。本项补齐 Controller 的 GrokChannel 输入拒绝，并验证现有可信绑定调用 Controller→Scheduler→Grok Adapter→CallGate/ReadTools→固定 Native→实际 wait/StopProof→release。未注册产品 Worker 或真实 subscription Forwarder。

新 Spec.GrokChannel 曾未被 validLaunch 检查；Resolver 返回它时可先写入启动 intent。[RED](wp15-grok-guard-red.log)证明缺口，随后增加 nil 检查。Grok/Claude 原始 channel 与既有 argv/env/session 等均在 intent 前拒绝，任务仍 ready、generation 0、Backend 不启动。原 GLM fixture 只提取共用构造函数，路线和 effort 预期未改变。

[固定 Grok 1.0.48 实际控制器回归](wp15-grok-native-first.log)为 1 个 top-level、7 个场景 PASS：文本成功、项目文本 Read、HTTP 请求断线、Pause、整项 CancelTask、阶段 Cancel、Close。首次启动通过真实 Management middleware；运行中及终态相同 key 读取原持久 receipt，不重复解析/执行。断线不取消 owned lifetime；暂停/取消/关闭验证实际 Native 退出、原 Supervisor StopProof 和容量释放。暂停收尾 needs_review，Continue 拒绝盲重跑；整项取消收尾 cancelled，禁止 Continue。running Observation 不暴露文本，失败/取消无成功文本；Completion 不含私有路径/输出。所有场景只读文件保持不变。

每次实际 HTTP 都由真实 Scheduler/Store Permit 记账：Read 3 次，其余 2 次，包含 Native 标题调用。上游、Inspector、Quota 和账号身份为合成 fixture，没有真实模型或额度调用。Adapter 的 StrictLockVerified/BillingVerified/QuotaVerified/StoppedVerified 独立标志保持 false；控制器的 StoppedVerified/Released 来自真实停止证明核验。固定版本与可控传输测试不等于真实路线准入或最终工程验收。

[control package race](wp15-grok-control-green.log)：23 PASS / 4 SKIP / 0 FAIL。[全 Fusion race](wp15-grok-fusion-tagged-race.log)：409 PASS / 17 SKIP / 0 FAIL；Native 条件诊断未传 flag 时跳过，不能把 SKIP 视作通过。[固定 Claude Code 2.1.287 回归](wp15-grok-claude-control-native.log)的 3 项真实 Controller/Native 测试另行通过，覆盖成功/幂等/断线、inflight Pause、inflight CancelTask。CLI、GUI 编译及全仓 tagged vet 的 [结果](build-results.json)均 exit 0，Go 1.26.3。检查后只新增文档和证据，不重复相同检查。

复现需要 macOS/arm64、sandbox-exec、clang/SDK、公开 Go caches 及显式固定 Native；临时 HOME/XDG、环境白名单，不登录或读取日常认证：

```sh
set -e
helper=docs/fusion/work-items/WP-13/CALLS-01/run-go.py
python3 "$helper" .fusion-dev/implementation/grok-controller-recheck.log \
  test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/control -run '^TestControllerPinnedGrokNativeLifecycle$' \
  -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
python3 "$helper" .fusion-dev/implementation/grok-controller-fusion-recheck.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/...
python3 scripts/fusion/build-dev.py
```

字段、日志派生 counts 及边界见 [test-results.json](test-results.json)，源文件和原始证据 SHA256 见 [artifacts.json](artifacts.json)。原始日志保留空白，diff-check 排除 *.log。Store/schema/API/dependencies/master 开关和日常认证未改，Jev off；primary 原有两个 .DS_Store 保留。

Native Resume、write/effort、真实 X/OpenAI 认证/额度/计费准入、Source 稳定性、产品 Resolver/Worker/GUI、工程闭环和 60 类最终 Gate 仍未完成。这里的同 key receipt 重读是幂等查询，不是 Native 会话恢复。
