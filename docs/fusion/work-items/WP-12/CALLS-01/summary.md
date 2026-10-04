# WP-12-CALLS-01：Codex 逐次 Responses HTTP 控制

基线 `d28abd8afee468e7f10cb43f0a03e841b03ee9a5`。完成内部 CallGate：冻结 Binding/Claims/target/effort，Stage 鉴权、per-run dispatch、逐次持久预算、可信单次订阅发送和完整有界 SSE 拒绝。合同见 [codex-call-gate.md](../../../contracts/codex-call-gate.md)。父 WP-12继续 in_progress；生产 Native Adapter、真实官方登录/Forwarder、全部子调用与模型准入仍未完成，最终 T01–T60 Gate不升级，Jev off。

验证使用真实 Store/Manager/Scheduler 和合成 subscription Inspector/Forwarder，没有真实 Native HTTP、登录、模型或额度网络请求。固定 Native 的启动/停止证据仍归属前项 d28abd8，不在本项重算。

验证结果：

- 新增16个顶层测试、93个通过子测试。Codex 包最终 race **41 PASS/0 SKIP/0 FAIL，127个通过子测试**；完整 Fusion race **486 PASS/24 SKIP/0 FAIL，835个通过子测试**。24个 SKIP未视为本次执行通过。
- 持久 MaxCalls=2：第一次429、第二次成功、第三次403；实际两次 Forwarder send、UsedCalls=2。失败与重试均计入，不退款。quota unknown、data permission或admission proof改变后，下一次拒绝，不额外消费预算。
- 覆盖错误模型/effort、工具与历史工具、summary、非stream/stored、service tier/metadata、query/absolute/encoded path、压缩、subagent、超限、重复/case aliases/null/trailing、原始及转义凭据反射。错误鉴权不污染有效 run。
- 冻结字段与 callbacks 参数有副本；Permit后撤销/身份漂移、发送/读取期间变化、transport错误、并发等待取消均拒绝交付。源/registry/current能力仍由可信接线提供，不用测试 Boolean 升级生产准入。
- 响应支持保守的文字消息、delta/part、无summary的reasoning、CRLF、从已独立核验上游报告取得模型；完整验证后才写出。错误/缺失模型报告、末尾模型/ID/输出不一致、未知事件/工具、错误usage、缺终态、未结束新增项、invalid UTF-8、私有数据均拒绝，零部分 SSE。
- Go1.26.3 CLI/GUI/full vet exit0；graph13774 nodes/121471 edges。最终行为验证后未修改源码，仅文档与 packet。

RED→GREEN与判断记录：

1. 初始测试因缺少 CallGate接口编译失败；实现后首轮仅 query用例失败。该 fixture使用 `?key=secret`，被 Manager 在进入 Gate前正确拒绝；按现有合同改为 `?model=other` 检查已鉴权 URL拒绝，保留独立鉴权不污染测试。
2. 两个实际行为 RED：Current callback内撤销 model grant后仍HTTP200、未完成 added item后仍HTTP200。增加 callback后的 grant fence及 completed前的未结束项核对，最终 GREEN。
3. 持久 fixture最初误将 EffortSelection.Value作为指针；按现有源码的string修正，未更改接口或放宽预算断言。
4. 私有保护 RED：invalid UTF-8放在SSE comment仍HTTP200、ForwardResponse JSON可序列化exported body reader。增加全流 UTF-8校验与Body JSON排除；格式化保持私有，最终 GREEN。
5. 文字delta挂到reasoning项实际RED HTTP200；固定added item类型，文字事件只允许message，done类型与added一致，最终 GREEN。
6. 实施判断：先交付受控 HTTP组件，暂不开放真实 Native generation。冻结源码将chatgptAuthTokens标为仅OpenAI内部使用，不能作为生产登录替代；未用普通API计费/冒充身份解决订阅接入。代价是未核验功能保守拒绝，完整父工作包继续实施。

复现（临时 HOME/XDG、白名单环境、无真实账号）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-gate.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

本项源码、packet与参考源码 SHA分别记录在 artifacts.json/reference-sha256.json；所属Git提交应回读校验。真实凭据不进入文件、日志或hash清单。后续须接通受管 Native的私有登录与stage grant、限定网络出口、原生工具/子调用控制、真正 subscription Forwarder与额度准入，再核验Native turn终态及Supervisor停止；本项不等于上述完成。
