# WP-13-ARCHIVE-01：可信 Grok 会话归档

基线 `f070043cefdb067a021813e53b5afe46e09307d8`。本工作包完成成功执行的私有归档生产端和持久核验；WP-13/full Resume 仍 in_progress。没有修改 Store schema、公共 API、依赖或产品执行开关。合同见 [grok-checkpoint.md](../../../contracts/grok-checkpoint.md)。

同 Adapter 持有的实际 Handle 必须成功、wait、VerifyStop、可信 Release 并释放 reservation，才能归档。冻结 run/原 owner/Target/UUID/停止证明、原 Native Root/cwd 身份、完整 14 文件和已批准 Read；随机私有 key 的 HMAC seal、文件摘要及 0600/0700、fsync/atomic publish 支持重开核验。调用者重算公开 digest 不能替换冻结账号或其它 payload。失败、取消、不同 Adapter/generation、文件漂移及原读取 Source 改变均拒绝。

RED→GREEN 留存：初次缺接口编译 RED；Host fixture 的 lock 误写 `{}`，依据实际 Native 空 lock 修正 fixture。实际 Native 成功归档首轮 RED 的根因是 Store.Finish 清空 owner；采用 owned Handle/StopProof/Release，并单独保留原 owner，未放松模型/账号绑定。超过 1MiB 的合法 Read manifest 曾发布后无法重开，新增独立 2MiB 归档 JSON 边界，保留 Native 帧上限。批准 Source 文件的 UID 必须原样记录，不要求所有 Source 文件属于当前用户；私有根目录/key 的 owner 仍严格核验。完整替换 Native Root 曾被接受，新增原目录身份检查后对应 RED 转 GREEN。

验证结果（顶层计数与子场景分别列出，原始日志和 JSON 保留）：

- 最终新增 Host：9 PASS、29 个通过子场景、0 FAIL，覆盖大小边界、MAC 伪造、取消不发布、链接/布局/凭据保护、目录替换和 Source 授权撤销。
- 固定实际 Grok Adapter/协议回归：7 PASS、30 个通过子场景；另有一个无子场景的叶用例，共 31 场景，其中 30 启动 Native、1 启动前 drift。新增 checkpoint 4 场景中，文本和 Read 各发布完整 14 文件，原批准 Read 分别 0/1；private-config 失败和 inflight cancel 均不发布。
- 实际 Native 校验了 Release 前拒绝、foreign Adapter/generation 拒绝、重复 reference、重开私有 seal。删除原 Native updates 不损坏已完成私有副本，但不能重新归档不完整源；修改原 Read 文件使旧批准记录失效。
- 固定 Native 控制器生命周期：1 PASS、7 个通过子场景，包含成功/Read/断线/暂停/两类取消/关闭及真实 wait/StopProof/Release。
- 全量 Fusion tagged race：418 PASS、18 SKIP、0 FAIL；未传 Native flag 的 SKIP 不算实际 Native 通过。
- Go 1.26.3 CLI/GUI 编译和 tagged vet 均 exit 0。最终检查后的变更仅为文档及证据包。

实际执行使用生产 default-deny profile、真实 Store/Manager/Scheduler/Supervisor 和固定 Native；上游/Inspector/Quota 仍合成，没有真实模型/额度调用、日常授权或 Keychain 导入。每 HTTP Permit 仍记账，停止证明不升级为真实账号/模型/计费/额度准入。Jev off。

复现（在仓库根目录运行，Native flag 只用于明确固定 executable）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-grok-archive-check.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/runtime/grok -run '^Test(Grok(CallGatePinnedNative|ControlledReadPinnedNative|AdapterPinnedNative)|ManagedPinnedNativeGrokChannel)' -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

`test-results.json` 记录各 RED/GREEN 日志计数、边界及限制；`artifacts.json` 绑定本项源码与证据包摘要。JSON、文档链接、gofmt、diff 及私有 key 排除检查随提交完成。主工作区已有两个 `.DS_Store` 保留。源码 graph 已刷新，文档不进入索引。

未完成：整个 Source 稳定性/当前项目权限服务、真实账号/池/计费/额度准入、可信 restore seed、历史 Read 导入、新授权/预算/持久恢复 receipt、实际受管 Resume、产品 Worker 和工程闭环。私有 key rotation/删除/通用备份 API 也未提供。60 类最终 Gate 全部仍 not_run，本项不宣称 WP-13 或整体任务完成。
