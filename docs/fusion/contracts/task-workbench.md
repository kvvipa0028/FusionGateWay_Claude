# 单阶段任务工作台：冻结提交与回读

组件 WP-16-TASK-UI-01，后续增补 SUBMISSION-UI-01、CONTROL-UI-01。界面直接复用 Magpie 原 `app.css` 及列表/行/按钮控件，在阶段视图中增补工作台；不引入另一套 UI 框架或配色。当前完成冻结提交、任务发现和完整目标/计划/预算回读；[运行控制页面](task-execution-ui.md)已在同一详情区接入明确阶段启动、暂停、继续与整项取消。真实供应商准入和实际 Native 运行操作仍未验证。

## 操作

1. 选择已登记项目，将配置范围改为“本次任务”，指定需要的单个阶段及模型/effort，填写目标。
2. 点击“预览单阶段任务”。只有服务端准入通过才显示冻结角色、具体路线版本、effort、账号/计费、锁定等级、计划 hash、调用/返工上限及有效期。auto 显示全部批准候选，不伪造已经选定的执行模型。
3. 核对后点击“冻结提交任务”。先用原 preview_id/plan_hash 与新生成的 Idempotency-Key 持久准备原请求，验证完整原目标/计划/预算；准备证明成功后才以同 body/key 发送 Task POST。不重新编译配置、不调用 start、不查询额度、不启动 Runtime。Task 回执必须为 201，项目、完整目标、ID、revision/generation/state 结构均校验。
4. 有效 201 Task 回执后，用同 body/key 确认原记录；只有精确 acknowledged 回执与 Task ID 一致才显示“任务已保存”，新任务初态 ready（待执行）；已有任务重试回执允许反映它的后来状态。保存回执不代表模型运行成功。工作台自动刷新任务列表，并读取已知任务 ID 的详情。
5. 用“刷新任务列表”“读取更早任务”读取固定 32 项分页；每行“查看任务”读取完整目标、当前冻结计划、具体绑定与预算。“重新读取任务”核对新状态。列表目标仅为最多 512 code points 的摘要，详情使用原完整目标。

当前 `fusion-ui` 产品入口仍使用未准入的 OpenControl 草稿宿主，真实路线没有完成 RuntimeFactory 注册；因此预览可以明确返回 422，不能声称用户已经能提交或执行真实模型任务。成功提交路径使用独立的合成 Factory 宿主验证；它只在测试二进制存在，Inspect/Resolve 始终拒绝，不能给真实路线准入。

## 未确认、冲突与过期

准备/提交/保存确认的断线、5xx 或异常成功回执保留原身份，并锁定项目、配置、目标和新预览。恢复区显示原冻结目标、计划、完整预算限制及 key，不从上方当前配置重新编译；Task ID 须由原记录独立证明，尚待核对的 Task 回执单独标注。任务列表和详情仍可只读读取。

已持久记录在新窗口启动时恢复，依次检查受信任的已登记项目，选择第一个存在未解决记录的项目；不自动 Task POST 或解决。用户可重新核对、重试同请求、或明确放弃未提交请求。保存确认未知只重试确认，封存未知只核对或重试封存；只有精确成功证明才解除锁定。详情见[原请求恢复 UI](task-submission-ui.md)。

首次准备明确 4xx 且此前没有未知结果/Task POST 时可以丢弃该未提交预览；409 随后读取项目原记录，防止忽略另一窗口提交。准备本身未持久成功时不会发送 Task POST；beforeunload 只是关闭提示，不保证未保存预览/目标恢复。UI 不写 localStorage/sessionStorage。宿主重启后的 prepared 请求不能恢复已丢失的进程内预览，只能核对或明确封存；committed 请求读同 Task。所属项目及管理/来源权限须继续有效。

## 读取一致性与权限

工作台只接受当前项目的任务与有效资源 ETag。Task/Plan/Budget 分别读取，再读取一次 Task；Task 条件变化时最多重新读取一轮，plan revision/ETag 必须匹配。预算是单独读取的服务端计数，不宣称这些接口构成原子的跨资源快照。异常详情会清空旧内容并提示重新读取。

列表校验页大小、项目、ID 去重、目标截断标记及 next_before 的页尾关系。刷新列表和选择项目/任务均有独立请求序号；旧项目或旧任务的迟到响应不能替换当前显示。目标、模型、状态和服务端字段通过 textContent/DOM 展示，不作为 HTML 或指令执行。

Native 桥新增固定 POST `/agent/v1/tasks`、GET `/agent/v1/tasks/{id}`、GET `/control/v1/tasks/{id}/plan`、`budget`；新增固定工作台模块资源，不开放通用 assets。详情先核验本窗口及私有宿主，再核对 Task 所属项目在可信 Source 中；未知和未登记项目的任务同为 404。Idempotency-Key 只转送冻结提交和受限原提交准备/确认/封存路径；缺少/重复 header 仍由原 API 拒绝。后续[运行控制桥](native-task-control.md)已开放受限 start/pause/continue/cancel 和 run 读取/取消，现由[运行控制页面](task-execution-ui.md)消费其中的阶段启动、Task 暂停/继续/取消和 run 读取；run 单独取消没有新增页面按钮，events/quota 通路继续待接入。来源/token 撤销、响应大小/期限和关闭取消沿用原桥保护。

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

测试使用私有临时目录与隔离环境，浏览器另起临时 profile，只允许访问本测试宿主；自己启动的宿主退出后清理对应目录，Management token 不进入日志。没有读取日常认证或执行真实 provider 调用。初始结果、失败修正及截图见 [TASK-UI-01](../work-items/WP-16/TASK-UI-01/summary.md)。Native 桥/HTTP 和 GUI 编译已有验证，本组件没有重跑实际 Native 窗口，浏览器截图不替代 Native 视觉证据。

当前恢复 UI 的最终回归与截图见 [SUBMISSION-UI-01](../work-items/WP-16/SUBMISSION-UI-01/summary.md)。Go/API/Native DTO 与样式未在本 UI 组件修改。
