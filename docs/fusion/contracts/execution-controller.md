# 可信执行控制器合同

`internal/fusion/control.Controller` 属于服务端内部编排，不是 HTTP DTO。Config 必须提供同一 Store 的 Scheduler 和可信 Resolve。请求只携带 task/role/plan revision/expected generation 与独立幂等 key；具体 Target、工作区、prompt、Runtime 和凭据来源由服务端冻结配置解析。管理鉴权由调用端落实，当前产品 endpoint 尚未注册。

后续 [execution-host.md](execution-host.md) 通过可信 RuntimeFactory 将 Controller 接入独立 loopback Handler，强制准确项目来源 Guard/read/write/route，并在关闭 Store 前等待 Controller 和额度读取。该内部初始化接口已用实际 Native 和合成准入验证；真实供应商 Factory、产品 CLI/GUI 与完整工程闭环仍未完成。

执行 API 的内部 Handler 通过 StartAuthorized 传入同一 Manager 的 ManagementCurrent；入口、慢预检查之后及 Inspection 前后重查，提交前撤销拒绝新 intent。Start 保留用于受信任内部编排；不得作为绕过 HTTP middleware 的公开执行入口。UsesStore 仅供可信 bootstrap 核对相同数据库，不能由客户端指定。

Start 先读取持久启动映射，已有请求直接返回 Created=false，不再解析 Runtime、探测能力或调用准入服务。新请求核对任务当前状态、版本和 generation，locked 直接采用 exact frozen Target；auto 需要可信 SelectAuto，选择结果必须与原冻结 candidates 中一项完全相同。传入选择器/Resolver 的 Target、候选及实际 launch input 使用独立副本，不能改写冻结授权。

新启动在目标选择与 Resolver 前经过[阶段次数上限](stage-attempt-limit.md)的当前权限与 Task 条件检查；每个 task/role 最多两个持久 intent。第三次允许请求的阶段启动会原子停在 needs_review，实际 intent 插入事务重复检查；原 key 重读和已批准工作流顺序保持原合同。

Resolver 返回的 Backend/Spec 只来自服务器配置。控制器在写 intent 前拒绝 caller argv/executable/env/session/validator/channel（包含 ClaudeChannel、GrokChannel 和 CodexChannel）、非法路径/UTF-8/input/time limit 和只读角色写权限。泛用 controller 时限最多十分钟，具体 Adapter 仍独立限制范围，服务端 resolver 必须使用该路线支持的范围。当前仅接受 Probe/Start/Events/Cancel 且无任意 network/child_processes 的能力；可信 Claude/Grok/Codex channel 的局部通信由 Adapter/Supervisor 控制，不据此开放泛用网络。

Backend 可提供可信 `ValidateLaunch(ctx, role, target, spec)`；BindAdapter 自动保留实现该方法的 Adapter。新请求在 probe 和 intent 前调用它，传入独立 Target/prompt 副本。它用于拒绝 Adapter 已知的输入/路线约束，不能提供 RPC/grant、替代 Inspection/CheckPrepared/Permit 或证明实际停止。检查后仍重核管理/来源，Adapter.Start 在 intent 后重复约束检查。已有不提供此方法的 Backend 保留原行为；自定义 Factory 不得把该 callback 作为可由请求体指定的授权。已提交 key 重读继续直接读取原 receipt，不重做 preflight 或启动。

调用 Scheduler.PrepareOnce 将 intent/预留/请求映射一次提交；只有 Created=true 继续。CheckPrepared 在启动前重查实时准入。执行 lifetime 从控制器父 Context 派生，提交之后 HTTP 请求 Context 失效不会取消它；明确 Cancel、Close、应用退出和 Runtime deadline 可取消。取消必须匹配 task/run/generation，并由当前控制器持有 job；重启 unknown 和非本控制器的运行记录不自动接管。终态取消幂等，只读取终态。

Backend.Start 没有返回 Handle 时，控制器标记 interrupted/needs_review，保留 reservation 和已用预算；没有发明“未启动”等价于 stop proof 的释放方式。若过期/已被持久 fencing，则保留既有 unknown，不将其改回可执行。已知 Handle 伴随 error 仍保留，取消并等待，不丢失实际进程。Handle 尚未发布期间的取消也作用于整个 owned lifetime。

后台等待实际 Handle.Wait。仅当 Store 中相同 generation 已处于终态、Result/Proof 指向相同 run/generation/native session、声明 descendants 停止，且本 Backend.Release（真实路线绑定原 Adapter/Supervisor）核验通过，才报告 StoppedVerified/Released。Wait error、缺少/错误退出证明或 Release error 保留容量；Runtime 文本和 exit 0 都不能代替停止证明、实际测试或工程验收。Completion 不含原始异常、输出文本、凭据、私有路径或 process proof。

控制器最多同时拥有 16 个启动/等待工作，与持久全局容量、物理池、同任务和 writer 预留独立限制；本地 job/pending 记录共最多 4096，达到限额拒绝新启动，已有映射仍可读取。Close 停派单、取消 owned lifetime 并等待实际执行/退出核验工作结束。Close 超时只返回超时，不能声称已停止；后续可等待原 job，不重启或重放。

