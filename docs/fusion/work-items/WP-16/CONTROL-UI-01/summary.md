# WP-16-CONTROL-UI-01 · 原任务详情区运行控制

状态：本组件完成；父 WP-16 和整体实施目标仍为 in_progress，最终 T01–T60 not_run。产品真实供应商执行与实际 Native 运行按钮未验证。

## 修改与 UI 范围

原任务详情区增加冻结角色选择、明确启动、暂停、继续、整项取消及原运行请求重试，沿用 Magpie `.profiles`/`.text` 等控件和已有换行布局。原 Magpie app.css/app.js/index.html、Fusion CSS 均未改，未引入新框架、导航或配色。详见[运行控制合同](../../../contracts/task-execution-ui.md)。

原启动 role/body/key/完整 Task If-Match 在未知时保留，独立同 run 和当前 Task 读取核对后才清除。错误成功 run ID 不作为执行证明；412 需重新读取，结构化 409 不丢弃已接受意图，pause/continue/cancel 不自动重发，继续不自动启动。run 读取失败保留已证实 Task 条件。切换项目同步清空旧详情，加载期间旧任务控件隐藏且禁用。成功阶段与整项验收分开，取消不表示回滚或退款。

仅 `_test.go` 的显式 hold/success fixture 能接进程内合成执行；原默认 Inspect/Resolve 关闭。真实 HTTP/Store/Controller、私有工作区副本参与测试，Runtime/session/额度/StopProof 为合成值，不启动真实 Native/provider。产品 Factory、API、Controller、Store/schema、Native 桥和准入均未修改。

## 验证及失败说明

有效 RED：原按钮不存在；进一步替换成功回执 run ID 的测试发现清除原 key 过早。修复为独立原 run/当前 Task 证明后 GREEN。完整浏览器 35/35 PASS、0 FAIL/0 SKIP；随后只复用既有 task-submit 换行类修正窄窗口角色标签，全部新增 8/8 运行控制测试再通过；最终另加项目切换加载检查 1/1 PASS。没有把这三次重复检查相加作为不同测试总数。桌面/390px 截图已查看，无横向溢出且角色标签可读。

保留 control-ui-browser-final.log 的中间失败：测试启动时 CLI 仍为前一构建，资源逐字比较拒绝旧 bundle；另一个用例在自动回读完成前断言状态。完成构建后重跑，并等待实际回读完成，没有削弱断言。项目切换中间失败 control-ui-project-verified.log 是测试通过默认可见 role locator 查找已正确隐藏的按钮导致超时；改为检查隐藏状态与 DOM 按钮 disabled，产品无需变更。两个进程均自然返回 exit1，未成功执行任何杀进程操作，最终检查 exit0。

Targeted Go race：45 顶层/100 子测试 PASS，0 FAIL/0 SKIP。Go1.26.3 CLI/GUI 构建、tagged 全包 vet exit0，构建发生在最终 HTML 之后。JSON/source/packet hashes、Markdown links、node/gofmt/diff、私有 key byte exclusion、graph 记录见 validation.json/artifacts.json。graph 14053 nodes/125250 edges，UI 资源/测试脚本按既有规则排除。

当前原启动身份仅在窗口内存中，关闭后恢复仍未实现；Task 提交的持久恢复不是 start 恢复。没有 SSE/自动轮询、额度面板、计划修订、checkpoint 或工程阶段 Gate；没有放宽产品真实准入，OpenControl 仍 execution_enabled=false，控制可能返回 503。实际 Native 运行/恢复窗口仍未验证，浏览器截图不代替它；零真实模型调用，Jev off。最终 whole-branch review 留待完整目标完成。

## 复现与清理

在当前实施工作树，需要预装 Go1.26.3、Python3、Xcode SDK、Node、Playwright 和 Google Chrome；module/cache 已就绪。以下隔离 runner 使用白名单与临时 HOME/XDG，不读取日常认证。浏览器另建私有 profile/来源/token，测试后关闭自己创建的宿主并删除对应临时目录。

```sh
mkdir -p .fusion-dev/implementation
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/control-ui-fixture-recheck.log \
  test -c -mod=readonly -tags fusion,nogui -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  PATH="$HOME/.local/bin:$PATH" node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/control-ui-go-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/bootstrap ./internal/fusion/api ./internal/fusion/store \
  -run '^Test(NativeStage|NativeTask|ControlHostStageUI|SubmissionJournal|SubmissionReceipt)'
```
