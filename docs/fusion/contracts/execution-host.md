# 本机可信执行服务接线

`bootstrap.OpenExecutionControl(parent, sourcePath, root, addr, RuntimeFactory)` 将已登记项目、同一 Store/Manager/Scheduler、可信 Controller 与 quota catalogue 接入独立 loopback HTTP 服务。它是进程内服务端接口，不能由 HTTP JSON、项目声明中的 `admitted` 字段或 CLI 开关调用。`fusion-control` CLI 继续使用草稿入口；没有真实 Factory 和完整准入证据时不开放用户任务执行。Jev off。

Factory 获得只读 Loaded、当前登记检查、同一 Store/Manager 和带宿主检查的 Scheduler。它负责实际账号/凭据用途、计费、Quota/pool、Native 能力、数据权限及验证服务的独立准入，并返回可信 Inspect/Resolve/可选 SelectAuto、已验证路线、额度源与 cleanup。初始化阶段不能启动任务或提前调用 Scheduler；后端 Adapter 必须使用该 Scheduler/Manager 和 Current。不存在默认供应商、普通 API fallback 或内置合成准入 Factory。

路线仅能匹配某个已登记项目的准确声明。ID/revision/model/account/provider workspace/credential identity/runtime version/plugin/effort/default/billing path 必须完全一致。Factory 仅补入 `Admitted`、`BillingKnown`、已验证 Capabilities 与 LockEnforcement；重复、未知或尚未准入路线拒绝整个初始化。未返回的声明仍未准入；原 global/project 五角色配置、默认预算不由 Factory 替换。初始化失败会执行已返回的 cleanup 并释放本服务拥有的 listener/Store，不输出原始服务异常。

新执行使用 `RequireSource=true`。Resolver 必须从对应登记原目录产生真实 Copy/Guard，`BoundToSource` 比较私有来源，`ValidFor` 校验实际副本与原来源；来自另一目录的有效 Guard 也拒绝。检查登记当前身份和 read/write，写入 Spec 与 Inspection 的 writer 预留受项目 write 约束；Runtime root 不得与任何登记项目互相包含。Inspection 前后核验宿主及准确登记路线，准入值和额度仍由原 Scheduler 校验。管理请求只携带已有 task/role/If-Match/key，不提交路径、argv、key 或真实准入标记。

quota catalogue 只接受已登记项目及准确 route/account/provider workspace。GET 不采集，刷新沿既有鉴权和 Broker；可信用途检查仍由 source.Current/Fetch 承担，宿主包装在调用前后核验私有登记、生命周期与关闭状态，不改写观察时间或证明池。脱离首个 HTTP 请求的共享读取也纳入宿主 WaitGroup，生命周期取消传给该读取器；读取器尚未结束不能关闭其依赖服务或 Store。

关闭先拒绝新 Handler、撤销管理、取消 owned Runtime 与额度读取、关闭 listener/HTTP 连接；随后等待已有 Handler、Controller 的实际 Handle/归档工作和所有 owned quota Fetch，最后调用 Factory cleanup，再关闭 Store。`CloseContext(ctx)` 超时仅表示本次等待结束，后台继续等待实际工作；后续可重试等待，不释放未证明容量、不退款、不重放。兼容 `Close()` 等待最多30秒；取消 parent 即使未调用 Serve 也开始同一关闭流程。来源/目录/token 失效时 watcher 撤销管理并取消 owned Runtime，现存草稿模式保持原503行为。

本项验证新增5项 Host测试及31个子测试，覆盖路线/额度篡改、来源错配、权限、慢 Resolver 撤销、初始化回收、执行和脱离请求的采集关闭超时/重试。实际固定 Grok Native 由 authenticated loopback HTTP 完成 preview→`POST /agent/v1/tasks`→start→同 key 重读→run GET/取消/关闭；5场景/5次 Native，10次合成 HTTP，实际 wait/StopProof/release 与持久重启回读通过。固定 Grok/Claude 既有20场景/25次 Native另回归通过。

证据见 [EXECUTION-HOST-01](../work-items/WP-15/EXECUTION-HOST-01/summary.md)。此次没有真实账号、模型或额度调用；内部可信 Factory 接线不能替代真实 Factory 注册、产品启动/UI、Codex Native、工程闭环或60类最终 Gate。来源复核仍为有界观测，未建立外部写锁或树的原子快照；见 [workspace-source-guard.md](workspace-source-guard.md)。
