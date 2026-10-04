# WP-15 任务控制 API 进度

状态：`in_progress`。子工作项 [WP-15-PREVIEW-01](PREVIEW-01/summary.md) 已完成：受鉴权的无调用计划预览、冻结提交和任务读取组件。

[SUBMISSION-RECEIPT-01](SUBMISSION-RECEIPT-01/summary.md) 已补齐 schema 6 持久提交身份，原 preview_id/plan_hash/key 在预览清理、Server 重建及 Store/宿主重启后仍能读回同一任务的当前状态。回执与任务/计划/预算/幂等记录/事件同事务，真实本机 HTTP/Native 桥重启及来源撤销已验证；不执行 Runtime、不重置计数，未登记项目拒绝恢复。未提交预览重启后仍失效，旧任务不猜造 preview 身份。窗口原请求持久保存及阶段运行 UI、真实供应商准入仍未完成，父包保持 in_progress。

[TASK-INDEX-01](TASK-INDEX-01/summary.md) 完成已登记项目的任务分页只读接口和精确 Native GET 通路，固定 32 项、同项目 taskID 边界、有限目标摘要和同一行 Task ETag。实际本机 HTTP 创建 33 任务、分页/桥接读取与 Source 撤销验证通过；不触发检查/执行，不改变准入。当前可信执行宿主和阶段配置 GUI 已有接线，真实供应商 Factory/账号/quota 与任务工作台仍待完成；下文保留各历史组件当时的边界。OpenAPI 当前 29 路径 33 操作 63 样本。

[CODEX-01](CODEX-01/summary.md) 完成可信 Codex Adapter → Controller 接线核验，并补齐 Adapter-specific preflight：prompt/时限/来源/路线/身份的已知错误在 intent 前拒绝，validator 获得冻结副本，慢检查后仍重核管理/来源。固定 Codex 11 场景真实 wait/StopProof/release 通过；同 key 不重放，HTTP 断线不取消，Pause/Cancel/Close 精确 owned lifetime。Grok/Claude 25 次既有 Native 与 Codex Adapter 13 次另回归通过。完整 Fusion 514 PASS/29 SKIP/0 FAIL，构建/vet 通过。真实账号/Forwarder/quota、产品 HTTP/Factory 和完整闭环继续未完成。

[EXECUTION-HOST-01](EXECUTION-HOST-01/summary.md) 完成进程内可信 RuntimeFactory 的本机服务接线：准确声明路线、登记来源/read/write、RequireSource Controller、独立额度源、撤销及按序关闭/超时重试。鉴权 HTTP 驱动固定 Grok Native 5场景/5进程通过；既有 Grok/Claude 20场景/25进程回归通过。完整 Fusion race 465 PASS/23 SKIP/0 FAIL，CLI/GUI/vet通过。草稿 CLI仍关闭执行，真实 Factory/账号准入、产品UI、Codex Native、工程闭环和最终Gate未完成。

[CHECKPOINT-API-01](CHECKPOINT-API-01/summary.md) 已接通owned成功run的可信归档管理入口、固定原Adapter生产端与Scope/管理/关闭前后重查；修复Close标志先到、取消回调后到的竞态。实际Native归档→准确恢复覆盖Read/取消/断线，重复归档无新增调用。完整Fusion race449 PASS/20 SKIP/0 FAIL，OpenAPI26 paths/30 operations/53 samples/9 negatives，CLI/GUI/vet通过。产品bootstrap/Worker/GUI和真实路线准入继续未完成。

[RESUME-API-01](RESUME-API-01/summary.md) 已将明确成功 checkpoint 恢复接入持久幂等身份、Controller 和 Management Handler；普通 Start/Continue 不隐式恢复。新增实际 Grok 恢复/取消/断线，原 Grok 和 Claude 兼容性回归通过。完整 Fusion race 442 PASS/20 SKIP/0 FAIL，OpenAPI 25 paths/29 operations/52 samples/8 negatives，CLI/GUI/vet 通过。真实执行 bootstrap/Worker/GUI、账号与整个 Source 准入仍待完成。

[WP-15-GROK-01](GROK-01/summary.md) 已补齐原始 GrokChannel 在 intent 前的拒绝，并验证可信 Controller→Grok Adapter→固定 Native 的 7 个实际生命周期场景、幂等 receipt、HTTP 断线、暂停/取消/关闭和实际 StopProof/release。全量 Fusion race 409 PASS / 17 SKIP，固定 Claude 控制器 3 项兼容回归及 CLI/GUI/vet 通过。产品执行注册、真实路线准入、Native Resume 和完整工程闭环仍未完成。

当前 Handler 已由 fusion-control 注册到独立草稿 listener；生产执行 Controller/GUI 尚未接线，旧执行入口继续关闭。完整 WP-15 仍需真实项目/Runtime/额度来源接线及新增行为对应的合同扩展；当前已实现 Handler 的 OpenAPI 合同已按下述 OPENAPI-01 及后续扩展完成。预算与输入事务、阶段计划修订、启动/阶段取消、暂停/继续、默认层读写、额度、预设组件及持久 sequence SSE 的已完成子项见下文。实际 UI 合同随后按最终接口交付。此组件测试不作为 T13/T25/T28/T33–T36/T43/T45/T49/T50/T56 的完整最终验收。

