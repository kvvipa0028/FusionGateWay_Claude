# 阶段配置界面合同

组件：WP-16-EDITOR-01，后续增补 MAGPIE-UI-01、TASK-UI-01、SUBMISSION-UI-01、CONTROL-UI-01、QUOTA-UI-01、MAIN-UI-01。五角色绑定合同、Store 与 Management API 保持不变；页面只编辑完整选择及请求服务端预览，不作执行准入判断。

## 页面与权限

现有私有 loopback ControlHost 提供 `/fusion/` 和 `/fusion/index.html`、`main.mjs`、`editor.mjs`、`model.mjs`、`workbench.mjs`、`editor.css`、`app.css`。每项资源经过同一个 Management middleware，以及 Host、Origin、Sec-Fetch-Site、可信私有来源和 token 文件核验。Stage Bearer 不能访问；无管理授权返回 401。来源或私有凭据权限改变后返回 503。只允许 GET/HEAD；未知资源 404、一般查询参数 400、含凭据的查询参数由既有 middleware 返回 403。响应 no-store、nosniff、no-referrer，并限定 CSP 的脚本、样式和连接来源。

页面不接收 key/token 输入，不使用 URL/cookie/localStorage 保存授权。JavaScript fetch 使用同源、credentials=omit、redirect=error。测试中由隔离浏览器 context 注入临时 Management Authorization header；产品 Native GUI 已有[受限鉴权桥](native-stage-ui.md)，基本窗口保存/载入/退出已验收。因此普通浏览器直接打开地址会得到 401，不能通过把 token 放进地址栏来操作。

## 复用 Magpie UI

按用户要求，直接使用原 `internal/gui/assets/app.css`，通过 `magpieassets.Styles` embed 包提供 `/fusion/app.css`；浏览器验证服务内容与原源文件逐字一致。沿用 `.top`、`.brand`、`.seg`、`.view`、`.profiles`、`.list`、`.row`、`.field` 和 `.text` 控件及原深浅色变量；`editor.css` 仅补 `.fusion-stage` 范围内的五角色表单布局和窄窗口排布，不再定义另一套配色、字体或页面框架。

原 Magpie 的 `index.html`、`app.js`、`app.css` 保持原内容。Fusion 通过[原主界面接入](magpie-main-ui.md)直接从原首页源文件提取页头和导航，只增加一个阶段入口；原页的未适配功能显示明确未开放状态。受限 Native 入口及隔离保持不变，不执行原 app.js 的客户端/账号初始化。这不是原有所有 Magpie 页面数据与操作已接入 Fusion ControlHost 的声明。

## 配置行为

- 显示设计与规划、实施与测试、审查与验收三个区域，展开后为 design、implementation、testing、review、acceptance。收起只隐藏控件；合并完整绑定需提示覆盖并由用户确认。
- 全局、项目、本次任务草稿分别保存。本次任务 > 项目 > 全局；inherit 使用较低层已保存的完整绑定。预设必须指定正整数版本，载入历史版本不会自动追踪同名预设头版本；后续[命名预设界面](preset-ui.md)已支持创建与保存新版本。
- locked 选择 route id/revision/model 精确匹配项；失效项保持显示，保存被阻止，不改选第一个模型。换模型后 effort 回到未指定；none、default、explicit 只显示相应登记能力。auto 从空清单开始，由用户逐一批准候选；空清单、重复路线或失效 effort 阻止保存。
- 展示登记账号、billing_path、锁定等级及尚未准入提示；没有调用 quota API，明确显示额度尚未读取。保存草稿不等于可执行，不会推广 route.admitted 或 billing_known。
- 全局/项目 PUT 携带 If-Match。每个配置内容与版本号来自同一 defaults 响应。configured=false/revision=0/layer=null 才使用可信配置响应中的来源基础层；已持久化的层使用 defaults 自身内容。412 保留本地选择并提示重新载入，禁止自动覆盖。
- 保存成功回执必须含可解析层、匹配且安全整数范围的 revision/ETag。断线、不可解析或结构异常的成功回执被视为保存结果未确认；禁用编辑并保留原 URL、body、If-Match，重试原请求。服务端幂等由既有 defaults 合同负责。重新载入会提示丢弃本地草稿并核对服务端。
- 本次任务选择一个必要角色，仅向服务端 POST preview，包含 task 层与可选精确 preset 引用。缺少准入的 route 得到 422，不生成虚假成功计划；页面按 `required_roles` 读取 `bindings[role]`，展示锁定模型或实际批准候选；异常成功响应不显示计划。修改目标、阶段或载入预设隐藏旧预览，必须重新核对。预览后可按[任务工作台合同](task-workbench.md)冻结提交；只提交原 preview_id/plan_hash 和幂等键，保存不启动阶段。

## 本机验证与复现

要求 macOS/arm64、Go 1.26.3、Python 3、Node，以及已有 Playwright 库和 Google Chrome。测试使用仓库外随机私有 0700 目录、0600 合成来源/Management token、临时 HOME/XDG 和新的 Chrome profile，不读取日常账号，不安装浏览器。先在本工作树执行：

```sh
mkdir -p .fusion-dev/implementation
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/task-ui-fixture-build.log \
  test -c -mod=readonly -tags fusion,nogui -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
node --test internal/gui/tests/fusion-editor-model.test.cjs
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  PATH="$HOME/.local/bin:$PATH" node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/stage-editor-bootstrap-check.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/bootstrap ./internal/fusion/api
```

浏览器测试启动真实私有产品 CLI，检查服务资源逐字等于当前源文件，操作页面后直接回读实际 Store API。退出时等待自己启动的宿主停止并删除自己创建的目录；截屏写到 `.fusion-dev/implementation/`。成功预览额外使用 `_test.go` 中的合成 RuntimeFactory 宿主：仅接受 synthetic-ui/fixture 标识的只读项目，默认 Inspect/Resolve 拒绝；后续 CONTROL-UI-01 的显式 test-only hold/success 模式接真实 Store/Controller 与进程内合成执行，不启动 Native/provider，StopProof 与额度为合成值。该旗标只存在测试二进制，产品 CLI 不具备这条准入入口。服务或浏览器不可用时保留失败证据，不能以模型单元测试替代浏览器验证。

## 操作流程与当前边界

Native 配置窗口操作：选择登记项目和配置范围 → 展开需要独立设置的角色 → 指定模型及 effort，或批准 auto 候选 → 保存并核对版本。合并前核对覆盖提示；有冲突时保留草稿，重新载入当前配置后再编辑。切换项目或重新载入需确认丢弃未保存内容。本次任务填写目标、选择一个阶段，点击预览；预览与执行是两个步骤，当前页面可冻结提交并回读任务、计划和预算；这两个操作均不启动阶段。

当前隔离验证可以按照上述测试命令复现交互；不可声称用户已经能在 Native GUI 运行真实任务。任务列表、冻结提交及详情已接入；[阶段启动、暂停/继续/整项取消](task-execution-ui.md)已由 CONTROL-UI-01 接入原详情区；工程流程的下一阶段 Gate、事件/日志、运行计划修订、真实账号/Factory 准入仍待后续组件。[原请求恢复 UI](task-submission-ui.md)已接入，浏览器恢复已验证；实际 Native 恢复窗口验证仍待桌面解锁。最终 T01–T60 状态保持 not_run。

[额度观察区](quota-ui.md)默认折叠，复用原 Magpie 列表/按钮，不修改 CSS；载入只读缓存、查询须手动，未知/过期/未核验与生成准入分开。
