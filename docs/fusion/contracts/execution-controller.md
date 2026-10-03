# 可信执行控制器合同

`internal/fusion/control.Controller` 属于服务端内部编排，不是 HTTP DTO。Config 必须提供同一 Store 的 Scheduler 和可信 Resolve。请求只携带 task/role/plan revision/expected generation 与独立幂等 key；具体 Target、工作区、prompt、Runtime 和凭据来源由服务端冻结配置解析。管理鉴权由调用端落实，当前产品 endpoint 尚未注册。

Start 先读取持久启动映射，已有请求直接返回 Created=false，不再解析 Runtime、探测能力或调用准入服务。新请求核对任务当前状态、版本和 generation，locked 直接采用 exact frozen Target；auto 需要可信 SelectAuto，选择结果必须与原冻结 candidates 中一项完全相同。传入选择器/Resolver 的 Target、候选及实际 launch input 使用独立副本，不能改写冻结授权。

Resolver 返回的 Backend/Spec 只来自服务器配置。控制器在写 intent 前拒绝 caller argv/executable/env/session/validator/channel、非法路径/UTF-8/input/time limit 和只读角色写权限。泛用 controller 时限最多十分钟，GLM Adapter 仍独立限制四分钟，服务端 resolver 必须使用该路线支持的范围。当前仅接受 Probe/Start/Events/Cancel 且无任意 network/child_processes 的能力；可信 ClaudeChannel 的局部通信由 Adapter/Supervisor 控制，不据此开放泛用网络。

调用 Scheduler.PrepareOnce 将 intent/预留/请求映射一次提交；只有 Created=true 继续。CheckPrepared 在启动前重查实时准入。执行 lifetime 从控制器父 Context 派生，提交之后 HTTP 请求 Context 失效不会取消它；明确 Cancel、Close、应用退出和 Runtime deadline 可取消。取消必须匹配 task/run/generation，并由当前控制器持有 job；重启 unknown 和非本控制器的运行记录不自动接管。终态取消幂等，只读取终态。

Backend.Start 没有返回 Handle 时，控制器标记 interrupted/needs_review，保留 reservation 和已用预算；没有发明“未启动”等价于 stop proof 的释放方式。若过期/已被持久 fencing，则保留既有 unknown，不将其改回可执行。已知 Handle 伴随 error 仍保留，取消并等待，不丢失实际进程。Handle 尚未发布期间的取消也作用于整个 owned lifetime。

后台等待实际 Handle.Wait。仅当 Store 中相同 generation 已处于终态、Result/Proof 指向相同 run/generation/native session、声明 descendants 停止，且本 Backend.Release（真实路线绑定原 Adapter/Supervisor）核验通过，才报告 StoppedVerified/Released。Wait error、缺少/错误退出证明或 Release error 保留容量；Runtime 文本和 exit 0 都不能代替停止证明、实际测试或工程验收。Completion 不含原始异常、输出文本、凭据、私有路径或 process proof。

控制器最多同时拥有 16 个启动/等待工作，与持久全局容量、物理池、同任务和 writer 预留独立限制；本地 job/pending 记录共最多 4096，达到限额拒绝新启动，已有映射仍可读取。Close 停派单、取消 owned lifetime 并等待实际执行/退出核验工作结束。Close 超时只返回超时，不能声称已停止；后续可等待原 job，不重启或重放。

11 个控制器离线测试通过，另以冻结 Claude Code 2.1.287 验证 Controller→PrepareOnce/CheckPrepared→GLM Adapter→owned loopback/CallGate→合成上游→Native validator/实际 wait→Supervisor StopProof→release。仅一个合成 HTTP/持久 Permit，重复请求没有再次执行；HTTP 请求取消未取消 Native。真实账户、计费、模型/effort 和池的准入仍未核验，假 Inspection 不提升为真实权限。暂停/恢复/有限返工及实际管理端接线由后续工作完成。
