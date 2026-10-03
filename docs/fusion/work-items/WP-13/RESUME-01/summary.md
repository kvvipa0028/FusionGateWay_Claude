# WP-13-RESUME-01：可信内部受管恢复

基线 `483297e0f25da0baece3bafbdd3ff7d6a3e70188`。本项完成成功归档到实际受管 Native 的恢复链，WP-13 仍 in_progress，60 类最终 Gate 仍 not_run。合同：[grok-managed-resume.md](../../../contracts/grok-managed-resume.md)。

`Adapter.ResumeCheckpoint(ctx, run, spec, archives, ref)` 消费已密封、已停止并释放的成功归档。Scheduler/Store 重核 distinct prepared run、generation/attempt、同任务/角色/project/revision/冻结 Target/cwd 与当前 readonly reservation。原批准 Read 导入新 scope；每次 main HTTP 要求历史 assistant/tool 配对，新增 Read 单独批准和计数，全部 HTTP 仍独立 Permit。无准确输入的通用 Resume 保持 unsupported。

新私有 Root 的 device/inode/UID 在回调前冻结，Current 前后重查；新 model grant、端口、config 和 prompt 均属于新 run。私有 typed seed 复制完整 14 文件、有界校验、0600/fsync/atomic 发布，安装后或失败时清空内存且禁止重复使用。只运行 `--resume` 原准确 UUID；不通过 session-id、标题、continue 或 fork 替代。保持现有 default-deny profile、协议/输出保护、wait/StopProof/Release。成功恢复可再次归档，失败与取消无成功产物。

恢复准备关系以私有 HMAC mapping 持久化，在 spawn 前记录新/旧 run、UUID、generation、Root 身份和 prompt SHA256，不含原始 prompt/grant/key。独占发布不覆盖其它 opener；同内容读取可重复，改变 prompt/Root 拒绝。RestoreInfo 仅核验准备元数据，不代表已启动或停止，不自动恢复未知/活动 run。重复低层 dispatch 拒绝第二次启动；产品 HTTP 幂等响应仍待实现。完整 Store/Archives/Manager/Adapter 重开验证限定为原 owner 成功停止并释放后的准确恢复。

验证结果：

- Host 最终针对性验证 14 PASS、61 子测试 PASS，其中 6 个新顶层测试（2 seed、4 receipt），另外 8 个为原 restore 回归。
- 新增实际 Native 恢复 12 场景：text/read/new_read/unicode_cwd/reopen/store_reopen 六成功，cancel_read 一取消，quota_drift 一失败，bad_ref/source_drift/target_drift/root_drift 四个新启动前拒绝；包含 20 次实际 Native 启动（每场景先实际生成原成功归档）。
- 联合 Grok 最终验证 8 顶层 PASS、42 子测试 PASS，共 43 场景，其中 42 场景运行 Native、1 场景仅启动前 Current 拒绝；合计 50 次 Native 启动。另有控制器 1 顶层/7 子测试 PASS、7 次 Native 启动。准确 UUID、完整历史、新 grant/config、持久调用计数、Source/Read 重核、取消停止释放均有断言。
- 完整 Fusion `-race` 回归 432 PASS、19 SKIP、0 FAIL（590 个 PASS 子测试）；显式 Native 测试单独运行，跳过不算通过。Go 1.26.3 CLI/GUI 编译与 vet 均 exit 0；编译之后仅增加 test fixture 的 StoreRoot 元数据及重开场景，最终完整/Native 回归覆盖这些测试变更。
- graph 已刷新：13558 nodes、118841 edges。之后仅更新文档/证据包；JSON、链接、文件摘要、gofmt、diff 和凭据排除在提交前核验。

RED 与裁决保留在日志：缺失 seed/ResumeCheckpoint 接口的编译 RED；Root 被替换仍启动的实际 RED 后增加身份冻结与回调前后重查；安装后 seed 仍保留历史字节的 RED 后改为一次使用并清空。第一次调用计数 oracle 假定恢复还生成 title，实际已有 title 的恢复仅 main HTTP 一次，按实际 Forwarder/持久预算改正；new_read 恢复为两次 main。取消/额度 fault injection 原在 message 扫描中提前返回，改为完整扫描历史配对后注入，保留原配对断言。未弱化 Permit 或历史授权。空控制器 selector 日志含 `no tests to run`，仅属诊断；随后按 graph 中准确 symbol 运行成功。

复现新增 Native 场景（工作区根目录，必须先传日志路径再传 Go verb）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-grok-resume-native.log test -race -v -count=1 -timeout=240s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/grok -run '^TestGrokAdapterPinnedNativeResume$' -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
```

真实模型/真实 Quota 调用均为零；upstream、inspection 和 Quota 使用合成数据。Jev off。未修改 Store schema、依赖、产品执行开关；CLI/GUI 未注册真实 Grok Worker。产品恢复 API/幂等 receipt/Worker、真实 NativeForwarder/私有 X 认证/账号/计费/Quota/pool、整个 Source 稳定性与当前项目授权服务仍待完成。写工具、其它 effort、unknown run 自动恢复、归档 key rotation/cleanup 不在本项完成范围。组件通过不等于五阶段工程闭环、真实订阅准入或全部需求完成。

