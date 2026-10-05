# 工作流 Management API 与 Native 通路

对应 WP-19-WORKFLOW-API-01。仅管理权限可附加流程、冻结方案或明确批准；Worker/stage bearer、模型结果和普通 continue 均无此权限。这些操作只写元数据，不调用模型/额度、不改变来源的 read/write 声明、不启动或恢复 Runtime。生产三路线 Gate A、实际写范围执行与工程闭环仍未完成。UI 消费尚未接入，后续只扩展原 Magpie 任务详情区，不改整体样式。

## 固定端点

| 方法 | 路径 | 请求 |
| --- | --- | --- |
| GET | `/control/v1/tasks/{task_id}/workflow` | 无 body；读取当前 Task 和流程 |
| POST | `/control/v1/tasks/{task_id}/workflow` | `{"kind":"change"}`；另允许 bugfix、investigate、review |
| POST | `/control/v1/tasks/{task_id}/workflow/design` | `run_id` 与完整 `document` |
| POST | `/control/v1/tasks/{task_id}/workflow/approve` | `design_hash` 与 `acceptance_hash` |

所有成功返回 200 `WorkflowReply {task, workflow}`，并提供当前 Task 的完整 `ETag`。登记 Task 尚未附加时 workflow=null；Task 不存在或所属项目未登记时 404，不返回私有目标。读取得到的 Task 与流程按同一未改变的 Task 条件核对，变化时拒绝响应。已有冻结设计/批准在宿主重开后可回读；读取不是执行或恢复准入。

POST 必须带唯一 `If-Match`，例如 `"p1-g1-ready"`，同时匹配 plan_revision、generation 和 state。当前条件不符 412，缺失 428，格式或重复 header 400。写事务仍以完整 TaskVersion 校验并发变化；事务冲突可以返回 409，客户端应重新读取核对，不能盲目换 key/条件/模型。接口不使用 `Idempotency-Key`，包括空 header 在内均拒绝；不可变内容的重复提交只读原记录，不增加事件或扣预算。

设计请求示例：

```json
{
  "run_id": "原 design run 的精确 ID",
  "document": {
    "goal": "原 Task 的目标",
    "scope": ["src"],
    "constraints": ["保持公开接口兼容"],
    "interfaces": ["需要修改的明确接口"],
    "acceptance": ["可实际核验的验收条件"]
  }
}
```

方案必须来自同一 Task 已 succeeded、确认启动并可靠停止释放的 design run；接口不接受 stopped_verified、凭据、目标、权限、Native session 或任意 argv。目标、相对路径范围、UTF-8 与文档 64 KiB 限制由[设计合同](workflow-design-gate.md)落实。hash 由服务端冻结，不由模型声明成功。Scope 仍只是声明，不能替代 OS 文件访问约束。

用户审阅原方案后，approve 请求分别提交其两项冻结 hash，格式为 64 位小写十六进制。hash、Task 或计划不匹配均拒绝；只批准当前计划，新计划需要新批准。重复相同冻结文档/批准可回读，不能覆盖原决定。普通 Task continue 只解除已有暂停；它不调用批准方法，批准自身也不派单。

## 权限、失败与并发

Management middleware 认证并去除原凭据后，Handler 在解析前、等待 Server 锁后、读写前后及发送回执前重新核验当前权限/取消状态。三个 `Store.*Authorized` 写入口还在 Store 锁内、提交事务前核验管理权限；等待期间或写入后被撤销时整次 metadata/event 回滚，返回零值，不残留部分授权。回调须为本地非阻塞检查，不重入 Store。ManagementCurrent 仅核对原 issuer、取消状态与原子管理权限标记，不取得 Manager 锁；这样不会与持有 Manager 锁并查询 Store 的 stage validator 形成锁反转。grant map 和 Worker 准入仍由原 Manager 锁及 StoreValidator 保护，撤销后不保留管理权限。原无 Authorized 后缀方法仅供可信内部宿主使用，不能直接暴露给 Worker。

若事务已经提交，之后权限或来源才失效，接口/Native 不披露该回执，已提交真实决定保留。重新授权后 GET 核对原 Task、文档和当前批准，不能伪造回滚或重新运行模型。read/write 在丢失权限时返回 401；Native 宿主来源/窗口关闭使用其原 503 合同。

| 状态 | 典型原因 |
| --- | --- |
| 400 | 非规范 JSON、未知/重复字段、未知 kind、非法 hash/范围、GET body、重复条件、不支持的 key |
| 401 / 403 | 管理权限失效/缺失、stage bearer、跨站来源或 Native 窗口不匹配 |
| 404 | 不存在或未登记项目的 Task；Native 未允许的路径/方法 |
| 405 | HTTP 已登记路径的方法不支持 |
| 409 | 方案/标准冲突、不可替换记录、前序或 Task 尚未允许；流程阻断为 workflow_requires_review |
| 412 / 428 | 当前 Task 条件变化 / 未提供条件 |
| 500 / 503 | 存储或宿主不可用；不返回私有错误文本 |

## Native 边界

NativeStageBridge 仅向这三条精确路径开放四个方法组合：workflow GET/POST、design POST、approve POST。沿用固定 Wails window id/name、虚拟资产来源、当前私有来源/token、已登记 Task 项目、取消/关闭和缓冲响应复核；不提供通用控制代理，也不传递管理凭据给页面。`If-Match` 不合并重复值；不支持的 key 在桥处拒绝，不静默丢弃。未知后缀、HEAD/PUT 和 GET approve/design 均不开放。

通路测试通过真实本机 HTTP 和 Native 桥执行 metadata 操作，原管理凭据始终留在宿主。Native 设计完成输入使用可信合成 Store 生命周期，未执行真实供应商进程；API lifecycle 另使用真实 Controller/Scheduler、合成 Runtime。实际 Wails 窗口点击、写副本/Handoff、供应商生成和最终 T01–T60 不由本组件证明。

## 验证与回退

Go1.26.3，仓库根目录：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/wp19-workflow-full-fusion-final.log \
  test -mod=readonly -tags fusion,nogui -race -count=1 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

OpenAPI 合同增加 3 路径/4 操作，见[合同验证](openapi-verification.md)。官方 schema 离线校验、真实 Handler 样本、DTO 输入/响应反例均同步，不能替代运行鉴权或内容 hash 语义核验。证据见[组件结果](../work-items/WP-19/WORKFLOW-API-01/summary.md)。本组件不增加 schema10，不改 001–009；回退仍保留 schema9 状态并按设计合同停机/备份，不改 user_version 或删除批准记录。原 UI/CSS 逐字保留，Jev off。
