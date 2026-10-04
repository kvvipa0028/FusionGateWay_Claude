# WP-12-CHANNEL-01：Codex 原生阶段 HTTP 通道

基线 `86c1a01011dc17a8a2962df72389763b1532ce19`。完成独占 scoped HTTP 通道、私有 custom stage provider、激活先于 driver、原生观测字段严格投影及实际固定 Native 验证。合同见 [codex-http-channel.md](../../../contracts/codex-http-channel.md)。父 WP-12仍 in_progress，真实登录/Forwarder/生产 Adapter/工具/恢复和最终60类 Gate仍未完成；Jev off。

实际 Native0.160.0，SHA256 `112fae7a5a1223e673c8a1791d32338f37df8b527ff1159bb8adac6c4dbf1b4b`。仅隔离目录/白名单环境/合成账号与上游，使用实际 Store/Manager/持久预算；测试 Permit调用真实 ReserveCall，但准入/Current为 synthetic，不能替代生产 Scheduler.Permit、registry、quota或SourceGuard接线。没有真实订阅模型、额度或登录网络请求，没有读取日常 auth.json/Keychain。

本项新增7个 Native 场景；同时回归前项15场景（12次 Native+3类启动前拒绝），合计19次实际 Native 进程，全部 wait/StopProof/预约释放。最终 Native4顶层 PASS/20子测试 PASS/0 FAIL/0 SKIP。

| 新场景 | 受控 HTTP | 上游合成发送/持久预算 | 结果 |
|---|---:|---:|---|
| 文字成功 | 1 | 1/1 | Native completed，Stage succeeded |
| HTTP429 | 1 | 1/1 | Native failed，Stage failed；未自动重试 |
| 不支持503 | 4 | 1/1 | 首个响应拒绝，Native重试也拒绝，Stage failed |
| MaxCalls=2三轮独立turn | 5 | 2/2 | 前两轮完成，第三轮及重试拒绝，Stage failed |
| inflight取消 | 1 | 1/1 | HTTP context结束，Native cancelled；已消费预算不退 |
| 激活先于driver | 0 | 0/0 | 阻塞验证器下driver等待激活成功 |
| 激活失败 | 0 | 0/0 | driver0；返回owned handle，取消/reap/release |

固定 Native 实际 account/read为空账号、requiresOpenaiAuth=false；thread/start/turn/start使用本地 fusion_codex_stage、冻结 model/effort/readonly。原生工具关闭，client_metadata只接受7个实测观测键/有界非空字符串；拒绝未知账号字段、类型、null、重复/alias、超长与凭据反射。metadata不是授权。默认 production Client/new generation gate未修改，未伪造ChatGPT账号metadata。

其他最终验证：

- 完整 Fusion race：491顶层 PASS/26顶层 SKIP/0 FAIL；851个通过子测试、5个显式 Native 子测试 SKIP。SKIP不算实际执行通过，Native另用显式pin单独运行。
- Grok/Claude Controller实际 Native回归：7顶层 PASS/17子测试 PASS；既有20场景/25次 Native，实际stop/release保持。认证 ExecutionHost Grok回归5场景/5次 Native：1顶层 PASS/5子测试 PASS。
- Go1.26.3 CLI/GUI构建/full vet exit0；graph刷新13794 nodes/121833 edges。最终行为验证后只有注释与文档/packet更新。
- 仅阶段grant进入 Native环境，config0600没有secret；独占双栈/活跃Close拒绝/target冻结和错误输入检查通过。真实私有key未进入本项源码、文档、日志或hash清单。

RED→GREEN及实施判断：

1. 构造器/scope/profile初始编译 RED后实现GREEN；fixture语法编译错误另存，不混为行为证据。
2. 实际激活顺序 RED：driver抢在grant激活前进入，阶段cancelled/-1。移动到激活成功后启动；失败保留owned handle并真实停止，最终通过。
3. 实际 Native首个HTTP被旧Gate拒绝，0发送/0预算。安全投影只记录字段名：7个client_metadata、3个goal工具。固定source验证goals key，关闭阶段goals（不会关闭当前控制器目标），增加严格观测字段测试：接受测试400 RED后GREEN，未知字段仍拒绝。
4. 初始429 retry/budget预期失败：实际只发一次。源码明确HTTP retry_429=false，不能把RateLimitExceeded的SSE路径与HTTP429混同。保留失败日志，改为验证真实429终止、unsafe503的native retries拒绝，以及三轮独立Native预算拒绝。
5. 第二轮预期2/3 HTTP仍失败：实际4/5。固定api_bridge将403归为UnexpectedStatus，protocol.retry_delay允许有界stream retry。固定request_max_retries=2/stream_max_retries=2下准确断言4/5，同时断言上游发送和预算仍1/2；没有退款、清空预算、伪造Native、放宽unsafe响应或按Native统计取代真实发送。
6. 内置openai不能被普通provider配置覆盖，采用仅阶段授权的本地custom provider。它不是真实ChatGPT登录、权益或普通API付费退路；代价是生产Client/官方订阅接线仍待完成，父工作包不能标done。

复现（从本工作树根目录，明确pin，无真实账号）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-http-native.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime -run '^TestManagedPinnedCodex(HTTP|Bootstrap)' -fusion-native-codex /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-http-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

测试helper只继承公共SDK/module cache与白名单临时HOME/XDG；执行后临时目录清理。错误pin/配置/scope应拒绝，不能换默认日常launcher以绕过。日志含合成Native安全字段名，无原始请求/鉴权正文；失败与最终通过日志均保留。源码、packet与公开参考hash见 artifacts.json/reference-sha256.json，回读所属Git提交验证；旧packet按旧所属提交核验。
