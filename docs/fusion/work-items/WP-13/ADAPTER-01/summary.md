# WP-13-ADAPTER-01：可信 Grok 只读 Adapter

基础 HEAD：`8303873fb88156a2f3235dbe71672a03bb339fda`。本项done，parent WP-13 in_progress。实现公共Runtime接口的Probe/Start与Observation/VerifyStop/Release；生成私有prompt、Native UUID/argv、ReadTools/Gate/channel、调用Supervisor。真实Native受管只读/取消/停止链通过；Resume、write/effort、真实subscription NativeForwarder/准入与产品注册仍未完成。合同：[grok-adapter.md](../../../contracts/grok-adapter.md)。

[构造器缺失RED](wp13-adapter-red.log)后实现。3项Host tests覆盖6种缺失service、19种caller launch controls/非法prompt/路径/timeout/write/quota/owner/identity、缺少pin/unsupported Resume/未开始Observation/外部StopProof；[targeted GREEN](wp13-adapter-green.log)3PASS。测试提供真实Store/Manager/Scheduler，但服务Inspector/Quota与所有upstream均合成，不能提升真实准入。

共享Native helper首次遗漏旧调用点的新增bool参数，[compile RED](wp13-adapter-native-first.log)保留后修正调用点。首次实际固定CLI [8场景结果](wp13-adapter-native-first-exec.log)全部通过，实际确认--prompt-file在固定版本支持，不以不同revision的publicreference代替Native事实。

新prompt snapshot用例先发现caller切片能在Current callback中改变私有输入：[实际RED](wp13-adapter-input-red.log)。将复制移到Start入口服务前，private prompt IO后再CheckPrepared/Current，未削弱任何断言；[GREEN](wp13-adapter-input-green.log)同时证明20byte snapshot、40020byte prompt-file，以及Current drift时无prompt/launch/预算。Current接受的Binding深复制，caller修改Tools/Capabilities不影响Native绑定。

[最终Native回归](wp13-adapter-native-final.log)6个top-level/27场景PASS，其中10项Adapter实际CLI、1项Adapter prelaunch drift；另含前项8 managed channel与8 legacy protocol/read场景。Adapter成功/Read输出准确“Fixture ready.”、Native UUID对应实际StopProof，独立flags false；失败/取消Observation没有成功文本，foreign generation拒绝；每项实际停止证明核验与可信Release，项目/外部fixture文件保持。预算为文本2、Read3、429 retry3、失败instruction2、budget不足1、inflight cancel2；失败包含private config、outside read和write instruction拒绝。只依赖Native end标签会遗漏title/retry，因此逐HTTP由真实Scheduler/Store记账。

[全Fusion race](wp13-adapter-fusion-tagged-race.log)：409PASS/16SKIP/0FAIL；[CLI/GUI/full tagged vet](build-results.json)exit0。16SKIP为显式Native/fixture诊断，上述Grok另行完成，不推定未执行的其它Native测试通过。共享Supervisor/Claude源码本项未改，前项8303873拥有的Claude11场景证据保留，不重复声明为本项新执行。Store/schema/API/master开关、依赖与日常认证未改，Jev off，真实模型/Quota调用0。

复现：macOS/arm64、Go1.26.3、clang/SDK、sandbox-exec/固定Native1.0.48 hash、公开Go caches，无登录/API key；临时HOME/XDG/env白名单/Store/工作区，真实wait与清理。命令：

```sh
set -e
helper=docs/fusion/work-items/WP-13/CALLS-01/run-go.py
python3 "$helper" .fusion-dev/implementation/adapter-native-recheck.log \
  test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/runtime/grok -run '^Test(Grok(CallGatePinnedNative|ControlledReadPinnedNative|AdapterPinnedNative)|ManagedPinnedNativeGrokChannel)' \
  -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
python3 "$helper" .fusion-dev/implementation/adapter-fusion-recheck.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/...
python3 scripts/fusion/build-dev.py
```

没有Native flag时诊断SKIP，错pin应失败。正式counts/flags见[test-results.json](test-results.json)，源码与packet hashes见[artifacts.json](artifacts.json)。raw logs保留空白，diff-check排除*.log；JSON/gofmt/links/凭据排除/hash独立检查，primary原2个DSStore保留。

本项交付可由可信control wiring调用的readonly Adapter，未注册产品Worker或真实Forwarder。Current/Inspector必须对实际项目权限/Source稳定性/账号/Quota/计费做独立验证；Host TOCTOU、敏感Source与准入没有因fixed argv消失。Native文本属于未独立核验产物，外层必须task/project鉴权再展示，不作为最终工程验收。写/多工具/offset/binary、Native effort/Resume、X登录/Heavy/pool/生产Forwarder与完整WP13/final Gate仍未完成。

全回归后只加强Native inflight阶段的Observation负断言：[定向复核](wp13-adapter-inflight-observation.log)PASS，running时不能拿到terminal文本；生产代码未改，不重复full/build。该用例随后取消/wait/StopProof/可信release均通过。
