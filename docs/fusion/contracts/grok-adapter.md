# Grok 只读 Adapter 合同

对应 [WP-13-ADAPTER-01](../work-items/WP-13/ADAPTER-01/summary.md)。实现公共 managed.Adapter 的 Probe/Start 与本控制器 VerifyStop/Release；Resume 明确 unsupported。只读 Native 文本/单个预授权 read_file 可执行，写范围、其它工具、Native effort 与恢复仍不支持。生产注册、真实 X subscription NativeForwarder/账号/池/计费/Quota/Source稳定性准入尚未完成，Jev off。

成功执行在本 Adapter 持有实际 Handle、完成 wait/StopProof/Release 后，可通过 [Checkpoint](grok-checkpoint.md) 生成私有完整会话 seal，并冻结原批准 Read。该生产端支持重开核验；新执行恢复消费者与历史 Read 导入尚未实现，不能据此登记 Resume capability。

可信 AdapterConfig 提供同一个 Scheduler/Store/Inspector、Manager、固定 executable、Current(GateBinding) 和 [NativeForwarder](grok-call-gate.md)。Forwarder 是独立准入的服务合同，必须单次发送、遵守context并核验冻结身份/端点/计费；本包没有普通 xAI API key/API gateway/web cookie fallback。Current 必须有界、不产生外部副作用；其参数深拷贝，不允许修改冻结绑定。Config不来自HTTP。

Start输入仅private Root/Workspace、UTF-8 prompt与timeout。拒绝caller executable/hash/argv/env/session/validator/两种channel、NUL或非法UTF-8/空prompt/超过64KiB、timeout超过4分钟。Writable以及持有write reservation拒绝，不因implementation/testing角色自动开放写权限。Root/Workspace canonical0700、独立、仓库外、互不包含；权限/Source/数据外发准入由Inspector与Current independently验证。

在调用服务前复制prompt字节，防止caller切片或服务callback改变任务内容。CheckPrepared重核starting/run owner/task/role/attempt/revision/generation/frozen target、quota/reservation/预算，随后从Store重新读取权威run，不留存caller的mutable target。Probe核对实际文件SHA256等于固定1.0.48；Effort只接受显式none/value=nil，其它返回unsupported，不能忽略所选effort。

Adapter生成全新v4 Native UUID；绑定run/gen/role/Cwd/version/hash/model/read_file与MaxTurns，MaxTurns=min(剩余预算,1000)，每HTTP真实Scheduler.Permit仍为最终预算权威，Native turn/end标签不能代替记账。每个Current检查实际Store身份、冻结target与完整Binding，再调用拷贝给外部Current；构造ReadTools时允许starting，真实调用必须通过Manager的running ModelAudience。

建立独立 ReadTools、NewReadSession、prepared model grant、CallGate、[GrokChannel](grok-managed-channel.md)。再次CheckPrepared/Current后用O_EXCL/0600/fsync私有`Root/grok-prompt.txt`，固定`--prompt-file`，不将prompt装入argv，也不接受自定义prompt文件。Native参数固定内部model alias fusion、streaming-json/dontAsk/no-subagents、Read与禁用search_tool/use_tool/web search/auto update、精确Cwd/session与turn上限。prompt IO后启动前再次复核prepared/current，然后交给同一Supervisor；只在ConfirmStarted后激活。

Supervisor通过真实exit0与EOF才调用validator。逐行有界协议Session验证+stage grant raw/decoded JSON反射拒绝，思考/工具/raw日志不作为产物，累计text事件最多64KiB。成功还要求Session.Finish成功与Gate.Healthy（完整HTTP/ReadScope），不是仅凭CLI exit0。Native错误、拒绝、预算耗尽、取消均不会返回成功文本。失败启动清理channel/grant/read descriptor/lifetime；已知Handle加activation错误必须保留到wait/reap；正常后台wait后撤销并清理。Source Host TOCTOU与不可撤销外部请求的边界保持前项合同，不因Adapter生成参数而解决。

Observation只在Store同generation终态且属于本Adapter记录时可读；其它阶段拒绝。返回已解析Outcome与text，仅是未独立验证的Native输出，可能含项目源码，外层必须先做task/project权限校验。失败只返回实际终态，text空；StrictLock/AllCalls/Billing/Quota/Upstream/Stopped等flags不通过Native标签自动提升。停止依据是单独由本Supervisor签发/核验的StopProof，Release必须通过同Adapter的Scheduler。每实例最多4096记录，到限拒绝新任务，不自动接管旧进程或重放。

实际固定Native验证：Adapter文本/read、429 retry、private config/outside/write instruction拒绝、预算耗尽、inflight cancel共8项，以及caller修改prompt的snapshot、40020byte prompt-file和启动前Current drift共3项；其中drift没有启动CLI或花预算。真实Store/Manager/Scheduler/Supervisor与生产profile，quota/route/inspection/上游全为合成。准确新UUID与StopProof一致、失败无成功text、foreign generation拒绝、可信Release及文件未改均验证。对外forwarder与产品路线不能拿fixture true flags替代真实准入。
