# 任务控制 API 与 Controller

内部 Handler 已实现此合同；[fusion-control 草稿服务](control-host.md) 已注册独立 listener，执行 Controller/GUI 尚未注册。所有请求走同一 Management middleware，stage/query credential 不能调用。可信 SetController 仅登记一次，并且 Controller 与 Server 必须使用同一 Store；客户端不能提供 owner、Native session、账号、workspace、Runtime 参数或 StopProof。

| 方法/路径 | 条件、请求与返回 |
|---|---|
| POST `/control/v1/tasks/{task_id}/pause` | 单个 strong Task If-Match，JSON `{}`。空闲可靠暂停或当前 paused 重读为 200；提交 pausing 意图为 202 |
| POST `/control/v1/tasks/{task_id}/cancel` | 单个 strong Task If-Match，JSON `{}`。空闲可靠取消/当前 cancelled 重读为 200；提交 cancelling 意图为 202，实际停止未确认的重复意图可为安全 receipt / 409 |
| POST `/control/v1/tasks/{task_id}/continue` | 单个 strong Task If-Match，JSON `{}`。仅解除已可靠 paused 的派单冻结为 200；当前 ready 重读不重复事件 |

If-Match 来自 GET Task 的 `"p{plan_revision}-g{generation}-{state}"`，不是 plan 的整数 ETag。Store 在实际事务内比对全部字段；stale 条件为 412，缺失 428，weak/通配/重复/多值/前导零/非法/溢出为 400。请求体的任何额外字段、null、非 object、尾随 JSON 和 encoding 均拒绝。沿用 1 MiB/UTF-8/字段唯一性限制。无/错 Management 为 401，stage/query credential 和不可信 Origin 拒绝，Controller 未登记/已关闭为 503。管理身份在入口及 body/控制预检查后再核对，读取 body 时撤销会阻止写入。

返回 TaskControlReply 包含当前 Task、可选安全 RunView、changed，失败可带固定 error。changed 表示本请求是否写入新控制转换，不是 task 已完成的结论；RunView 不包含 owner、lease、Native session 或原始输出。X-Fusion-Task-ETag 与 reply.task 来自同一次读取，Location 指向 GET Task；不把 Task ETag 声称为整个回复资源的 ETag。

暂停或取消的停止可能在回复前已经完成：202 表示该事务接受了 pausing/cancelling 意图，reply.task 仍为当前读取的状态，可以已经是 needs_review 或 cancelled。页面必须依 Task/state/error 和后续持久事件显示实际进度。HTTP 断开不撤销已提交暂停；重连先读 Task 和事件，用当前条件重试。旧条件会得到 412，不通过重复 POST 或旧 tag 再启动执行。此控制不需要新的 Idempotency-Key；同一当前 paused 的 pause/当前 ready 的 continue 是只读，没有新事件。

## 所有权、实际停止和错误保留

Controller.PauseAuthorized/ContinueAuthorized/CancelTaskAuthorized 重查同一 issuer 的本地 ManagementCurrent，TaskVersion CAS 由 Store 独立强制。运行 pause 在 Store 提交 cancelling/pausing 后，只取消本 Controller jobs 中同 task/run/generation 的 owned lifetime/Handle。其他 owner 的活跃 run 被 fencing 拒绝，不自动接管；未知任务为 404。

如果 Start 正在返回 Handle，Pause 可先取消已登记的 owned lifetime；Handle 后到时，Controller.Start 也必须向精确 Handle 交付 Cancel，即使 backend 在启动期间忽略 Context。只有实际 Wait、正确 Native identity/descendant StopProof、同 Adapter 的 Release 都通过，才允许释放预留。没有 Handle/owner 或停止无法核验时保留 intent/held，不推定已停。已有 job 结束但 proof/release 失败的重复 pause 返回 409 和安全 TaskControlReply，不能丢弃已存在的意图或输出原始 Runtime 错误。