11 个控制器离线测试通过，另以冻结 Claude Code 2.1.287 验证 Controller→PrepareOnce/CheckPrepared→GLM Adapter→owned loopback/CallGate→合成上游→Native validator/实际 wait→Supervisor StopProof→release。仅一个合成 HTTP/持久 Permit，重复请求没有再次执行；HTTP 请求取消未取消 Native。真实账户、计费、模型/effort 和池的准入仍未核验，假 Inspection 不提升为真实权限。上述为初始控制器证据；后续暂停和派单继续已接入，见 [task-control-api.md](task-control-api.md)。Native session 恢复、有限返工及实际产品管理端仍需继续实施。

WP-15-PAUSE-API-01 新增 Pause/Continue 与同 issuer 管理重查，Store 全 TaskVersion CAS 后仅取消 owned job，已提交意图不随 HTTP 消失。Start 返回晚到 Handle 后仍交付已取消 lifetime 的 Cancel，RED→GREEN 验证。新增固定 Native inflight Pause 实际 wait/proof/release 通过，停止后需核对，不隐式恢复。

WP-15-CANCEL-API-01 新增 CancelTask/CancelTaskAuthorized：全 TaskVersion Store 意图事务后才取消精确 owned lifetime/Handle，不能接管 unknown 或改变 frozen Target。空闲取消不解析/启动 Runtime；不确定证明/释放仍保留 receipt/held。6 项 Controller 测试及新增固定 Native inflight TaskCancel→实际 wait/StopProof/release→cancelled 通过；现有 Pause 与 Stage Cancel 语义保持，合成准入不提升为真实账号准入。

[WP-15-GROK-01](../work-items/WP-15/GROK-01/summary.md) 补齐原始 GrokChannel 在 intent 前的拒绝，固定 Grok 1.0.48 经可信 Adapter 绑定完成 7 个真实 Native 控制器场景：成功、Read、断线、Pause、TaskCancel、Stage Cancel、Close。幂等 receipt 不重复执行，停止后核验实际 proof/release；独立模型/计费/额度证据未升级。固定 Claude 2.1.287 的 3 项控制器回归通过。产品 Worker/真实 Forwarder/准入未注册，同 key 重读不代表 Native Resume。

[RESUME-API-01](../work-items/WP-15/RESUME-API-01/summary.md) 新增 Restore/RestoreAuthorized 与明确 CheckRestore/Restore 后端。归档来源、冻结 Target、scope 与当前 Source 在 intent 前核验，Store 同事务重查成功 released origin；restore 身份参与 StartOnce hash，普通 Start 拒绝该身份。实际固定 Grok 恢复、恢复中取消和请求断线均通过同一 owned lifetime/实际 proof/release 链；产品注册/真实账号准入仍待完成。

[CHECKPOINT-API-01](../work-items/WP-15/CHECKPOINT-API-01/summary.md) 新增可信归档入口：保留启动时生产callback及释放时私有终态，只有成功owned Wait/proof/release允许归档；前后Task/管理/关闭状态重查。归档计入原work slots/WaitGroup，Close取消并等待；补齐关闭标志先到、取消回调后到的RED→GREEN竞态。实际Native取得稳定reference再恢复原UUID通过，包括批准Read，未新增真实准入声明。

[WP-15-SOURCE-GUARD-01](../work-items/WP-15/SOURCE-GUARD-01/summary.md) 已将私有SourceGuard接入Controller派单/归档、Grok/GLM Current和Supervisor启动/heartbeat/结果检查，见[合同](workspace-source-guard.md)。RequireSource=true拒绝无来源新执行；旧诊断兼容不提升来源证据。实际固定Native源漂移拒绝成功并核验wait/proof/release；产品项目登记与真实准入仍未完成。

[WP-15-CODEX-01](../work-items/WP-15/CODEX-01/summary.md) 验证 BindAdapter → Codex 只读 Adapter 的实际 11 个原生生命周期场景：成功、429/unsafe503 失败、HTTP 断线、Pause、TaskCancel、Stage Cancel、Close、来源/身份漂移和 grant 激活失败。准确 receipt 不重放，已知错误 Handle 仍实际 wait/proof/release；暂停进入 needs_review，取消不暴露成功文字。新增 Adapter preflight 关闭可提前发现的错误在 intent 后占用预留的缺口，真实 Host RED→GREEN 记录保留。真实 subscription Factory/账号/Forwarder/quota、产品 HTTP 接线、写/工具/恢复与最终工程 Gate 继续未完成。

## 受控返工继续

Config.AfterRelease 是可信进程内 callback，不属于 HTTP DTO 或公开导入配置。只有实际终态 Wait、对应 StopProof、原 Backend.Release 成功后才调用。由已批准流程和 owned 当前 review 证据派生精确 Followup；callback 不选新模型、不发管理 Token、不取消既有 budget/intent 上限。

继续工作纳入原 Controller lifetime、容量和 WaitGroup。先登记受控继续工作，再释放旧 work slot，允许容量为1时推进；Close 停派单、取消并等待 callback 与随后运行，不留下 detached worker。Host 对项目/计划/generation、来源和当前撤销状态封装复核，实际 Start 仍走原 Resolve/Scheduler/Permit。继续启动失败仅对精确 ready TaskVersion 写 needs_review 与 generation fence；不启动、不释放不确定进程、不退款。中断后的持久记录不在重启时自动执行，需明确恢复。详见[返工策略](rework-policy.md)。
