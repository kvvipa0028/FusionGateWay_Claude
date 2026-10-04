# WP-12-ADAPTER-01：Codex 受管只读 Adapter

基线 `35bceba4641da04dbf812b8dab5626097803bfcf`。本项交付 `internal/fusion/runtime/codexadapter`，合同见 [codex-managed-adapter.md](../../../contracts/codex-managed-adapter.md)。父 WP-12 继续 in_progress，最终 T01–T60 仍 not_run。

将类型化阶段 Client 接到真实 Store、Scheduler.Permit、Manager、来源证明、独占 HTTP 通道和 Supervisor。启动参数由 Adapter 固定；调用方只提供已准备 run、可信 SourceGuard、私有目录、prompt、timeout。五角色均只读/无工具，普通 Resume unsupported；真实账号/Forwarder/quota、产品 Controller/Factory、工具/写入/恢复和工程闭环继续未完成，Jev off。

## 验证结果

- Host：4 个顶层 PASS、31 个子测试 PASS、2 个显式 Native 顶层 SKIP、0 FAIL。构造器必需服务、23 类调用方/来源/准入拒绝、缺少 pin、恢复和提前输出/伪停止证明拒绝、JSON 转义凭据防护与格式化/序列化保护。
- 显式 Native：2 个顶层 PASS、19 个子测试 PASS、0 SKIP、0 FAIL；13 次实际固定 Native 进程，另 6 类启动前拒绝没有进程。13 次全部实际 wait/reap/StopProof/release，没有将 prelaunch 拒绝算成 Native 执行。
- 完整 Fusion race：510 个顶层 PASS、28 个顶层 SKIP、0 FAIL；925 个子测试 PASS、10 个子测试 SKIP。SKIP 不代表原生能力通过，实际 Native 证据单独列出。
- Go 1.26.3：CLI 构建、GUI 构建、全仓 vet 全部 exit 0。graph 更新为 13866 nodes、122606 edges。

| 原生场景 | Stage 结果 | 合成上游发送 / 持久预算 |
|---|---|---|
| 正常文字 | succeeded，观察到准确 thread/turn 与正文 fixture | 1 / 1 |
| HTTP429 | failed，无成功正文 | 1 / 1 |
| unsafe503 与后续原生重试 | failed，后续不增加发送/预算 | 1 / 1 |
| Handle.Cancel | cancelled，HTTP context 结束、实际停止释放 | 1 / 1 |
| 父 context 取消 | cancelled，HTTP context 结束、实际停止释放 | 1 / 1 |
| timeout | cancelled，HTTP context 结束、实际停止释放 | 1 / 1 |
| 来源原文件漂移 | cancelled，保护性停止，无成功正文 | 1 / 1 |
| 独立身份 epoch 漂移 | cancelled，保护性停止，无成功正文 | 1 / 1 |
| 发送前 unknown quota | failed，没有上游发送 | 0 / 0 |
| grant 激活拒绝 | cancelled，保留已知 Handle 并等待释放 | 0 / 0 |
| callback 修改调用方 prompt | succeeded，模型收到原冻结正文 | 1 / 1 |
| callback 修改调用方 effort | succeeded，冻结目标保持 | 1 / 1 |
| 同 run 重复 Start | 第二次拒绝，原运行取消并实际释放 | 1 / 1 |

启动前 6 类拒绝：Current=false、身份 generation 无效、账号漂移、workspace 漂移、credential identity 漂移、Current callback 返回 true 同时撤销 epoch；均零 launch intent、零发送、零预算。13 次进程合计 11 次合成上游发送、11 次持久调用记账，没有任何真实订阅/账号/额度请求。

## RED → GREEN 与实施判断

1. AdapterConfig/NewAdapter 缺少时编译 RED；按固定协议、现有 channel/lifecycle 与可信服务接口实现。独立包避免 runtime → codex 的 import cycle，没有改公共 Supervisor 行为。
2. 首轮实际 Native 的文字与 prompt snapshot 场景为 exit 0 / Stage failed。原因是 Adapter 错误要求 stdout 为空，而 CodexStream 有意将所有原生 stdio 记录在有界 Supervisor output。保留失败日志，改为逐行 JSON 阶段凭据排除；typed Client 原生终态、Gate Healthy、来源/身份重核与真实 wait 条件保留。不能仅凭 exit 0 或 JSON 解析成功通过。
3. 凭据投影与格式化测试先 RED，再实现输出检查；JSON 转义值和 key 中的阶段 marker 都拒绝，错误 JSON 不通过。原生 stdout 不公开，模型文字只来自严格验证的完成项。
4. Ruling：Adapter 强制 SourceGuard 存在，不采用通用 Supervisor 缺省来源的诊断语义；要求来源有效、副本目录身份和独立 Root 持续有效。代价是缺少复制来源证明的旧调用方须先补齐 provenance。
5. Ruling：prompt 原始上限 64 KiB，但 JSON 字符串编码上限 32 KiB；保留 scoped stdio 累计 64 KiB 防护。代价是部分大 prompt 在启动前被明确拒绝，不能静默放宽 transport。
6. 所有组件准入 callbacks/上游响应为合成 fixture；实际 Store/Scheduler/Manager/Native/OS 证据不能升级为真实官方账号、权益和计费证明。未导入日常 HOME、Keychain、OAuth 或 API key。

## 复现

从工作树根目录执行。helper 使用临时 HOME/XDG 和环境白名单，不继承实际认证。

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-adapter-host.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/codexadapter
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-adapter-native.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/codexadapter -run '^TestCodexAdapterPinned' -fusion-native-codex-adapter /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-adapter-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

原始失败/通过日志、最终测试统计、构建报告、Native pin 与源文件/packet hash 随本项保存。没有复制真实 key 或 Native 私有请求/鉴权 payload，提交前执行真实私有 key 排除检查。最终源码验证后仅更新文档/packet，不重复已通过且输入未变的测试；旧 packet 的 hash 仅在其所属旧提交核验。
