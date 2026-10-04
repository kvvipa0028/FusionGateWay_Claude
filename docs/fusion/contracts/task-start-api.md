# 原阶段启动记录 API 与 Native 通路

组件 WP-15-START-JOURNAL-API-01，基于 [Store 持久记录](task-start-journal.md)。Management API 与精确 Native 桥已接入；原 Magpie 详情区的 UI 消费尚未接入，本组件未改 UI/CSS。记录只用于恢复原请求，不授予执行准入、Runtime 接管、停止证明或验收权限。

## 固定接口

| 方法 | 路径 | 输入与结果 |
| --- | --- | --- |
| GET | `/control/v1/projects/{project_id}/start-request` | 登记项目的 prepared/committed 记录；没有待解决记录时 `{"request":null}` |
| POST | `/control/v1/tasks/{task_id}/start-request` | 严格 `{role}`；从可信 Store 保存原 ready Task、历史 Plan 和 key，不创建 run |
| GET | `/control/v1/tasks/{task_id}/start-request` | 用原 key 和完整原 Task 条件读取记录，包括 acknowledged/abandoned 历史 |
| POST | `/control/v1/tasks/{task_id}/start-request/acknowledge` | 严格 `{role,run_id}`，仅确认原记录关联的精确 run |
| POST | `/control/v1/tasks/{task_id}/start-request/abandon` | 严格 `{role}`，仅封存没有 run 回执的原请求 |

Task 接口必须携带单个 `Idempotency-Key` 和原完整 `If-Match`，例如 `"p1-g0-ready"`；不是当前任务的新条件或仅计划版本。GET 不接收 body、role header 或 query；角色从受校验的原记录读取。项目 pending GET 不依赖客户端原 key，供窗口重开后发现记录。所有操作成功为 200，Task 精确读取/写入的 request 非 null。

响应 `request` 包含 key、原 Task、原 Plan、role、etag、state、run_id。原 Task 始终是准备时的 ready Task；运行后不能把它当作当前运行状态。prepared/abandoned 的 run_id 为空，committed/acknowledged 的 run_id 为原回执关联 ID。不返回管理凭据、源码路径、CredentialIdentity、owner、Native session 或 reservation 权限；账号/模型/执行路线标识与原已公开冻结计划一致。

## 身份与状态边界

新准备要求当前 ready Task 的计划版本与 generation 精确匹配，且角色存在于冻结计划。旧记录只按原 key/task/role/revision/generation 回读，不重编译配置、不重新预览，不调用 Controller、Runtime 或额度供应商。缺少 Controller 的草稿宿主仍可准备、读取或封存元数据；原 start 仍按已有合同返回 503。

Task 状态改变、Server/宿主重开或预览被清理后，原条件仍用于核对旧记录。POST 改原身份返回 409；GET 用错误原条件返回 412；不存在的记录、未登记项目、未知 Task 或其他 Task 的记录返回 404。无管理凭据 401，Stage Bearer 403，非法输入/重复 header/额外字段 400，缺 Task If-Match 428，错误方法 405。禁止传入任意 Task/Plan/target/run 以替代可信原记录。

每项目最多一个 prepared/committed 记录，其他请求不得绕过它创建新 pending。封存与实际 StartReservedOnce 使用同一数据库事务保护；已写入 run 的请求无法封存，封存后迟到 start 冲突拒绝。acknowledge 只核对精确原 run_id；即使运行是 unknown，也不能改变 Task/Run 状态、释放 reservation 或生成 StopProof。读取记录不是独立 run 证明；UI 后续仍须核对原 RunView 与当前 Task。

## Native 与权限

NativeStageBridge 只新增上表的精确方法/段数，Task 仍须存在且项目仍登记。原绑定窗口、SDK 来源、当前私有 Source/token、8 秒期限、128 KiB 请求/2 MiB 缓冲响应与 Close 取消边界保持。原 Task 条件与 key 多值原样转送，由 API 拒绝重复；仅 Task start-request 的 GET/POST 新增转送 key，项目 pending GET 不转送。没有通用代理、查询参数、任意 x-*、cookie 或页面 Authorization 通路。

Handler 在处理入口、等待服务端锁后、阻塞读取后与写入前核对 Management/上下文，输出前再次核对。Native 在缓冲后另查当前来源。写入已提交但后续来源失效时可返回 503 而不泄漏回执；记录仍保存，后续授权读取按同一原身份恢复，不能凭错误响应断言请求未发生。

## 复现与证据

在本组件实现工作树，使用 Go1.26.3、Python3、已有离线依赖与 macOS SDK：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/start-api.log test -mod=readonly -tags fusion,nogui -race -count=1 -v \
  ./internal/fusion/api ./internal/fusion/bootstrap ./internal/fusion/store \
  ./internal/fusion/control ./internal/fusion/policy
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

测试使用临时私有 HOME/XDG、合成账号/路线/额度与真实 loopback HTTP/Store/Controller。Native 桥重开测试用可信 Store 构造“已持久意图、尚未调用 Runtime”的崩溃记录，重开后 unknown 保持；不作为真实 Native 进程执行证据。最终 354 顶层/418 子测试 PASS，12 顶层 SKIP、0 FAIL；跳过项为 opt-in 的真实 Native/供应商测试及 helper，不计通过。CLI/GUI/vet exit0。OpenAPI 37 路径/43 操作/106 实际 Handler 样本全部覆盖，新增 10 个响应反例；跨字段身份、权限时序与运行证明由行为测试核对，schema 不代替这些验证。

原 Magpie app.css/app.js/index.html、本组件前全部 Fusion UI/CSS、Controller/Runtime/policy 实现及 schema001–008 均未改。UI 未接入本 API，因此原启动身份跨窗口的用户操作流程仍未交付；实际 Native 点击/像素未验证，真实模型与额度调用为零，Jev off。证据见 [START-JOURNAL-API-01](../work-items/WP-15/START-JOURNAL-API-01/summary.md)。父 WP-15/WP-16 与整体目标仍 in_progress，最终 T01–T60 not_run。
