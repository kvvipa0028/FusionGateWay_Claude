# GLM 单阶段生产 Factory

`bootstrap.NewGLMRuntimeFactory(GLMRuntimeConfig)` 将既有 GLM Adapter、FileCredential、固定 CNTransport、可信 Scheduler/Manager 和私有工作副本接到 [OpenExecutionControl](execution-host.md)。这次交付完成实际后端组装，不自动授予真实账号准入，也没有向项目 JSON、任务 HTTP 或 CLI 开放 `admitted` 开关。默认产品 CLI/GUI 仍不启动模型，Jev off。

## 必需的可信输入

- 精确 project ID、route ID/revision；Registry 必须用独立报告的内容验证器完成 generation admission。没有 Registry、没有 verifier 或只有声明的路线都拒绝。
- 当前 Inspector，独立提供账号、额度/物理池、数据外发权限、沙箱和验证能力的证据。它仍受原 Scheduler 每次检查约束；Factory 不生成 true 标志、完整额度或可用池。
- 固定版本的 Claude Code 绝对路径；已准入 route 必须为 CN / bigmodel / claude / api_key / coding_plan、Runtime 2.1.287，且与私有项目声明的全部身份和 effort 对应。每次启动仍由 Adapter Probe 校验冻结 executable hash。
- 仓库外的私有 `glm-coding-plan.key` 文件，沿用 FileCredential 的 owner、0600、无链接、单链接、内容/inode 冻结和逐次加载检查。
- 已存在、当前用户独占的私有 ExecutionRoot，不能与任何项目互相包含，不能包含凭据。冻结根目录 inode，初始化后被替换、权限改变或出现链接均拒绝。

这是 Go 服务端组装接口，不能把这些服务引用换成用户 JSON，也不能使用测试 Inspector 或只比较字符串 hash 的 Registry 验证器：

```go
factory, err := bootstrap.NewGLMRuntimeFactory(bootstrap.GLMRuntimeConfig{
    ProjectID: projectID, Route: routeRef,
    Registry: independentlyVerifiedRegistry,
    Inspect: currentAdmissionInspector,
    Executable: pinnedClaudeExecutable,
    CredentialPath: privateCredentialPath,
    ExecutionRoot: privateExecutionRoot,
})
// 处理 err 后，在原管理鉴权和私有登记边界内：
host, err := bootstrap.OpenExecutionControl(ctx, sourcePath, stateRoot, addr, factory)
```

Timeout 默认 2 分钟，可由可信宿主配置为不超过 4 分钟的正值。初始化不查询供应商，不调用 Inspector/Scheduler，不启动任务或发放 stage/model grant。公开构造器固定使用既有 CNTransport；可替换上游的函数仅在 bootstrap 包内部供测试使用，不接受外部 URL、普通 API、Global host、argv 或另一账号 fallback。

## 执行与撤销

只接受已经冻结的单角色计划。角色和目标从 Store/Controller 取得；模型、账号、effort、计费路径、Runtime、capabilities 必须匹配 Registry 当前不可变路线。JSON prompt 保留实际 Task goal 和 role，目标文本作为字段传递，不作为 argv。每次 Resolve 创建独立 `launch-*` 目录，分别放置 workspace 副本和 worker root；Copy/SourceGuard 必须对应原登记目录，原项目不由 Native 修改。

只有项目显式授权 write 且角色为 implementation/testing 时才生成 Writable Spec，并要求 Inspector 提供 writer 预留。design/review/acceptance 和只读项目不能获得写权限。复制前核验 DataAllowed；权限、原来源、凭据、路线或执行根变化时拒绝。

每个实际 Start 使用 owned context，并以 100ms 间隔检查当前路线、私有登记、凭据和执行根。撤销会取消真实 Adapter/Native 进程及其在途模型请求；已知 Handle 即使同时返回错误也保留。只有实际 Handle 终态 Wait 才结束 watcher，Controller 继续负责真实 StopProof 和 Release；不凭“已请求取消”提前释放容量，不退款、不重放。此检查是有界观察，不构成外部文件树的原子写锁。

## 工程闭环边界

多阶段任务需要传递已验证实施副本、设计合同、测试和审查产物；重新复制原项目会丢失上一阶段修改。因此本 Factory 暂时返回 unsupported，而不是把独立单阶段复制伪装成五阶段 Handoff。完整工程交接仍属于必须实现的后续工作，不缩减 WP-19–WP-22 或最终 T01–T60。

本组件没有登记真实账号/publisher/计费/物理池报告，没有打开产品 CLI 执行，也没有新增 quota reader。真实 CN quota-only 查询保持独立；其 unverified 快照仍不能放行 Scheduler。OpenAI/X 登录、实际供应商 Gate A、完整原 Magpie 主界面整合与实际 Native UI 点击仍未完成。

## 验证与回退

实际固定 Claude Code 进程经产品 HTTP、Controller、Store 和这个 Factory 完成成功、真实 Edit 创建副本文件、取消、Registry 撤销、凭据轮换和源声明撤销六个场景。上游、Registry 和 Inspector 使用合成 fixtures；真实私有 key 没有进入这些测试，不把进程/沙箱证据当作真实供应商准入。证据、初始失败及最终结果见 [GLM-FACTORY-01](../work-items/WP-14/GLM-FACTORY-01/summary.md)。

复现：使用 Go1.26.3，在仓库根执行已有隔离 runner，显式指定冻结的可执行文件；不使用 `~/.local/bin/claude` 可变 launcher，不填真实 key：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/glm-factory-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=75s -run '^TestGLMFactory' -v ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
```

没有该 pin 的环境可运行不带 Native flag 的单元测试；Native 场景会明确 skip，不能记作通过。回退时停止派单，确认所有 owned Handle 实际停止并释放，再去掉可信 Factory 登记或撤回本组件；不删除未核验 launch 目录、重放请求或回滚 SQLite。现有草稿/额度产品入口、schema9、所有 UI 文件及样式保持原状。
