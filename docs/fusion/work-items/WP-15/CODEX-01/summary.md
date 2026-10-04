# WP-15-CODEX-01：Codex Controller 生命周期与启动前校验

基线 `4e98ed6d813791fe5dc8c04903f003f031168ce0`。本子项完成；父 WP-12/WP-15 继续 in_progress，60 类最终 Gate 仍 not_run。合同见 [execution-controller.md](../../../contracts/execution-controller.md) 与 [codex-managed-adapter.md](../../../contracts/codex-managed-adapter.md)。

## 改动与缺口修复

BindAdapter 自动保留 Adapter 提供的 ValidateLaunch。Controller 新请求在 probe/intent 前传入冻结 prompt/Target 副本；同 key 重读不重复检查或执行。新 callback 仅是可信服务器接线，不由请求 DTO、项目声明或模型输出授予权限；Inspection/CheckPrepared/Permit、来源与管理重核、原 Adapter 启动后的复核和真实停止证明均保留。

Codex ValidateLaunch 使用共享静态 gateway Target 合同，并核对角色、registry identity、SourceGuard、私有目录、readonly、prompt 和 timeout。身份 callback 之后仍检查 context、身份与来源；不创建 grant、不发 RPC、不花预算。Start 对冻结输入重复校验。既有未实现 ValidateLaunch 的 Backend 保留兼容行为，实际 Grok/Claude 回归通过。

测试先得到编译 RED（Backend 无此接口），随后真实行为 RED：五类 Adapter 特有错误在 Controller 中仍 Created=true/ErrLaunch，task 已被 intent 改变；validator 调用次数为零。实现前置校验后全部 GREEN。测试通过 pin/platform probe fixture 隔离 Adapter 约束，不把缺少可执行文件造成的提前失败当作此修复证据。通用 readonly write 拒绝也单独覆盖。错误 callback、撤销管理、改变来源及 request context 失效都保持 ready/generation0，无 receipt、无 Backend.Start；原始错误不公开。

## 实际原生场景

使用真实 Management middleware → StartAuthorized → Controller/Scheduler/Store → BindAdapter → Codex Adapter/CallGate → 固定 Native → 实际 wait/StopProof → release。上游、身份与额度均为合成 fixture，没有真实登录/订阅调用。测试中的 middleware 入口是内部 httptest 管理入口，尚未宣称产品 loopback HTTP 全链路已验证。

| 场景 | Stage 结果 | 合成发送 / 持久预算 |
|---|---|---|
| 文字成功 | succeeded，准确 thread/turn 与 fixture 正文 | 1 / 1 |
| HTTP429 | failed，无成功正文 | 1 / 1 |
| unsafe503 与原生重试 | failed，不增加上游发送 | 1 / 1 |
| HTTP 请求断线 | owned Native 继续，最终 succeeded | 1 / 1 |
| Pause | cancelled，task 收尾 needs_review，禁止盲 Continue | 1 / 1 |
| 整项 CancelTask | cancelled，task 收尾 cancelled | 1 / 1 |
| 阶段 Cancel | cancelled，准确 owned run 停止释放 | 1 / 1 |
| Controller.Close | cancelled，等待实际停止释放 | 1 / 1 |
| 原来源漂移 | cancelled，保护性停止，无成功正文 | 1 / 1 |
| 独立 identity epoch 漂移 | cancelled，保护性停止 | 1 / 1 |
| grant 激活拒绝 | 错误返回仍保留 Handle，实际 cancelled/停止释放 | 0 / 0 |

11 次 Native 全部实际 wait/StopProof/release，共 10 次合成发送、10 次持久调用记账。运行中/终态相同 key 返回原准确 run，不重新 Resolve、启动或发送。Completion 无私有路径、凭据或模型文本；运行中 Observation 拒绝，失败/取消无成功文本；复制项目文件未改，原文件只在明确注入的 source_drift 中改变。

## 最终验证

- control/codex/codexadapter 三包 Host race：98 顶层 PASS、10 顶层 SKIP、0 FAIL；276 子测试 PASS。新增 4 个 Host 顶层测试、20 个子场景：六类 Controller 拒绝、冻结副本、四类 callback/撤销、十类 Adapter preflight。
- 显式 Controller Native：8 顶层 PASS、28 子测试 PASS、0 SKIP/FAIL；Codex 11 次、Grok 20 次、Claude 5 次，合计 36 次原生进程。31 个场景中 Grok 的五个恢复场景各有两次进程，不能只按测试行数当作进程数。
- 显式 Codex Adapter 回归：2 顶层 PASS、19 子测试 PASS、0 SKIP/FAIL；13 次原生进程及 6 类启动前拒绝。合计本项最终原生回归 49 次实际进程，不包含这些前置拒绝。
- 完整 Fusion race：514 顶层 PASS、29 顶层 SKIP、0 FAIL；945 子测试 PASS、10 子测试 SKIP。SKIP 不作实际 Native 通过。
- Go 1.26.3 CLI/GUI 构建、全仓 tagged vet 全部 exit 0；graph 13881 nodes/122855 edges。

## 复现与剩余范围

从工作树根目录执行；helper 使用临时 HOME/XDG/白名单环境，不继承实际认证。

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-controller-host.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/control ./internal/fusion/runtime/codex ./internal/fusion/runtime/codexadapter
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-controller-native.log test -race -v -count=1 -timeout=240s -mod=readonly -tags fusion,nogui ./internal/fusion/control -run '^TestControllerPinned(Codex|Grok|Native)' -fusion-control-native-codex /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 -fusion-control-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-controller-adapter.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/codexadapter -run '^TestCodexAdapterPinned' -fusion-native-codex-adapter /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-controller-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

真实 OpenAI 私有 identity/登录/续期、subscription Forwarder/计费/quota、Codex 产品 Factory/HTTP、写/工具/checkpoint/恢复、UI 与五阶段工程闭环及最终 T01–T60 继续待完成。没有借用日常 HOME/Keychain，没有 fakeJWT/chatgptAuthTokens/API billing fallback，Jev off；此项实际过程与合成准入的边界不升级。

最终源码验证后只更新文档/packet；原始 RED/GREEN、最终统计、构建报告和源文件/packet hash 保留并在所属 commit 回读。私有真实 GLM key 排除检查不输出 key 或其 hash；主项目原有两个 .DS_Store 保留。
