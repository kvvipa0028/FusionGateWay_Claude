# WP-15-CHECKPOINT-API-01：可信归档的管理入口

基线 `7c26dc367124966d71bc2588daa79411cba1f740`。完成 owned 成功 run 的内部 Controller/Management checkpoint 生产入口与 Grok 实际归档→恢复验证。WP-15/WP-13/full goal 仍 in_progress，产品执行 bootstrap/Worker/GUI、真实账号/计费/Quota/pool/当前项目授权/整个 Source 准入仍未完成，60 类最终 Gate not_run。合同：[task-checkpoint-api.md](../../../contracts/task-checkpoint-api.md)。

Controller 在启动时保存 Backend.Checkpoint，真实观察 Wait/proof/Release 成功时保存私有终态 run 快照。Checkpoint/CheckpointAuthorized 要求当前 ready Task、精确全 TaskVersion/generation、同 Controller 的已完成 succeeded job、StoppedVerified/Released、完整 Store run 未改变、reservation确实不存在。后续 Resolve/config 修改不替换原生产端，unowned/重启/未知/失败/取消/未停/释放失败无法归档。BindGrokCheckpoint 只调用原 Adapter.Checkpoint/私有 Archives，继续严格 HMAC/14 Native 文件/批准 Read/Source/proof 核验，输出仅 opaque ID/digest。

新增 Management POST `/control/v1/tasks/{task_id}/runs/{run_id}/checkpoint`，单个强 Task If-Match、正文严格 {}，不用 caller ref/Native/Root/argv/grant/proof。200 返回 ref与安全原 RunView/Location/X-Fusion-Task-ETag；不设置其它资源的 ETag、不输出私有数据。错条件/状态412、缺条件428、越权/结构/能力/停止失败按固定错误拒绝。撤销在前后与回复前重查；Backend错误/坏ref不回显。已私有发布但授权随后失效时拒绝披露，不宣称文件系统已回滚。

操作沿用16个work slots和wait group，Context同时接受HTTP请求及Controller lifetime取消。Close取消并等待真实归档工作，非合作Backend导致Close超时不宣称完成。确定性RED证明：关闭标志置位、取消AfterFunc尚未到达时旧代码会披露ref；新增关闭/lifetime同步前后重查后GREEN。生产端不启动Native、不花模型/Quota/预算、不改Task/event。既有私有Archives本身持久化，成功重复生产ref稳定；不新增DB表/schema，不构造重启后unowned停止证明。调用方保存reference后仍可使用原有持久Info/Restore消费者。

验证：

- 新 Host **7 顶层 PASS/23 子测试 PASS**（4 Controller、3 API）：原后端固定、重复/未完成/unowned拒绝、10类scope/失败/revoke、Close取消/非合作等待、关闭回调竞态、真实loopback HTTP、安全字段/条件/授权/未绑定/固定错误/held预算事件。
- 实际固定Grok经新的Controller生产入口归档两次取得相同ref，再恢复准确原UUID：文本、批准Read历史、恢复中取消、请求断线四场景，共8次Native启动。Read为旧3HTTP/新1HTTP，显式四次任务预算；其它为旧2/新1。归档重复无新调用，恢复重复无新启动，真实StopProof/release通过，Source保持。
- 合并原Grok7生命周期及Claude3兼容场景：**5 顶层 PASS/11 子测试 PASS，14场景/18次实际Native启动**。Native证据经过Controller，HTTP鉴权证据使用合成Backend，未将分开的证据说成真实账号经产品HTTP端到端。
- 完整Fusion race **449 PASS/20 SKIP/0 FAIL**，643个PASS子测试；skip不算通过，显式Native另跑。最终修复后Go1.26.3 CLI/GUI build、vet均exit0。
- OpenAPI标准/离线样本：**26 paths/30 operations/53 actual Handler samples/30 covered operations/9 negative schema cases**，含新增checkpoint200与ref禁止Native字段反例；prod执行注册false。
- graph最终13603 nodes/119479 edges；之后仅文档/证据包。JSON/链接/摘要/gofmt/diff及真实key排除提交前核验。真实模型/Quota调用0，Jev off。

记录的RED包括缺Controller接口编译、API路径未注册404、OpenAPI缺路径，以及关闭标志竞态。最初测试猜运行中错误为ErrReconcile，实际当前非ready状态按明确前置条件冲突拒绝；改正错误码oracle，保留不调用producer的断言和成功/证明/释放门槛。另明确reservation仅ErrNotFound可通过，不把其他数据库错误解释成已释放。

复现（独立HOME/XDG helper第一个参数必须是日志路径）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-checkpoint-host.log test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/control ./internal/fusion/api -run '^Test(ControllerCheckpoint|CheckpointAPI)'
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-checkpoint-native.log test -race -v -count=1 -timeout=180s -mod=readonly -tags fusion,nogui ./internal/fusion/control -run '^TestControllerPinned(Grok|Native)' -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 -fusion-control-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
python3 scripts/fusion/check-openapi.py --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json --samples docs/fusion/work-items/WP-15/CHECKPOINT-API-01/wp15-checkpoint-handler-samples.json
```

本项不提升真实subscription、独立模型/effort/计费或wholeSource结论，不完成写恢复/五阶段工程闭环/返工/最终Gate。已有私有key rotation/清理和完整产品恢复管理仍须后续按原范围交付；原Source/当前项目授权缺口继续明确保留。
