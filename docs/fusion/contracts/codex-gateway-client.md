# Codex 类型化阶段协议客户端

`NewGateway` 将受管 Codex0.160.0 的独占阶段 provider接到原有类型化Client/RPC，不再用test-only raw Peer作为阶段协议实现。它是可信内部协议组件；正式Adapter/Factory、官方登录/订阅Forwarder、额度与工具/恢复准入仍待完成。证据见 [GATEWAY-CLIENT-01](../work-items/WP-12/GATEWAY-CLIENT-01/summary.md)。

## 身份与请求

构造器冻结 Scope、上游Identity、target、Cwd、CodexHome及已解析effort副本，要求subscription/controlled_calls、有效route revision、相同requested/resolved model、固定CLI、非插件；callbacks只能来自可信管理层。仅本地provider `fusion_codex_stage`，无任意provider/URL/argv/原始RPC注入接口。

Native account/read必须account=null、requiresOpenaiAuth=false，workspaceRouting缺省或null；其它字段、非空账号、需OpenAI认证或非null路由都拒绝。这个检查只证明本地阶段传输的预期认证形态。稳定账号/workspace/credential identity/generation与准入仍来自独立registry；generationAdmitted缺省/false时零thread/turn发送。默认`New`继续要求内置openai/真实Native ChatGPT认证形态，没有借空账号打开原客户端。

固定源码的[GetAccountResponse](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/app-server-protocol/src/protocol/v2/account.rs)将workspaceRouting标为experimental Option；非实验schema省略此属性，但实际Native序列化null。只接受该空形态，不采用其中的backend_origin或account_routing_override。

initialize验证version/private CodexHome/platform并发initialized；thread/start固定本地provider/model/effort/cwd/never/read-only/summary=none/ephemeral。返回的CLI/model/provider/effort/cwd/审批/无网络sandbox全部核对。Gateway五角色均保持readonly/tool-free；不把implementation/testing角色本身当作写入许可。默认Client已有的分角色write字段不变。

线程启动前置state=initialized；发送前置execution_uncertain，丢失确认或字段漂移后不能盲目重放。turn/start同样固定目标、只允许ready状态；回复后再次核对generation/current，并拒绝取消后迟到的RPC确认。单个Gateway客户端目前只执行一个thread/turn。Resume在RPC前拒绝，必须等checkpoint、SourceGuard、持久thread/账号身份与恢复证明完成。

## 事件与终态

沿用有界StdioPeer及Supervisor transport。Gateway通知独立严格投影，拒绝重复/case aliases、invalidUTF-8、null包、trailing/depth/size、未知方法/参数；可选emittedAtMs必须signed int64。所有运行通知核对Scope、当前Identity、thread与turn，不凭文本或exit0认定成功。

允许固定Native实测的启动/文字/错误观测：remoteControl/status/changed（只接受disabled，serverName允许空字符串）、thread/started、warning、thread/status/changed、turn/started、item/started/completed、item/agentMessage/delta、thread/tokenUsage/updated、account/rateLimits/updated、error、turn/completed。remoteControl元数据只在本私有Peer中核对，不是新增网络授权；warning、quota观测、idle、delta不会改变终态，也不提升真实额度/账号准入。

只接受userMessage、agentMessage、无summary的reasoning；agent phase仅commentary/final_answer或null，memoryCitation/delivery/questions只允许null。工具、文件写、plan、子Agent、compaction、reroute、审批等待或用户输入等待、远程连接都拒绝并将当前turn标为execution_uncertain。

turn/started只接受同一ID、inProgress和空初始items；最多128个观测输出项，开始/完成成对，同ID同类型，delta最多1MiB且必须与完成正文一致。终态提供的items必须在已完成观测中，不允许未观察项或未完成项变成功。非重试error不能被后续completed抹去；failed需要原生error，interrupted保留原生语义。终态重复/覆盖拒绝。

turn/interrupt固定当前thread/turn，进入cancelling，最终按Native终态取得interrupted。它不能代替Controller的持久cancel intent或OS停止：实际测试RPC中断后的Native为interrupted，但未提交Controllercancel intent的组件Stage为failed，绝不宣称产品cancelled闭环已经完成。Supervisor Cancel场景则真实cancelled/wait/StopProof/release。

## 验证与边界

固定Native实际通过typed Client→StdioPeer→Supervisor→私有stage grant→Responses Gate→合成upstream→原生终态。五个新增场景：文字成功、429终止、unsafe503后的Native重试拒绝、Supervisor inflight取消、原生turn/interrupt。逐次持久预算与实际发送均核对；前项HTTP预算与metadata启动场景同时回归。

本项未把合成Current/admitted callback升级为真实身份/权限/权益证明；Native诊断Permit使用真实Store.ReserveCall，生产Adapter必须接到真实Scheduler.Permit/SourceGuard/registry与单次Forwarder。quota通知不是正式额度pool证据，Gate/SSE completed不是阶段产物验收；64KiBstdio限额仍为当前有界组件约束。没有真实OAuth/APIkey/Keychain导入或真实订阅模型请求，Jev off。

后续 [受管只读 Adapter](codex-managed-adapter.md) 已接通 Scheduler.Permit、SourceGuard、可信 registry callbacks 和单次 Forwarder 接口，并完成实际 Native 合成生命周期核验；真实 registry/Forwarder/quota 与产品注册仍须独立完成，不能升级为真实订阅准入。
