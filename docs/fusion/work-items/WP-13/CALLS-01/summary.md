# WP-13-CALLS-01 证据

基础 HEAD：`929d2e3d2ade59bdf287b54ee213ed93440589d4`。交付 Grok 每次 HTTP 的 ModelAudience/冻结绑定/调用许可与有界 SSE Gate；不登记生产 Native transport 或 Worker，parent WP-13 继续 in_progress。合同见 [grok-call-gate.md](../../../contracts/grok-call-gate.md)。

先运行缺少 CallGate 的 [RED](wp13-calls-red.log)，实现后修复 [choice extension RED](wp13-calls-boundary-red.log)：未知 choice 字段必须拒绝，不能静默接收。测试 harness 初期错误使用 background model context、通过 model-only validator mint Events grant，随后改为真正认证 context 和合法 Events grant；保留 [scope RED](wp13-calls-scope-red.log) 和两个 compile RED，没有弱化 Manager 或 Gate。Native retry 初次 [RED](wp13-calls-native-rate-red.log) 在 max_retries=1 时只发一次主请求；固定通用 JSON error 加 max_retries=2 后实际观察到重试。两项一起修改，不能把具体原因归结为其中一项或用不同版本公开源码推断。最终五场景是在最新 Gate 和真实 Store/Scheduler 上重新执行。

十二项新增 Host 测试覆盖：每个 title/main 许可、17 种非法请求、认证拒绝不 poison 合法 grant、权限/身份/预算/transport 变化、危险/超大/工具/模型/不完整 SSE 零泄漏、429 再许可、effort 和深拷贝、构造限制、marker反射/late revoke/choice 扩展、并发串行，以及实际 Store/Scheduler 的持久化预算和 quota/permission/proof 再检查。完整结果：[targeted 22 PASS/1 SKIP](wp13-calls-green.log)，[Fusion race 387 PASS/11 SKIP/0 FAIL](wp13-calls-fusion-tagged-race.log)。SKIP 是需要显式 Native 环境的诊断；本包另行执行 Grok 五场景。原有其它 Native 检查未在本包重跑，不因此宣称其本次真实路线通过。

固定 Native hash `1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05`，CLI `1.0.48/b94d5072c95f`。实际 [Native diagnostic](wp13-calls-native.log) 使用私有临时目录、空项目和合成 stage grant；Store/Manager/Scheduler 真正执行，Inspection/quota/route admission 全为合成 fixture。进程实际 wait，较宽 Mach lookup 的诊断 profile 不等于受管 Worker StopProof/isolation。未使用真实 key/登录，不联系真实模型或额度接口。

| 场景 | Native exit | HTTP | 转发 / 持久化 usedCalls | 结果 |
| --- | --- | --- | --- | --- |
| success | 0 | 2 | 2 | title/main 各扣一次；end.modelCalls=1 |
| default_title_model | 1 | 2 | 0 | 默认 grok-4.6 被拦截，之后主请求也拒绝 |
| budget_second | 1 | 2 | 1 | MaxCalls=1，第二次 Permit 拒绝且不转发 |
| implicit_tools | 1 | 2 | 1 | Read 附加 discovery/use tools 被拦截 |
| sdk_retry | 0 | 3 | 3 | title一次/main两次；429无退还；end.modelCalls=1 |

复现要求：macOS/arm64、Go1.26.3、公开 dependency 缓存、clang/Xcode SDK、sandbox-exec、固定且 hash 匹配的已登记 Native；无需真实授权。`run-go.py` 是此次实际检查 helper 的原样快照，只继承 Go 工具路径与公开 cache，HOME/XDG/env 独立，网络 dependency 下载关闭。它不自动安装或登录 Native。

在开发 worktree 执行（先确认 `go version` 为 1.26.3）：

```sh
set -e
mkdir -p .fusion-dev/implementation
packet=docs/fusion/work-items/WP-13/CALLS-01
python3 "$packet/run-go.py" .fusion-dev/implementation/grok-native-recheck.log \
  test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/runtime/grok -run '^TestGrokCallGatePinnedNativeDiagnostic$' \
  -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
python3 "$packet/run-go.py" .fusion-dev/implementation/fusion-recheck.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/...
python3 scripts/fusion/build-dev.py
```

Native 测试在不提供 flag 时 SKIP；显式提供却 pin/版本协议不符应失败。临时目录、监听器和 Store 随用例清理；CommandContext timeout/WaitDelay 回收自己拥有的诊断进程。该诊断不是生产启动/取消实现。

正式命令、counts 和未验证 flags 见 [test-results.json](test-results.json)；CLI/GUI/full tagged vet 均 exit0，见 [build-results.json](build-results.json)。源文件/证据 hashes 见 [artifacts.json](artifacts.json)，按拥有它的提交树验证。原始日志 whitespace 不修改，diff 检查排除 `*.log`；文档/源码/JSON/Python 分别检查。未更改 Store schema、既有生产 API、route admission、主执行开关或日常配置。真实模型/额度调用均0，Jev off；真实 forwarder/账号/计费/额度、effort Native、工具、Resume、受管取消停止、工程闭环和最终60类验收仍未验证。