[WP-15-BUDGET-01](BUDGET-01/summary.md) 已完成预览预算、提交预算事务和预算读取；任务、快照、幂等与预算共同提交，重试不退还已消耗的次数。162 个 Fusion tagged race tests 通过。剩余阶段修订、控制、事件流与接线继续实施。

[WP-15-EVENTS-01](EVENTS-01/summary.md) 已完成持久序列 SSE、重连补读、当前管理 Context 验证与撤销、连接/页/写入界限。实际 loopback HTTP 验证通过，完整 Fusion tagged race 173 个通过。剩余 If-Match、阶段控制、预设/额度与生产接线仍未完成。

[WP-15-REVISION-01](REVISION-01/summary.md) 已完成尚未开始角色的计划预览、If-Match 修订及读取组件；冻结已开始/已结束绑定，原子重查竞态，保留活动凭据、历史与预算。189 个 Fusion tagged race tests、CLI/GUI 编译和全仓 vet 通过。暂停/取消/继续、预设/额度、OpenAPI 与生产/UI/调度接线仍未完成。

[WP-15-START-ONCE-01](START-ONCE-01/summary.md) 已完成启动 intent 的持久幂等映射、task generation CAS 和新建/重读区分，与容量预留同事务提交；重启 unknown、终态重试、故障回滚及 schema 2→3 数据保留通过验证。此项是启动控制的存储基础，尚未注册产品 endpoint 或连接实际 Runtime；父工作包继续 in_progress。

[WP-15-PREPARE-ONCE-01](PREPARE-ONCE-01/summary.md) 将上述事务接入 Scheduler；已提交请求在准入检查之前只读 receipt，并发准入失败时也能只读另一个已提交的相同请求，新 key 仍需实时准入和 generation CAS。237 个 Fusion race tests、原生 Claude Code 合成上游 8 个场景及编译/vet 通过。真实控制器/产品 endpoint、暂停/取消/恢复、预设/额度、OpenAPI 与 UI 接线继续实施。

[WP-15-CONTROLLER-01](CONTROLLER-01/summary.md) 已实现可信服务端控制器的启动、幂等重读、owned lifetime、取消/Close、未知执行保留及可信退出释放。固定 Claude Code 经此控制器完成合成上游一次执行/Permit、HTTP 断线与重试不重复执行、真实 Supervisor StopProof 释放。248 个 Fusion race tests 通过，8 个条件跳过；编译/vet 通过。产品 HTTP 鉴权与接口注册、实际项目/账户配置、暂停/恢复/返工、预设/额度、OpenAPI 和 UI 仍未完成。

[WP-15-EXECUTION-API-01](EXECUTION-API-01/summary.md) 已将内部 Handler 的受管理鉴权启动/取消/运行记录读取接到可信 Controller；Task strong ETag 跟踪 revision/generation/state，重读原启动映射不重复执行，慢预检查期间管理身份撤销会阻止新 intent。258 个 Fusion race tests、CLI/GUI 和 vet 通过，新增执行接口 OpenAPI draft。完整 OpenAPI、产品 listener/GUI 注册、实际配置、暂停/恢复/返工、预设/额度仍未完成，父包继续 in_progress。

[WP-15-CANCEL-REVISION-01](CANCEL-REVISION-01/summary.md) 将取消的 Task revision 条件放入 cancel_intent 写入事务，关闭未来角色修订保持活动 generation 时的预检查竞态。旧版本不会取消 Worker，当前版本可取消历史计划的活动 run；终态重读仍检查当前版本。父包继续 in_progress。

[WP-15-QUOTA-API-01](QUOTA-API-01/summary.md) 已完成 Management 保护的项目额度缓存读取/手动刷新、精确来源登记、过期/撤销/失败状态和共享 pool 展示，以及全服务八个采集 Worker 名额。采集器为 fixture；真实额度来源与产品注册继续接线，完整 OpenAPI、预设/暂停/恢复及 UI 未完成。

[WP-15-PRESETS-01](PRESETS-01/summary.md) 已完成独立持久化的五角色预设版本、明确版本应用、全绑定覆盖、Task 原子来源引用、配置只读与受鉴权的列表/读写 API。schema 4 迁移保留旧任务/幂等/预算/事件/容量；实际固定 Native 合成回归通过。默认层及完整 OpenAPI 已由后续工作完成；产品接线、暂停/恢复与 GUI 继续实施。

[WP-15-OPENAPI-01](OPENAPI-01/summary.md) 已完成当前 Handler 的 17 路径/19 操作合同、字段/nullable/error media type 核对、官方文档 schema 和 27 个实际返回样本的离线检查。API race 67 项全部通过，API vet 通过。未来控制与产品接线仍须扩展合同，父包继续 in_progress。

