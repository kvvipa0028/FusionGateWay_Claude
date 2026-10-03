# WP-13-RESTORE-01：准确归档恢复准备与历史 Read

基线 `e9aeb1fcc625e3f191fceb26694e22a989e931ba`。完成包内可信恢复准备和历史 Read 导入；完整 WP-13/Native Resume 仍 in_progress，没有开放 HTTP、修改 Store schema 或登记生产恢复能力。合同见 [grok-restore.md](../../../contracts/grok-restore.md)。

prepareRestore 先冻结 caller 输入，再通过真实 Scheduler/Store 核对新 prepared run、quota/permission/reservation/预算，以及原 seal 与成功 released run。新旧 task/project/role/plan/Target/cwd 必须相同，新 run ID/attempt/generation 独立；只读新 reservation 不允许 WriteKey。原 Native 目录不作为恢复授权来源，消费者取得密封完整 14 文件的私有副本。

原批准 Read 导入新的 per-run ReadTools。每个 main HTTP 必须保留所有旧 assistant/tool 配对并吻合原 ID/name/path/output/model（如声明），每次重新检查 Source；旧 event 重放、call ID 复用、孤立/重复/伪造配对、缺历史及 Source 漂移拒绝。历史项不占新 tool turn，旧与新合计 64 项/512KiB 边界保持。每 HTTP 继续用新 ModelAudience 和真实 Permit，旧预算不退款，不重用旧 grant。

验证结果：

- 新增 Host：8 PASS、39 个通过子场景、0 FAIL；覆盖 Scope/Target/cwd/Source/seal/当前准入漂移、历史配对完整性、重放拒绝、新 tool turn、合法 seal 中 JSON 转义的新 grant 反射、caller callback 变更和原 Native 目录删除。
- 真实 Store/Scheduler/Gate 记账场景：旧 run 用 1 次，归档/恢复准备 0 次，新 run 两次重复历史 HTTP 均独立 Permit，task 总计 3 次。丢失历史的请求在 forward 前拒绝，没有新增花费；这是合成上游 Host 证据。
- 固定 Native 现有 Adapter/协议/归档回归：7 PASS、30 个通过子场景；一个无子场景叶用例合计 31 场景，其中 30 启动 Native、1 启动前 drift。使用实际生产 profile/wait/StopProof/Release，但没有启动 Resume，不能作为新恢复消费者的 Native 证据。
- 全量 Fusion tagged race：426 PASS、18 SKIP、0 FAIL。没有 Native flag 的 SKIP 不算实际执行。
- Go 1.26.3 CLI/GUI 编译及 tagged vet 均 exit 0。最终检查后仅修改文档和证据包，源码 graph 已刷新。

先观察缺 prepareRestore 的编译 RED，实施后 Scope 转 GREEN；历史导入及补充边界检查的日志完整保留，没有删断言或跳过失败。Ruling：历史 Read 必须保留在每个 main 请求中，否则缺 transcript 可导致空历史成功；辅助 title 不承认工具配对，仍继续 per-call Permit/Source 核验。历史记录只从可信 seal 导入，不能从 Native transcript 生成授权。

同时修正 ARCHIVE-01 的复现命令：run-go.py 第一个参数是日志路径，随后才是 Go verb；原实际测试日志和 owning commit 摘要保持，初次误用命令的临时帮助输出已清理，不计为 TDD RED。

复现（仓库根目录，输出日志任选未用临时路径）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-grok-restore-host.log test -race -v -count=1 -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/grok -run '^TestRestore'
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-grok-restore-native.log test -race -v -count=1 -timeout=180s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/grok -run '^Test(Grok(CallGatePinnedNative|ControlledReadPinnedNative|AdapterPinnedNative)|ManagedPinnedNativeGrokChannel)' -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

test-results.json 保留独立顶层/子场景计数与限制，artifacts.json 绑定当前源码和本包摘要。JSON/counts/doc links/gofmt/diff/key 排除随提交检查；主工作区两个既有 `.DS_Store` 保留。

未完成：typed seed 在新私有 Root 中的安装、新 config/grant/端口、准确 `--resume UUID` 的受管启动、持久恢复 mapping/receipt、整体 Source/当前项目权限、真实 account/pool/billing/Quota 准入、产品 Worker 和工程闭环。没有真实模型/额度调用，Jev off；60 类最终 Gate 全部仍 not_run。