继续只解除可靠暂停的派单冻结，不能隐式调用 Native Start/Resume、不重置调用/返工预算、不选择新模型或重新解释旧快照。暂停因 cancelled/interrupted/unknown 或其他副作用未核对时为 needs_review/409，仍需 WP-24 的明确检查点/工作区核对与对应 Runtime 恢复能力。整项任务已可信停止的 cancelled 不通过普通 continue 恢复；与既有不适用 Task 控制条件一致，返回 412，不授予新执行。缺少准确输入的通用 Native Resume 继续 unsupported；明确成功归档的恢复已接入 [task-resume-api.md](task-resume-api.md)，不改变 Continue 的派单语义。新阶段启动继续独立验证当前 route/权限/额度与被冻结的 Target；旧 StartIdentity 只能读回同一个 run，不重放。

Store 的 pausing→paused/needs_review、空闲 generation 防 ABA 与完整历史停止检查见 [task-pause-store.md](task-pause-store.md)。未改变 schema 5、CreateRequest/StartIdentity hash、预算或现有公开 start/cancel 合同；本项新增两条 control 操作。Start 对晚到 Handle 的取消同样修复 owned Close/Cancel lifetime 已失效的情况。

## 整项任务取消

CANCEL-API-01 新增 `/control/v1/tasks/{task_id}/cancel`；现有 `/runs/{run_id}/cancel` 仍只指向单阶段。Path 结构区分两者，不能借整项控制传入任意 run/owner。Controller.CancelTaskAuthorized 在预检查后重查 Management，调用 Store.CancelTask，提交后只取消同 task/run/generation 的 owned job，沿用晚到 Handle 取消和实际 Wait/StopProof/release。

空闲或已暂停且全历史已停止的 Task 直接 cancelled，不调用 Resolve/Runtime；活动或协议已终态 held 的执行先 cancelling。任务取消可取代 pausing 意图，不能恢复已取消任务。缺 Handle/未知执行/不同 owner 均不接管；失效 proof 或 release 保留 held，重复取消返回 409 和安全 receipt，不输出原始错误。Task 刚写入 cancelling 后进程可能已完成，202 仍表示接受意图；GET Task/事件才提供后续状态。详见 [task-cancel-store.md](task-cancel-store.md)。Cancelled 不表示文件变更已回滚、预算退款或验收通过。

## 验证与复核

PAUSE-API-01 历史新增 6 个 Controller 和 6 个 API 测试通过，覆盖只读控制、实际本机 HTTP、当前 Task/header、旧启动重试、owner/完整版本、失效 proof/释放失败、晚到 Handle、body/header/Origin/query/凭据、body 中撤销和不丢失意图的固定错误。晚到 Handle 的问题实际 RED 后修复为 GREEN；没有削弱已有断言。API fixture 增加 no_stop 模式，仅用于证明 held/error 边界，默认真实 stop fixture 保持原断言。

新增固定 Claude Code 2.1.287 的实际 Native inflight 暂停测试：Controller→Store→owned Cancel→真正 Wait/StopProof/Release→needs_review，保持一次合成 HTTP/Permit，不退款或重试；普通 continue 与旧 start 不能重跑。连同既有成功 Native 回归，显式执行 2 PASS。真实模型/额度查询为 0；合成 account/route/billing/quota 不能作为真实账号准入。此验证经过 Controller，HTTP 鉴权由独立 API 测试覆盖，不能宣称已完成产品 listener/GUI 或真实账号 smoke。

CANCEL-API-01 新增 6 个 Controller、5 个 API、1 个 Native 测试：空闲/paused 不启动，owned 停止/旧映射只读，不可信 stop/释放失败保留 held，owner/条件/管理撤销，晚到 Handle，真实 loopback HTTP，越权 body/header，未知/no-handle 固定拒绝和 409 receipt。实际固定 Native TaskCancel 经 Controller/Store/Adapter/CallGate，在合成上游 HTTP 正在等待时取消，真正 Wait/StopProof/release 后为 cancelled；一次 HTTP/Permit，不退款/重放，普通 continue 拒绝。当前三个固定 Controller Native 场景显式 3 PASS；真实账户/计费/额度准入仍不由 fixtures 证明。