[WP-15-DEFAULTS-01](DEFAULTS-01/summary.md) 已完成持久化 global/project 默认层、五角色整体覆盖/继承、不可变历史与 If-Match、当前配置投影和事务内 stamp 检查。跨 Server 已提交请求重读保持原任务，额度来源与选择版本分离且不刷新观测。schema 5 保留历史数据与原 payload hash，005 失败回滚；21 路径/25 操作、36 样本合同已校验。产品接线、暂停/继续和 GUI 仍未完成，父包保持 in_progress。

[WP-15-PAUSE-STORE-01](PAUSE-STORE-01/summary.md) 已完成完整 TaskVersion 的原子暂停/继续存储基础、空闲 generation fencing、活动停止意图，以及 Finish/可信 release 的暂停收尾。当前执行 proof 不能代替旧执行证明；未知/取消副作用保持 needs_review。新增 11 测试通过，全量 Fusion race 319 PASS / 8 SKIP，CLI/GUI/vet 与既有 Native 合成回归通过。Controller、HTTP、OpenAPI 和实际暂停 Native 验证未接入，完整功能继续实施。

[WP-15-PAUSE-API-01](PAUSE-API-01/summary.md) 已完成 Controller owned Pause/Continue、晚到 Handle 取消竞态修复、Management/完整 Task If-Match 的 HTTP 两操作和安全不确定意图回复。新增固定 Native inflight Pause 实际 wait/StopProof/release→needs_review 验证，继续不会盲重跑；全量 Fusion race 331 PASS / 9 SKIP，CLI/GUI/vet 通过。当前合同 23 路径/27 操作/44 样本。产品 bootstrap/listener/GUI、真实路线准入、Native session/检查点恢复和其他闭环控制仍待实施，父包保持 in_progress。

[WP-15-CANCEL-STORE-01](CANCEL-STORE-01/summary.md) 已完成整项 CancelTask 存储基础，覆盖尚未启动/paused、活动 owned 取消与停止证明收尾、暂停升级取消、完整版本/lease fencing、全历史执行停止证据与重启 unknown。10 项新增测试、全量 Fusion race 341 PASS / 9 SKIP，CLI/GUI/vet 和既有 Native 回归通过。未注册整项取消 HTTP/Controller/Native 调用链，随后接线；父包保持 in_progress。

[WP-15-CANCEL-API-01](CANCEL-API-01/summary.md) 已完成 Controller owned 整项取消、Management/全 Task If-Match 的 HTTP cancel、安全意图 receipt 和 OpenAPI/capture。12 新测试；固定 Native inflight 整项取消真实 wait/StopProof/release→cancelled，预算不退/旧启动不重放。全量 Fusion race 352 PASS / 10 SKIP，CLI/GUI/vet 通过；当前24路径/28操作/49样本。产品 bootstrap/listener/GUI、真实路线准入和剩余工程闭环继续实施，父包 in_progress。

[WP-15-PROJECT-SOURCE-01](PROJECT-SOURCE-01/summary.md) 已完成 Git 外私有本机项目配置加载组件：已有文件夹及明确 read/write、精确内置路线元数据、五角色空选继承、预算边界与来源/文件夹身份重查。配置不能授予真实准入，实际 Handler 可读未准入登记而 preview 拒绝；产品 CLI/listener/Resolver/GUI 尚未接线。父包保持 in_progress，真实项目选取与账号/计费/额度仍待验证。

[WP-15-CONTROL-HOST-01](CONTROL-HOST-01/summary.md) 已完成独立本机 fusion-control 草稿服务：私有随机管理文件、loopback/Host/Origin、源/目录/管理文件变化撤销、持久重启、SSE及Close，并修复run-dev缺data子目录与SIGTERM不转发。实际隔离launcher→CLI→HTTP→SIGTERM退出验证通过。全量Fusion race365 PASS/10 SKIP，CLI参数2 PASS、编译/vet与无tag编译通过。路线未准入，Controller/Native/额度/GUI及工程闭环仍待接线，父包保持in_progress。

后续 [WP-15-SOURCE-GUARD-01](../WP-15/SOURCE-GUARD-01/summary.md) 完成私有来源记录接入执行/归档边界，固定Grok/Claude Native源漂移拒绝成功并实际停止/释放通过。完整Fusion460PASS22SKIP0FAIL，Native7顶层/17子测试通过；整体产品登记/真实路线/最终Gate仍未完成。

[WP-15-CODEX-HOST-01](CODEX-HOST-01/summary.md) 已补充 Codex authenticated loopback HTTP/Factory 的5个实际 Native 生命周期场景、2类 preintent 拒绝、鉴权和幂等及真实停止后 Store 重开。Grok 同路径5次进程回归通过；完整 Fusion race 514 PASS/30 SKIP/0 FAIL。现有生产代码无需修改，真实账号/Forwarder/quota/生产注册、CLI/GUI、写入/工具/恢复、工程闭环和最终Gate仍未完成；父包继续 in_progress。

[WP-15-PROJECT-INDEX-01](PROJECT-INDEX-01/summary.md) 已完成鉴权只读项目列表，供阶段界面发现已有登记；不公开目录/凭据，不新增登记/执行准入。API97PASS、bootstrap19PASS/2SKIP、构建/vet通过，OpenAPI27路径31操作56样本同步。WP-16页面与GUI鉴权、真实供应商准入及最终Gate仍待完成。
