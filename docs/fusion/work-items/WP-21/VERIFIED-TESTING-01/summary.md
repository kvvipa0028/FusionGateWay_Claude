# WP-21-VERIFIED-TESTING-01 · 实际 testing 与持久可信证据

本组件完成，BASE `a46de1137abd38ed284a73a056c50f282cbbfe3b`；WP-21 整体继续 in_progress。原 Magpie UI/CSS 零修改，Jev off。本机真实固定 Claude Code 2.1.287 使用合成上游/key/准入，真实供应商调用为 0。

## 实现与行为

- `bootstrap.GLMVerificationConfig` 注册独立可信 Spec 与 Acceptance，Factory 构造深复制、检查工具 pin；多角色 testing 在启动前要求与人工已批准设计的 Acceptance 逐项一致。缺少验证器或标准不符均无新 Native 意图/模型请求。
- 实际 testing Native 成功停止、真实代码 Freeze 后执行 pinned 版本查询及测试命令。实际报告、exit、停止、产物/测试/spec/environment hash 决定 hard verdict；模型文本不能设置结论。
- `handoff.PublishVerified` 只接受 owned Result；摘要记录真实 tests_executed 与 passed/failed/unverified，不再错误保留 engineering_tests_not_executed。普通 Publish 继续 advisory/unverified。header 本身没有派单或证据赋权。
- `store.RecordVerifiedArtifactAuthorized` 从 owned Result 生成 optional Verification，在原 immutable receipt 的 Task/Plan/Run/target/parent/current transaction 内登记真实报告与 DesignHash/AcceptanceHash；普通写入拒绝自声明 JSON。非 passed 同时写 Task needs_review 和独立事件，事件故障全部回滚，精确重试幂等。
- Native succeeded 保留原进程协议事实；真正的硬失败/无效报告通过 Task needs_review 阻断后续，不能被模型完成覆盖。注册没有释放 Native writer，实际执行器停止与原 Adapter Release 后方可受控读取。
- `VerifiedArtifact` 按 exact released-run 私有 receipt 和独立 source/ExecutionRoot 恢复真实产物，核验 header、标准、代码与报告；重启可核验，改变标准返回 superseded。Stored JSON/ValidStored/EvaluateStored 本身不建立来源，也不许可启动后续阶段。

公开 Task/HTTP API、模型 Adapter、权限策略与 SQL001–010 不改；ArtifactRecord 私有可选字段保留旧 receipt 的 canonical bytes。handoff v1 schema 扩展为上述真实 testing 摘要，兼容旧 advisory 文档；旧严格消费者不能读取新 verified 形状，按下述回退边界处理。

## 验证与证据

Go1.26.3，Fusion race 回归 685 个顶层、1238 个子测试 PASS，33 个顶层、11 个子测试 SKIP，19 packages，0 FAIL。未指定 Native pin 的测试按原规则 skip，不算真实 Native 通过。

显式指定固定真实 Native 的 Factory 回归 10 个顶层、41 个子测试 PASS，0 SKIP/FAIL。实际 design→人工批准→implementation→宿主 reopen→testing 继续原代码、限域 Edit、真实直接验证、持久登记和再次 reopen 核验通过；硬 exit7、无效报告、缺失 verifier、错误标准、调用者修改原 config、Native 越界 Edit、父 header 漂移和宿主越界修改均覆盖。

Store targeted 3 个顶层 PASS，覆盖 held release 阻断、原 receipt immutable、公开伪造/空 Result/异源产物拒绝、取消 current 不写事件、重启、标准变化、硬失败事件回滚与精确幂等。实际 captured owned Record、报告/stderr hash、真实 verified header、历史 advisory header 以及 6 个错误 header 通过离线 schema 检查。

CLI/GUI build 与全仓 go vet exit0；本次没有运行真实 GUI 点击验收。缺少新 API 的编译 RED 保留；有效 mutation 临时移除 needs_review 后，真实 Native hard_test_failure 因 Task 留在 ready 而 FAIL，生产 bytes 精确恢复后同一场景 PASS。日志和检查明细见 [test-results.json](test-results.json)、[artifacts.json](artifacts.json)、[schema-results.json](schema-results.json)。capture 归档脚本最初错误处理 nil stderr，已改为空 bytes 并核验实际 hash；不涉及生产更改。

## 复现

在 implementation checkout 根运行。只传固定工具路径，不传真实 key：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/verified-native-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=150s -run '^TestGLM(Factory|StageWritePaths)' -v \
  ./internal/fusion/bootstrap \
  -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287

PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/verified-fusion-check.log test -mod=readonly -tags fusion,nogui \
  -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...

PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
python3 docs/fusion/work-items/WP-21/VERIFIED-TESTING-01/check-schema.py
```

schema 命令需要现有 Python jsonschema。可选 `-fusion-verification-capture /absolute/private/path.json` 仅给 Store targeted 测试导出合成 owned 文档；不是生产导入接口。不覆盖已归档证据文件。

## 未完成与回退

受控子进程工具链 backend、review/acceptance 消费、有限返工、真实账号/项目 Gate A、Native UI 点击和最终 T01–T60 仍须实现/验证。多角色 review/acceptance 继续 unsupported。本 backend 禁止 fork；不能直接运行 shell/Go/compiler 流水线，不能冒充整项目编译、HIL/WCET 或最终验收。

schema10 不变不代表旧二进制可读新 Verification：旧 canonical reader 和旧严格 handoff schema 会安全拒绝。回退前停止派单并确认所有 owned Native/验证器真实停止；恢复同一检查点的私有数据库和 artifact/ExecutionRoot 备份，再启用对应二进制。不删字段伪装兼容、不自动重放或替换模型/账号、不改真实项目仓库。
