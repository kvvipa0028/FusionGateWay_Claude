# WP-12-GATEWAY-CLIENT-01：类型化 Codex 阶段协议

基线 `b36e847a44146a8e2a14856417f732ef477309f9`。交付可信NewGateway、本地stage provider/独立上游Identity准入、完整实际thread/turn/通知/interrupt核验。合同见 [codex-gateway-client.md](../../../contracts/codex-gateway-client.md)。父WP-12继续in_progress；正式Adapter/官方登录/Forwarder/真实quota/工具/恢复和最终60类Gate仍未完成，Jev off。

本项实际Gateway Client并非生产路线已开放。可信Scope/Identity/admitted由进程内回调提供，测试使用合成上游/账号准入与真实Store/Manager/ReserveCall预算；生产Factory仍须真实Scheduler.Permit、SourceGuard与registry接线。Native为空账号，不借日常账号/Keychain/APIkey，不构造fakeJWT/chatgptAuthTokens。默认New/openai认证与原角色行为保持。

14个新增Host测试、43个通过子测试，覆盖target/route/billing/effort/version/identity、local account空形态、未准入零生成、五角色readonly、默认auth guard不放宽、固定provider/effort副本、callback撤销、丢失thread确认不可重放、RPC取消后迟到确认、严格通知/终态/工具拒绝及workspaceRouting只接受null。

固定Codex0.160.0通过typedClient实际新增五个场景：

| 场景 | HTTP/合成发送/持久预算 | 原生与组件结果 |
|---|---|---|
| 文字 | 1/1/1 | Native completed/Client succeeded/Stage succeeded |
| HTTP429 | 1/1/1 | Native failed/Client failed/Stage failed |
| unsafe503 | 4/1/1 | Native重试均被Gate拒绝/Client failed/Stage failed |
| Supervisor取消 | 1/1/1 | inflight context结束/Stage cancelled/wait/reap/release |
| 原生turn/interrupt | 1/1/1 | typedRPC确认，Native/Client interrupted；无Controller cancel intent时Stage failed，不能冒充产品cancelled |

前项metadata启动、activation及raw HTTP场景同时回归，总计27场景、24次实际Native进程（另3类intent前拒绝），全部实际wait/StopProof/release；Native最终5顶层PASS/25子测试PASS/0SKIP/0FAIL。新增五场景共8HTTP/5次合成上游发送/5持久预算，没有真实订阅模型/额度/登录请求。

最终检查：Codex包race56PASS/176子测试PASS/0SKIP/0FAIL；完整Fusionrace506PASS/26顶层SKIP/0FAIL，894子测试PASS/10子测试SKIP。显式Native SKIP未算实际执行通过，另有上述Native运行。Go1.26.3 CLI/GUI/fullvet exit0；graph13829nodes/122182edges。源码最终验证后仅更新文档/packet，真实私有key排除，原项目DSStore不动。

RED→GREEN/实施判断：

1. 新构造器/常量最初不存在，编译RED后实现。空Native账号只证明stage传输，独立upstreamIdentity与admitted仍必须有效；Gateway五角色readonly/tool-free，resume先拒绝，代价是完整Adapter/write/工具/恢复仍待实现。
2. 实际typedNative先拒绝remoteControl/status/changed，尚未发送模型；观测诊断仅收集method名，故意不给Stage成功。核对固定schema后增加有界观测投影，删除临时“只看terminal”的诊断分支。未将忽略未知通知当作通过。
3. disabled remote通知实际serverName为空，schema允许空字符串；保留该合法形态，connected/connecting/errored仍拒绝。MessagePhase固定枚举commentary/final_answer，按schema核对而非猜测。
4. 两个行为RED：terminal中未观察item仍succeeded、lost thread ack仍发第二次thread/start。绑定已完成item/type、线程发送前置uncertain并拒绝重放，最终GREEN。
5. 迟到RPC确认在取消后仍initialized，实际RED；Gateway增加回复后的context fence，默认原Client不改，最终GREEN。
6. 最终严格account字段检查导致实际Native再次拒绝，安全诊断只输出keys，发现workspaceRouting。固定v2/account.rs为experimental Option，非实验生成schema不列该字段但Native序列化null。null接受测试RED后只允许缺省/null，非null路由继续拒绝，不采用其backend/account override。保留失败的最终日志并重跑全部相关检查。
7. budget仍以真实每次Gate→Store发送计，不用Native事件统计/terminal替代；原生RPC interrupt不等于持久cancel intent或OS停止。语义边界明确保留，后续正式Adapter/Controller须闭合它们。

复现（从工作树根目录，明确pin/隔离HOME与白名单环境）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-gateway.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-gateway-native.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime -run '^TestManagedPinnedCodex(Gateway|HTTP|Bootstrap)' -fusion-native-codex /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-gateway-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

日志仅含合成结果、方法/字段名和拒绝时字段类型/长度，不含Native私有请求/账号/鉴权正文。失败/诊断与最终通过日志保留，不把诊断通过或SKIP当作生产功能通过。源码、packet与公开/既有Native schema hash记录在artifacts.json/reference-sha256.json，按所属Git提交回读。旧item manifest只在旧所属提交校验。
