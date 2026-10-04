# WP-15-CODEX-HOST-01：Codex 本机 HTTP 执行与持久结果验证

基线 `ddb93d43dd7c97d821662391b8eeb9a8a185aa04`。本验证子项完成；父 WP-12/WP-15 继续 in_progress，最终 T01–T60 仍 not_run。新增测试位于 `internal/fusion/bootstrap/execution_codex_native_test.go`，合同见 [execution-host.md](../../../contracts/execution-host.md)。

## 验证范围与结果

使用真实 loopback listener、Management token、HTTP Handler、RuntimeFactory、同一 Store/Scheduler/Manager、Controller、Codex Adapter/CallGate 与固定 Codex 0.160.0 进程。HTTP 完成 preview、提交 task、start、相同 key 重读、取消及 run GET。五个实际 Native 场景全部通过：

| 场景 | 持久结果 | 合成调用 / 预算 |
|---|---|---|
| success | succeeded，准确终态正文 | 1 / 1 |
| cancel | cancelled，无成功正文 | 1 / 1 |
| source-revocation | cancelled，登记配置撤销，后续读取拒绝 | 1 / 1 |
| close | cancelled，宿主等待实际停止 | 1 / 1 |
| parent-cancel | cancelled，生命周期取消 owned 请求及进程 | 1 / 1 |

每次都验证真实 Wait/StopProof、reservation 释放，Factory cleanup 先于 Store 关闭；宿主关闭后重新打开 Store，检查终态、LaunchConfirmed、NativeSessionID、Owner 清空、预算和 reservation。原项目文件保持未修改。相同 key 不再次 Resolve/启动/调用；success/cancel 还通过真实 HTTP 重读终态收据，并检查 Adapter Observation 的成功正文或取消空正文。

未经 Management 鉴权的 HTTP start 返回401，Resolve/模型调用为零。编码后过大的 prompt（原始6000字节，经 JSON 编码超过32KiB）与5分钟 timeout 返回503/runtime_unsupported：task 保持 ready/generation0，无幂等 receipt、调用或预算。两类拒绝没有启动 Native，不能计为原生进程。

真实锁定 target 的 model、credential identity、effort、runtime version、subscription billing 在发送处核对。收据和拒绝错误不返回私有目录；Native 只使用隔离 HOME/XDG、阶段凭据和白名单环境，没有读取日常账号认证。

## 最终检查

- 显式 Codex/Grok HTTP Native 回归：2 个顶层 PASS、12 个子场景 PASS，0 FAIL/SKIP。Codex 5 次和 Grok 5 次实际进程，另有2类 preintent 拒绝。共15次合成发送和持久记账（Codex5、Grok10）。
- bootstrap Host race：18 PASS、2 SKIP、0 FAIL；75 子测试 PASS。显式 Native 测试在默认运行中 SKIP，以上单独验证。
- 完整 Fusion race：514 PASS、30 SKIP、0 FAIL；945 子测试 PASS、10 子测试 SKIP。
- bootstrap tagged vet exit0；Go1.26.3；graph刷新到13886 nodes/122956 edges。
- 本项只有测试与文档变更，现有生产接线直接通过，没有修改运行行为、API、Store schema 或沙箱。因此没有人为制造行为 RED，也没有重复重建未变的产品；上一项 CLI/GUI 构建证据保留在原所属提交。

## 复现

从工作树根目录执行，测试 helper 使用临时 HOME/XDG 和白名单环境：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-host-native.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/bootstrap -run '^TestExecutionHostPinned(CodexHTTP|NativeHTTP)$' -fusion-host-native-codex /Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex -fusion-host-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-host-bootstrap.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-host-full.log test -race -v -count=1 -timeout=300s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-codex-host-vet.log vet -mod=readonly -tags fusion,nogui ./internal/fusion/bootstrap
```

## 证据边界与后续

测试 Factory 的 Inspect、identity、Forwarder、额度及池均为合成 fixture；只有宿主/HTTP/Adapter/Native/停止和持久存储是真实执行。它不是可供真实账号使用的生产 Factory，不能开放真实供应商准入。草稿 CLI 继续 execution off，真实 OpenAI 登录/续期/身份/订阅 Forwarder/quota、三路线生产注册、工具/写入/checkpoint/恢复、产品 UI、五阶段工程闭环和最终 Gate 仍待实施；Jev off。

原始最终日志、统计、可执行文件固定 hash 和本项源文件/packet hash 留在此目录；hash 从所属提交回读，不能要求后续修改后的树与历史 hash 相同。没有导入真实 key，没有将测试准入升级为生产证明。
