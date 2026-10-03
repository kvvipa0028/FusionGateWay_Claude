# WP-15-RESUME-API-01：明确恢复的幂等控制与管理 API

基线 `26e010dc31efc82d7f8d7de6fa57aef2cfcbe29d`。本项完成明确成功 checkpoint 恢复的内部 Store→Scheduler→Controller→Management Handler 及具体 Grok 绑定。WP-15/WP-13 仍 in_progress，真实执行 bootstrap/Worker/GUI 与独立真实路线准入未完成，60 类最终 Gate not_run。合同：[task-resume-api.md](../../../contracts/task-resume-api.md)。

`StartIdentity.restore` 记录原 run/checkpoint ID/digest并纳入既有 start_requests hash/事务。nil 的 JSON/hash与旧普通启动完全相同，不改 schema；新恢复要求原成功 confirmed Native run、同 task/role/plan/frozen Target、已释放 reservation/stop proof，与当前 generation/状态/预算/容量在事务内一起核对。改变 restore 或用相同 key 改成普通启动不能读取旧映射。Scheduler 在 Inspection 前复制引用；持久重开后 unknown/interrupted 只读，不重放，不退款、不释放 held。

Controller 增加 Restore/RestoreAuthorized 与明确 CheckRestore/Restore 后端，普通 Start 拒绝 restore。所有 argv/executable/env/channel/权限/路径/input限制沿用现有边界；恢复当前仅 readonly。BindGrokCheckpoint 由服务端持有私有 Archives 和 Adapter，密封/Source/旧身份在 intent 前核验，准确 frozen Target 不能更换；实际 launch 由 Adapter.ResumeCheckpoint 再重核新 prepared scope。提交后仍是 owned lifetime、准确 Cancel/Wait/StopProof/Release。无 Handle 返回 409 和安全已提交 receipt，保留 needs_review/held。重复请求在 Resolve/归档/Inspection 前只读同一 run。

内部 Management Handler 注册 `POST /control/v1/tasks/{task_id}/resume`，正文仅 role/restore，单一 ready Task strong If-Match 和 Idempotency-Key，管理身份在慢预检查与 Inspection 前后重查。回复复用安全 RunView/ExecutionReply；202 新意图、200 只读重试，失败输出固定 code/message，Location/X-Fusion-Task-ETag 对应实际 run/Task。Stage/query/Origin/body/重复字段和 header 沿用原 middleware/decoder。普通 Start 和 Continue 语义不变；通用缺参数 Resume 仍 unsupported。

验证：

- 新 Host 10 顶层/30 子测试 PASS（Store 3、Scheduler 1、Controller 3、API 3）；覆盖 legacy hash、并发单 intent、8 类原 scope/结构拒绝、引用冻结、实际 Store 重开、origin/checkpoint冲突、只读重复、revoke/未绑定/writable拒绝、未知 intent held、真实 loopback HTTP/条件/正文/错误脱敏。
- 新实际 Grok Controller 恢复 3 场景：准确恢复成功、恢复中取消、提交后请求断线。每场景先实际原 Native→成功归档，再新 Native→准确原 UUID，合计 6 次 Native 启动。旧2 HTTP+新1 HTTP全部持久计数，取消实际停止并释放，断线仍完成，重复 key 没有新进程/调用。
- 与原 Grok 7 场景和 Claude 3 场景一起显式回归：5 顶层/10 子测试 PASS，共 13 场景、16 次实际 Native 启动。这里 Native 经 Controller 验证；Management API 的真实 HTTP测试使用合成 Backend，未宣称真实订阅经 HTTP 的产品端到端已完成。
- 完整 Fusion race **442 PASS/20 SKIP/0 FAIL**，620 个 PASS 子测试。20 个 skip 不算通过；显式 Native 单独运行。Go 1.26.3 CLI/GUI build 与 vet 均 exit 0；源码之后未改，仅补合同/证据。
- OpenAPI离线标准校验 **25 paths/29 operations/52 actual Handler samples/29 covered operations/8 negative schema cases**。新增 resume 202/200/未知409样本，schema拒绝Native/session/argv等越权；不因Unknown而删除样本或头。
- graph刷新为13586 nodes、119243 edges；凭据排除、JSON/链接/摘要/gofmt/diff在提交前核验。Jev off；真实模型/Quota调用0。

RED/裁决：缺失RestoreIdentity/Restore/绑定方法的编译RED，未注册resume路径的实际HTTP RED，新增样本与旧OpenAPI缺路径的RED均保留。Native测试最初使用旧记忆的Checkpoint参数，按实际签名更正后仅缺绑定的RED保留。OpenAPI补路径后发现已有Start风格的409 intent头未描述，补齐Start/Resume实际Location/Task头。Interrupted为现有终态Cancel只读合同，测试验证状态和held未改变，未将其强行改为ErrReconcile；实际重开Store验证原恢复映射只读。

复现（仓库根目录，独立HOME/XDG helper先接日志路径）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-resume-host.log test -race -v -count=1 -timeout=120s -mod=readonly -tags fusion,nogui ./internal/fusion/store ./internal/fusion/policy ./internal/fusion/control ./internal/fusion/api -run '^Test(Restore|PrepareOnceRestore|ControllerRestore|ResumeAPI)'
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-resume-native.log test -race -v -count=1 -timeout=180s -mod=readonly -tags fusion,nogui ./internal/fusion/control -run '^TestControllerPinned(Grok|Native)' -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 -fusion-control-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
python3 scripts/fusion/check-openapi.py --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json --samples docs/fusion/work-items/WP-15/RESUME-API-01/wp15-resume-handler-samples.json
```

剩余边界：API已在内部Handler接线，真实执行Controller未在产品bootstrap注册；真实Grok Forwarder/私有X认证/账号/计费/Quota/pool、整个Source稳定性和当前项目授权服务仍待完成。写恢复、其它effort、暂停/未知/取消来源的副作用核对、跨工程角色Handoff/返工/验收、最终60类Gate不因本项通过而标记完成。私有Archive mapping与request hash都不是停止证明。

