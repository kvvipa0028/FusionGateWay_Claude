# 单阶段任务工作台：冻结提交与回读

组件 WP-16-TASK-UI-01。界面直接复用 Magpie 原 `app.css` 及列表/行/按钮控件，在阶段视图中增补工作台；不引入另一套 UI 框架或配色。当前完成冻结提交、任务发现和完整目标/计划/预算回读，阶段启动及运行控制仍待接入。

## 操作

1. 选择已登记项目，将配置范围改为“本次任务”，指定需要的单个阶段及模型/effort，填写目标。
2. 点击“预览单阶段任务”。只有服务端准入通过才显示冻结角色、具体路线版本、effort、账号/计费、锁定等级、计划 hash、调用/返工上限及有效期。auto 显示全部批准候选，不伪造已经选定的执行模型。
3. 核对后点击“冻结提交任务”。请求只含原 preview_id/plan_hash，带新生成的 Idempotency-Key；不重新编译配置、不调用 start、不查询额度、不启动 Runtime。Task 回执必须为 201，项目、完整目标、ID、revision/generation/state 结构均校验。
4. 提交得到“任务已保存”，新任务初态 ready（待执行）；已有任务重试回执允许反映它的后来状态。保存回执不代表模型运行成功。工作台自动刷新任务列表，并读取已知任务 ID 的详情。
5. 用“刷新任务列表”“读取更早任务”读取固定 32 项分页；每行“查看任务”读取完整目标、当前冻结计划、具体绑定与预算。“重新读取任务”核对新状态。列表目标仅为最多 512 code points 的摘要，详情使用原完整目标。

当前 `fusion-ui` 产品入口仍使用未准入的 OpenControl 草稿宿主，真实路线没有完成 RuntimeFactory 注册；因此预览可以明确返回 422，不能声称用户已经能提交或执行真实模型任务。成功提交路径使用独立的合成 Factory 宿主验证；它只在测试二进制存在，Inspect/Resolve 始终拒绝，不能给真实路线准入。

## 未确认、冲突与过期

首次提交的断线、5xx、异常/不可解析的 2xx 回执，都视为结果未确认。保留原 project/goal/preview/hash/body/key，并锁定项目、配置、目标和新预览；“重试同一任务提交”发送原样请求。此时任务列表和详情仍可只读核对。已经未确认的提交再次得到 4xx，也不能据此认定第一次没有入库，仍保留原请求。没有从相同目标或相同计划猜测 Task ID，也不自动换 key 创建任务。

首次明确 4xx 拒绝可解除提交锁定、丢弃旧预览并保留草稿供核对；409 提示预览失效或提交冲突。修改模型、目标、阶段、配置范围或载入预设都会使旧预览失效。客户端到期禁止发起新的提交；服务端仍独立执行配置版本/过期校验。已经未确认的原请求允许继续重试，不将“到期”当作第一次未提交的证明。

本组件的未确认原请求仅保留在当前窗口内，beforeunload 提示不是持久恢复保证。关闭/重载窗口后尚不能恢复该原请求；服务端已由 [SUBMISSION-RECEIPT-01](task-submission-receipt.md) 将已提交的 preview_id/plan_hash/key 与原任务关联持久化，预览被清理或宿主重启后，仍持有原请求的客户端可通过原 POST 对账。所属项目须继续登记且管理/来源权限须有效；只有未提交的进程内预览仍在重启后失效。UI 没有将授权或任务输入写入 localStorage/sessionStorage，不将重新提交或任务列表猜测冒充对账。窗口关闭后的原请求恢复仍须后续实现，完整 WP-16/M4 验收尚未完成。

## 读取一致性与权限

工作台只接受当前项目的任务与有效资源 ETag。Task/Plan/Budget 分别读取，再读取一次 Task；Task 条件变化时最多重新读取一轮，plan revision/ETag 必须匹配。预算是单独读取的服务端计数，不宣称这些接口构成原子的跨资源快照。异常详情会清空旧内容并提示重新读取。

列表校验页大小、项目、ID 去重、目标截断标记及 next_before 的页尾关系。刷新列表和选择项目/任务均有独立请求序号；旧项目或旧任务的迟到响应不能替换当前显示。目标、模型、状态和服务端字段通过 textContent/DOM 展示，不作为 HTML 或指令执行。

Native 桥新增固定 POST `/agent/v1/tasks`、GET `/agent/v1/tasks/{id}`、GET `/control/v1/tasks/{id}/plan`、`budget`；新增固定工作台模块资源，不开放通用 assets。详情先核验本窗口及私有宿主，再核对 Task 所属项目在可信 Source 中；未知和未登记项目的任务同为 404。Idempotency-Key 只转送冻结提交；缺少/重复 header 仍由原 API 拒绝。start/pause/cancel/events/quota 等不因本组件扩大权限。来源/token 撤销、响应大小/期限和关闭取消沿用原桥保护。

## 复现与证据

本机 macOS/arm64，需要 Go1.26.3、Xcode SDK、Python3、Node、已有 Chrome 和 Playwright。使用当前隔离实施工作树运行：

```sh
mkdir -p .fusion-dev/implementation
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/task-ui-fixture-build.log \
  test -c -mod=readonly -tags fusion,nogui -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  PATH="$HOME/.local/bin:$PATH" node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/task-ui-host-api.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/bootstrap ./internal/fusion/api
```

测试使用私有临时目录与隔离环境，浏览器另起临时 profile，只允许访问本测试宿主；自己启动的宿主退出后清理对应目录，Management token 不进入日志。没有读取日常认证或执行真实 provider 调用。最终结果、失败修正及截图见 [TASK-UI-01](../work-items/WP-16/TASK-UI-01/summary.md)。Native 桥/HTTP 和 GUI 编译已有验证，本组件没有重跑实际 Native 窗口，浏览器截图不替代 Native 视觉证据。