CANCEL-API-01 时 full Fusion tagged race **352 PASS / 10 SKIP / 0 FAIL**，CLI/GUI build 和全仓 tagged vet exit 0。十项 skip 为七个显式 Native（独立执行其中三个 Controller Native）、一个真实额度 opt-in、两个 helper。OpenAPI 当前 **24 paths / 28 operations / 43 schemas**，49 个实际 Handler 样本覆盖成功操作，另有六个越权字段 schema 反例与暂停/取消不确定意图 409 样本。历史 PAUSE-API-01 的 331/9、23/27/44 和 2 Native PASS 保留在其证据包，不作为当前计数。后续草稿 bootstrap 回归为365 PASS / 10 SKIP，见 [control-host.md](control-host.md)；本项未重跑真实 Native。

在仓库根目录执行下面的独立 HOME/XDG 复核，不读取日常认证。需预先准备 Go 1.26.3、公开 module/cache、Xcode SDK 和已固定的 Claude Code 2.1.287：

```sh
fusion_test_root=$(mktemp -d)
fusion_sdk=$(xcrun --show-sdk-path)
fusion_claude_path="$HOME/.local/share/claude/versions/2.1.287"
fusion_go() {
  env -i PATH="$HOME/.local/bin:/usr/bin:/bin" \
    HOME="$fusion_test_root" XDG_CONFIG_HOME="$fusion_test_root/config" \
    XDG_CACHE_HOME="$fusion_test_root/cache" XDG_DATA_HOME="$fusion_test_root/data" \
    TMPDIR="$fusion_test_root" GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
    GOMODCACHE="$HOME/go/pkg/mod" GOCACHE="$HOME/Library/Caches/go-build" \
    CGO_ENABLED=1 CC=/usr/bin/clang CXX=/usr/bin/clang++ SDKROOT="$fusion_sdk" \
    CGO_CFLAGS="-O2 -g -isysroot $fusion_sdk" \
    CGO_CXXFLAGS="-O2 -g -isysroot $fusion_sdk" CGO_LDFLAGS="-isysroot $fusion_sdk" \
    MAGPIE_NO_STATS=1 DO_NOT_TRACK=1 go "$@"
}
fusion_go test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/control -run '^TestControllerPause'
fusion_go test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/control -run '^TestControllerCancelTask'
fusion_go test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/api -run '^TestTaskCancelAPI'
fusion_go test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/control -run '^TestControllerPinnedNative' -args \
  -fusion-control-native-claude="$fusion_claude_path"
```

依次预期 6 / 6 / 5 / 3 PASS；此 flag 仅运行合成 Native fixtures，不加载真实 key。失败时保留日志和该私有临时目录，核对字段/状态/proof，不关闭断言或改用浮动 CLI。命令只创建本人临时测试数据；完成后可按本人清理策略处理该目录，不宽泛删除其他临时目录。API capture/checker 的同类隔离命令见 [openapi-verification.md](openapi-verification.md)。

本机二进制 SHA256 应为 `6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea`，Adapter 会验证 pin，不支持替换成任意可执行文件。Go 1.26.3 与公开 module/cache 需预先准备，不能带入日常 HOME 或真实认证。完整日志、样本与 source/packet hash 见 [CANCEL-API-01](../work-items/WP-15/CANCEL-API-01/summary.md)；暂停历史证据见 [PAUSE-API-01](../work-items/WP-15/PAUSE-API-01/summary.md)。Jev off。产品接线、Native 恢复/副作用核对及最终 Gate 继续实施。
