# WP-14-CALLS-01：Claude Code Messages 调用出口

本子工作项 `done`，父 WP-14 保持 `in_progress`。新增可信服务端构造的 `glm.CallGate` HTTP Handler，并复用 Manager 的阶段身份与共享单 run 调用互斥；尚未注册生产 listener、Native Adapter 或实际 GLM Transport。

## 调用合同

CallGate 只接受当前 model audience 的 Bearer capability 与固定 Messages 路径；客户端 key、账号、权限 header 和 cookie 不传给上游。请求不能指定另一个模型、路线、上游地址或未批准工具，未知/重复/大小写别名字段、query credential、错误 Origin、尾随 JSON、无效 UTF-8 和超限输入被拒绝，拒绝发生在预算与 Transport 之前。

每个实际 HTTP 请求单独调用 mandatory Permit，包括 Native SDK 的重试；生产接线必须使用当前路线/额度/预留核验与持久共享预算。Permit 后和响应交付前再次核验同一 issuer、当前 capability 和冻结 Binding。传给 gate callbacks 的目标为独立复制值。不同 CallGate 实例与原有 Dispatcher 共用 Manager.BeginModelCall，防止同一个 run 的模型调用并行。ModelCurrent 不接受来自其他 issuer 的 Context。

一次请求最多调用 Transport 一次，没有内部重试、redirect、模型/账号 fallback 或失败退款。上游 URL 固定 CN Coding Plan Anthropic endpoint，API key 只在控制器 Transport 的新请求中设置；Native 诊断仅获得合成阶段 grant。Transport 是受信任依赖，生产版本必须单独证明没有隐含外发/重试，不能把这个接口本身当作全部调用证据。

请求最大 1MiB；响应最多 8MiB、SSE 单行最大 1MiB，每次调用 Context 最长一分钟。完整响应先缓冲，核对 canonical message model、单次 start/stop、事件类型和当前授权后才交付。模型漂移、缺失终止、重复消息、超限、redirect 和上游原始错误不交付给 Native，错误响应只有静态状态文本。缓冲带来首字节延迟；后续生产 listener 还必须落实 request/read/write deadline、生命周期取消和容量限制。这里未声称完成产品实时流 UI。

## effort 与 Native 重试

本机固定 2.1.287 的实际请求默认有 `thinking.type=adaptive` 和 `output_config.effort=high`。CallGate 对 effort 按 FrozenEffort 精确匹配；`none` 必须 value=null，并拒绝上述 Native 默认值。请求字节匹配只证明宿主参数没有静默漂移，不能证明 GLM 接受或执行了该推理档位；真实 EffortVerified 准入仍未完成。

固定 Native 在 429 后发出 `system/api_retry`：attempt=1、max_retries=10、retry_delay_ms 为本次延时、error_status=429。旧解析器因未知 subtype 拒绝（真实 Native RED）；新增状态与数值边界后通过。该事件仅为信息，不授予调用、不递增预算，也不替代 HTTP 计数。未初始化、已取消、消息正在交付、缺字段、越界 attempt/max/delay 或不受支持状态均拒绝。

## 验证

新增模块缺失、SSE model 大小写别名、未批准 tool_choice 和 Native api_retry 分别形成 RED；实现后 GREEN。8 个 CallGate top-level tests 加 1 个 retry protocol test 覆盖固定目标/凭据边界、输入拒绝零 outbound、Permit 后撤销/身份变化、传输错误计数、响应漂移/错误/超限、缺依赖、多个实例互斥、其他 issuer、深拷贝与 none effort 拒绝。

真实 loopback HTTP + Store 预留/共享预算测试：三次请求返回 200/200/403，持久 UsedCalls=2，Transport 调用=2；第三次没有外发。此处 Permit 使用 Store.ReserveCall 的合成服务端组合，不作为真实额度或路线检查证明。

显式固定 Native 测试有两个实际 CLI 场景：成功路径 1 次 HTTP/Permit；第一次假上游 429 后成功路径 2 次 HTTP/Permit。两个场景均通过全部 Native 协议与 EOF/exit 核验，ObservedMessages 仍为 1；证明只计 Native message_start 会漏掉 SDK 重试。没有真实模型调用或用户凭据读取。

Native 测试手工构造 nofork + 唯一 loopback 端口 profile 和 synthetic env，尚未通过生产 Supervisor：没有 Native StopProof、真实路线准入、计费或额度证明。API key helper/真实凭据配置均未改写。复现（仓库根目录、Go 1.26.3、将路径替换为固定 hash 的已安装文件）：

```sh
go test -race -v -count=1 -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/glm \
  -run '^TestCallGatePinnedNativeLoopbackDiagnostic$' \
  -args -fusion-native-claude /absolute/path/to/claude/versions/2.1.287
```

完整 Fusion tagged race：199 个 top-level PASS、3 个 SKIP（两个父进程 helper、一个显式 Native 诊断入口；后者另行实际执行通过）。CLI/GUI 编译和全仓 Fusion/nogui vet exit 0。生产受管启动、启动后 model grant 交付、loopback OS 准入、实际 key reader/Transport、取消/恢复、GLM 套餐/额度/推理核验仍待完成。WP-14/WP-17/Gate A 和最终 T 场景保持未完成。
