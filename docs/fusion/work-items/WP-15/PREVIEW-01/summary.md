# WP-15-PREVIEW-01：冻结预览与任务提交

状态：本子工作项 `done`，父工作包 WP-15 为 `in_progress`。新增 `internal/fusion/api` 组件；无新 HTTP listener、模型连接或 Runtime 启动。

## 接口合同

所有接口先经过 WP-06 管理 Bearer 鉴权；stage token、URL key 和跨 Origin 请求不获管理权限。HTTP body 仅接受规范 JSON 字段，不接受账号密钥、workspace path、global/project 配置、准入标志、stage capability 或冻结 ExecutionTarget。

| 方法与路径 | 输入 | 行为 |
|---|---|---|
| POST `/control/v1/tasks/preview` | project_id、goal、required_roles、可选 task layer | 使用受信任项目的 global/project layers 和 route registry 编译；返回 preview_id、configuration_revision、expires_at、带 hash 的 plan；没有模型调用 |
| POST `/agent/v1/tasks` | preview_id、plan_hash；Idempotency-Key header | 固化原预览 goal/project/plan，不接受客户端替换目标；调用 Store.Create 的同一事务保存快照、幂等记录和 created event |
| GET `/agent/v1/tasks/{id}` | 已存在 task ID | 返回当前持久任务；不启动或恢复执行 |

未提交预览有效期 5 分钟，最多保留 1024 个；受信任项目最多 128 个、每个登记最多 256 条 route。输入 body 上限 128 KiB、goal 上限 64 KiB，计划使用既有 64 KiB validator。拒绝重复字段、大小写别名、非规范字段、无效 UTF-8、null、额外 JSON、嵌套深度溢出和不支持的编码。错误只返回常量 code 与 HTTP 描述，不回显错误输入或依赖原始报错；未准入 route 返回 `route_revision_not_admitted`。

SetProject 仅供受信任控制器调用，复制整个配置并递增 revision；即使重写同值，也拒绝旧未提交预览。配置变更和提交使用同一组件锁，提交不在检查后重新解释新配置。已提交 receipt 的同 key 重试读取原任务；不能通过另一个 key 再用同 receipt 创建任务。Store 层持续检查 project-scoped key 与固定 payload 的冲突。

预览是当前 API 进程内的临时值；API 重启后旧预览返回 409，客户端重新预览，再用原 Idempotency-Key 提交。持久任务的幂等记录会返回原 task，不因丢失预览重复创建或执行。预览 expiry 不是 worker lease，不提供执行授权；路线、额度、沙箱和共享预算必须在真正启动前再次检查。

## 验证

模块缺失先 RED；可行动阻断码缺失先 RED，补齐常量 JSON 错误后 GREEN。14 个 API top-level tests 验证预览/快照 hash 一致、stale/tampered/expired 拒绝、8 路并发同 key、key payload 冲突、配置和返回值无指针串改、未准入路线、凭据/路径/全局配置注入、鉴权及重启后幂等。持久 events 仅有一次 created，不出现启动事件。

完整 Fusion tagged race：156 个 top-level tests 通过、2 个父进程 helper skip（其子进程在既有 OS 测试中执行）。Go 1.26.3 的 Fusion CLI、GUI 编译和全仓 Fusion/nogui vet 均 exit 0。全部使用隔离 HOME/XDG 和合成 route/账号，没有真实套餐调用。

## 未完成部分

这个 Handler 暂未注册到生产 gateway/GUI；真实路线没有借用 fixture 的 Admitted=true。预算持久配置与任务提交的整体合同、更新未开始阶段的 If-Match、SSE 实时鉴权/补读、取消/暂停/继续和调度执行仍在 WP-15 范围内。UI、真实工作流和最终验收未完成，父包不能标 done。
