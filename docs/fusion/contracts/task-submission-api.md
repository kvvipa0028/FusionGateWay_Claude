# 原提交恢复 API 与 Native 通路

WP-15-SUBMISSION-API-01 完成受管理认证的原提交记录准备、读取和解决，以及精确 Native 桥接。本 API 组件未修改 UI；后续[原请求恢复 UI](task-submission-ui.md)已消费这些接口，浏览器恢复通过，实际 Native 恢复窗口操作仍待验证。依赖 [私有提交存储](task-submission-journal.md) 与 [原 Task 提交回执](task-submission-receipt.md)，schema 7 不变。

## 接口与输入

| 路径 | 方法 | 行为 |
| --- | --- | --- |
| `/control/v1/projects/{project_id}/submission` | GET | 读取该项目唯一 prepared/committed 记录；没有记录时 200 `{"submission":null}` |
| 同上 | POST | 从真实、有效的服务端原预览准备私有记录；精确重试读历史，统一 200 |
| `/control/v1/projects/{project_id}/submission/acknowledge` | POST | 只确认已有 committed 原任务回执；确认保存不等于工程验收 |
| `/control/v1/projects/{project_id}/submission/abandon` | POST | 仅未提交的原请求可以原子封存；拒绝此后迟到的原 Task 提交 |

POST 必须是 application/json，body 与既有 SubmitRequest 相同，只含 `preview_id`、`plan_hash`；提供单个原 `Idempotency-Key`。例如合成请求 body：

```json
{"preview_id":"preview-fixture","plan_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
```

该例不能替代真实预览身份。客户端不能提供 CreateRequest、目标、workspace、配置版本、预算、完整冻结计划、准入结果或认证字段；重复/大小写歧义字段、多个 key、未知字段、错误 Content-Type/超大 body 继续拒绝。查询参数中的 key/token 被既有权限边界拒绝。提交 key 是幂等身份，不是认证 Token，也不能授予执行权限。

回复对象只有 `submission` 字段。非 null 记录含 `project_id`、原 `goal`、`key`、`state`、`task_id` 和原 `preview`，preview 沿用既有 DTO，含冻结计划、显式预算、到期时间、配置版本及可选原预设引用。prepared/abandoned 的 task_id 是空字符串；committed/acknowledged 必须有原任务 ID。回复不包含通用内部 CreateRequest、目录或认证材料，no-store/nosniff 保留。

## 准备与重试

新准备仅使用该项目服务端真实预览，拒绝未知、过期、计划 hash 不匹配、其它项目、计划修订预览、当前项目/default stamp 已变化的请求。Store 在事务内再次检查默认 stamp。保存不创建 Task、消耗预算或运行 Runtime。每项目只有一条未解决记录；另一个 preview/key 必须先核对原记录，不能覆盖它。

已有记录通过 Store `LookupSubmissionJournal` 校验精确 project/preview/key/plan hash、完整草稿及其 hash，包括终态。丢失准备回执后，宿主重启、预览清理、配置变化都不改写原记录；相同 POST 读原状态。终态重试不会恢复 pending，也不创建新任务。GET 只读未解决记录，不对旧 Task 猜造记录。

读回记录不恢复丢失的进程内预览。重启后 prepared 请求对原 `/agent/v1/tasks` POST 仍是 409；客户端须明确封存并证明成功，才能重新准备。committed 请求可以使用原 body/key 读同一 Task 回执。Task 后续版本/预算计数仍按原合同；prepare/ack/abandon 不改事件、状态、计划或已用计数。

acknowledge 与 abandon 使用精确原身份，由既有 Store 事务决定合法转移。prepared 不能确认，committed 不能放弃；同动作重复可读回终态。提交/放弃竞态只有一个成功结果。丢失解决回执不能当作成功，也不能换 key；重复原动作核对。404 表示未登记项目或原记录不存在，409 表示原请求冲突/新预览失效，400 表示输入无效，405 表示方法不允许；鉴权/来源失效沿用原 401/403/503 边界。错误不回显目标/key。

Store 通用草稿可不包含显式预算，但它不是 UI 预览。恢复 API 对这种记录返回 409；不会猜造预算、panic 或通过解决释放它，Store owner 仍可按其原合同处理。这不改变旧 Task 提交或 Store 通用接口语义。

## 权限与 Native

所有接口必须通过当前 Management middleware，且路径项目仍登记。读取请求体后、获得 Server 锁后先重查当前 Management，防止等待中的撤销/取消继续修改；Store 读取/事务之后再重查，未授权不发送原记录或存在性细节。来源/文件夹/token 的 ControlHost 核验继续生效，不以恢复绕过真实 Factory/账号/额度准入。

Native 仅开放已登记项目上述准确路径与方法，沿用固定 SDK/本窗口/Origin/URL 检查；拒绝客户端 Authorization、cookie、query 和转发 header。只向自己拥有的 loopback 注入私有 Management，页面不接收 Token。仅允许的 POST 传原 Idempotency-Key，未知路径和方法不透传。HTTP 回复缓冲完后再次核验当前 Source/token/取消状态，撤销则 503，不返回已缓冲记录；此检查也覆盖既有桥接通路。

## 验证与后续

Store 精确读取/终态历史/重开/身份及关闭拒绝，API 原生命周期/新 Server/资格检查/输入冲突/取消撤销，真实 owned HTTP 宿主及 Store 关闭重开→Native 原 prepared/committed 恢复、原请求封存、同 Task 重读、来源撤销和零 Inspect/Resolve，均已验证。真实 Native 窗口操作、浏览器恢复 UI、SIGKILL/硬件断电与真实账号/模型/额度没有新增验证。详细日志见 [SUBMISSION-API-01](../work-items/WP-15/SUBMISSION-API-01/summary.md)。

本项不将服务端/桥接结果算作完整窗口恢复。后续页面已在原 Magpie 工作台 task POST 前准备记录，重开后读取原项目/目标/预览/key，提供同一提交核对与明确解决操作；继续沿用原布局和组件，不改版。运行控制、真实供应商准入、工程闭环和最终 T01–T60 继续待完成，Jev off。

复现需要 Go1.26.3、macOS Xcode SDK、Python3 及离线 checker 所用 PyYAML/jsonschema/referencing。工作树中执行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-api-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/store ./internal/fusion/api ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
python3 scripts/fusion/check-openapi.py \
  --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json \
  --samples docs/fusion/work-items/WP-15/SUBMISSION-API-01/submission-api-samples.json
```

run-go/build-dev 使用临时私有 HOME/XDG 和环境白名单；合成管理材料只属于测试宿主，不读取日常账号/共享 Keychain，不需要发送真实 key。该验证不启动生产账号任务。
