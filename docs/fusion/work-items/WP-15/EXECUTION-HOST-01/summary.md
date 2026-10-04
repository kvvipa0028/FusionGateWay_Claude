# WP-15-EXECUTION-HOST-01：本机可信执行服务接线

基线 `db18554bc7b67688d233a3b6fcdca6ce3e31a6ea`。完成进程内 RuntimeFactory、来源绑定的 Scheduler/Controller、准确登记路线及独立 quota catalogue 接入本机鉴权 HTTP；合同见 [execution-host.md](../../../contracts/execution-host.md)。原草稿 CLI 保持 execution off。真实 Factory/账号/计费/模型/Quota/pool 准入、产品 CLI/GUI、Codex Native、工程闭环和60类最终 Gate仍未完成，父 WP-15保持 in_progress。Jev off，本项真实模型/额度调用0。

具体改动：

- Factory 仅补入已验证声明路线的准入/能力，不能改变 ID/revision/model/account/provider workspace/credential/runtime/plugin/effort/billing path，也不能替换五角色层或预算。初始化失败保留并执行已返回 cleanup；错误不披露原始私有内容。可信工厂是服务端授权边界，不来自 HTTP/CLI，也不是凭一个布尔值证成真实准入。
- Environment 提供同一 Store/Manager、带登记前后检查的 Scheduler 和 Current；Controller 明确 RequireSource=true。Resolver 的 Guard 必须对应该登记源和实际 Cwd，另一项目的有效 Guard 拒绝；Spec write 与 Inspection writer 预留受登记 write 限制，Runtime root 不包含任何项目也不在项目内。HTTP body仍只提交公开任务控制参数。
- quota Current/Fetch 包装核验生命周期/来源及原用途 callback，GET不采集、刷新不重写年龄/池。脱离请求的 Broker读取仍受 owned取消和服务 WaitGroup 管理。
- parent取消/来源身份撤销会取消 owned Runtime。关闭先停派单/撤销/取消/关闭HTTP，再等待 Handler、Controller和quota，Factory cleanup后才关闭Store。CloseContext超时不假报停止、不释放未证容量、不关仍使用的Store；可继续等待。Close兼容调用最多等待30秒。

验证证据：

- 初始新接口测试编译RED（缺 RuntimeFactory/OpenExecutionControl/CloseContext），实现后GREEN。首次 HTTP fixture 提交误用 `/control/v1/tasks` 返回404；按既有合同修正为 `POST /agent/v1/tasks`，没有改变产品路由或弱化拒绝断言。
- 新增5项Host测试、31个PASS子测试：17类路线/初始化拒绝；7类来源/权限/Runtime root/慢Resolver变化拒绝且不写intent；执行关闭超时/重试/Store顺序；7类quota身份/源撤销；脱离请求采集取消/等待/超时。最后新增root边界由完整最终套件验证。
- 固定 Grok1.0.48，经实际 authenticated loopback HTTP preview→submit→start→旧key重读→GET/取消/关闭，5场景/5次Native：success、cancel、source-revocation、close、parent-cancel。共10次合成模型 HTTP/持久预算，重复启动不重Resolve或新增进程；准确模型/credential identity固定，真实wait/StopProof/release、停止后重新打开Store回读与原项目文件保持通过。没有读取真实key，也没有真实X上游。
- 固定 Grok/Claude既有控制器回归7顶层PASS/17子测试PASS，20场景/25次Native，含Read/断线/暂停/取消/Close/来源漂移/归档准确UUID恢复。此处内部Controller证据与上面Management HTTP证据分别记录，未声称所有供应商经HTTP或真实账号准入通过。
- 完整Fusion race **465 PASS/23 SKIP/0 FAIL，694个PASS子测试**。SKIP包括显式Native测试；Native另跑，不能算成默认套件已运行。Go1.26.3 CLI/GUI编译、完整vet exit0。实际草稿launcher→CLI→HTTP→SIGTERM诊断exit0，正确鉴权配置、空选择preview拒绝、旧model路径关闭、来源变化拒绝、私有token不输出通过。
- graph刷新到13677 nodes/120292 edges。最终运行代码/行为验证完成后只修改文档及证据，无需重复相同检查。记录的源码/packet SHA256须在所属提交中回读，不用后续修改树代替原证据。

复现：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-execution-host-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-execution-host-native.log test -race -v -count=1 -timeout=240s -mod=readonly -tags fusion,nogui ./internal/fusion/bootstrap -run '^TestExecutionHostPinnedNativeHTTP$' -fusion-host-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-execution-host-regression.log test -race -v -count=1 -timeout=240s -mod=readonly -tags fusion,nogui ./internal/fusion/control -run '^TestControllerPinned(Grok|Native)' -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 -fusion-control-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/check-control-host.py
```

Factory回调必须尊重取消并为自己的Adapter/额度服务独立核验权限、路线和预算；可信服务忽略取消时关闭保留资源并持续等待，不擅自杀其他进程。来源是有界观测，非外部写锁或原子树；已外发副作用不会自动撤销。该工作项完成后继续真实生产Factory/产品入口、Codex Native与原计划其余包，不以内部接线完成缩减整项目标。
