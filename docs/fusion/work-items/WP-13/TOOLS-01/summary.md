# WP-13-TOOLS-01：实际 readonly 工具边界

基础 HEAD：`be0acb559c10099954fbfb1621ba53644463553f`。本项完成固定 Native 工具协议调查、真实 fixture 与拒绝边界回归，没有启用生产工具。WP-13 继续 in_progress；生产 CallGate/Session/Store/API 完全未改动。未更改历史 CALLS-01 hash，按各自拥有的提交树验证。

在全新私有 HOME/XDG/GROK_HOME、自己创建的项目/文件、loopback 假上游和固定 `1.0.48/b94d5072c95f` 中运行 Native。合成回复发出 `read_file(target_file)`，随后一次文本回复；工具真实执行，没有使用真实账号/Keychain/key或公司代码。server 只记录 tool messages、assistant tool_calls 与 metadata，没有完整保存 vendor system prompt 或 Authorization。随机临时根替换为 `<fixture-root>`/`<denied-root>`，其他输出保持实际观察。

| 原生调查 | 结果 | 关键证据 |
| --- | --- | --- |
| [read](read/characterization.json) | Native exit0，项目内文件读取成功 | tool_call → location update → completed；marker 出现在后续 tool message |
| [outside](outside/characterization.json) | 工具 failed/Permission denied，但 Native 仍 exit0/end_turn | OS profile 不许读取另一个自己创建的临时根；此失败不能由 end_turn 变成验收成功 |
| [private-config](private-config/characterization.json) | 工具读取自己的 GROK_HOME config；合成 key 出现在 stdout/tool message | profile 允许整个私有 HOME；Read/dontAsk 并不保护运行时自身配置。预期 marker 不外传的断言失败，见 [原始 RED](wp13-tools-private-config.log)，该失败未修复或改为通过 |

三次各有 title+main+tool-continuation 三次 HTTP，均 fixture-model；Native end 的 modelCalls=2/num_turns=2 不包含 title。CLI 在自己创建的 config 中加入 marketplace metadata，不由此宣称安装/更新已经验证。诊断 profile 仍允许较宽 Mach lookup，禁止 fork/securityd、只许假上游端口；此局部 OS 拒绝与配置泄漏调查不是生产 Worker isolation/admission 证明。

工具 stdout 的实际字段包括 `toolCallId/title/kind/status/toolName/rawInput/content/locations`；更新中 `rawOutput` 区分 `FileContent` 与 `PermissionDenied`。后续 HTTP 追加 assistant `tool_calls` 和 tool `tool_call_id/content`。仅验证 这些精确 fixtures，不推出其它工具、多工具、取消、Resume 或 SDK 版本兼容性。

现有文本 Gate 会在向 Native 输出之前拒绝工具响应，Session 也拒绝工具事件/继续消息。新增两项 fixture 回归确保这三种 Native exit0 不能被提升为受控成功，tool continuation 在 Permit 前就被拒绝。它们是既有保护的回归，没有以当前行为为依据更改保护或测试预期。

新增 [实际 Native Gate attack](wp13-tools-gated-native.log) 则把读取 private config 的同样工具指令经过现有 CallGate：title完成，第二次 HTTP 扣账后响应因工具 delta 被拒绝，Native exit1，未出现 tool_call/update，没有 stage grant 回显；Store usedCalls=2，Gate uncertain。它证明当前拒绝边界会在工具开始之前生效，不能证明未来启用工具后的 path 授权或凭据隔离。

[Targeted race](wp13-tools-green.log)：24 PASS/2 SKIP/0 FAIL；两个 SKIP 是需显式 pinned Native flag 的诊断，其中新增攻击场景在本项单独显式执行并通过。CALLS-01 原有五 Native 场景不在本项重跑。生产源码、依赖、schema、公共接口均未变化，本项不重复全 Fusion/build/vet；前项的 387 PASS/11 SKIP 和 CLI/GUI/vet0 是其拥有提交的证据，不能记为本项新执行。

复现要求同 [CALLS-01](../CALLS-01/summary.md)：macOS/arm64、固定 binary/hash、Go1.26.3、SDK、公开 module cache；Python3/sandbox-exec。本脚本会创建/删除自己的两个临时目录与进程组，只访问自己生成的 fixture。诊断读取到的 key 为显式 `FUSION_SYNTHETIC_PRIVATE_CONFIG_MARKER`，并非真实凭据。

```sh
set -e
packet=docs/fusion/work-items/WP-13/TOOLS-01
mkdir -p .fusion-dev/implementation
python3 "$packet/characterize-tools.py" --mode read --output .fusion-dev/implementation/tools-read
python3 "$packet/characterize-tools.py" --mode outside --output .fusion-dev/implementation/tools-outside
```

以下原生 profile 的负面诊断预期退出1并保留 AssertionError，代表已复现未解决的原生读取边界；不要将其当成生产准入：

```sh
python3 docs/fusion/work-items/WP-13/TOOLS-01/characterize-tools.py \
  --mode private-config --output .fusion-dev/implementation/tools-private-config
```

回归与 Gate 实际拦截（`run-go.py` 为前项已记录的私有环境 helper）：

```sh
set -e
helper=docs/fusion/work-items/WP-13/CALLS-01/run-go.py
python3 "$helper" .fusion-dev/implementation/tools-regression.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/grok
python3 "$helper" .fusion-dev/implementation/tools-gated-native.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/runtime/grok -run '^TestGrokCallGatePinnedNativeRejectsPrivateConfigTool$' \
  -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
```

事实与 checks/未验证 flags 见 [test-results.json](test-results.json)，源码/fixtures/原始 packet SHA 见 [artifacts.json](artifacts.json)。JSON/NDJSON/Python/link/gofmt/diff 检查；raw log whitespace 保留，diff 排除 `*.log`。真实模型/额度调用0、Jev off，没有提升任何真实准入。

后续启用工具前必须在上游工具响应进入 Native 之前验证 canonical project-path、symlink/越界/身份漂移、tool ID/参数/返回关联以及 Native stdout/后续 HTTP 的 credential 边界；执行后才读到的 tool event 不能代替前置授权。Grok 生产 transport、受管启动/取消停止、真实账号/计费/额度池、准确恢复和最终验收仍未完成。
