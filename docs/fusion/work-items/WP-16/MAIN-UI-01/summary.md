# WP-16 · MAIN-UI-01

已直接复用 Magpie 原首页源文件的页头、鸟形标识、八个导航入口和 actions，增加一个“阶段模型配置”入口，并保留原 CSS 与现有 Fusion 表单/工作台。不是复制另做页头，不引入 UI 框架，也没有重新设计配色和页面结构。

基线：`923c08d57c14262b7c277efeae5105ce62dc2406`。原 `index.html`、`app.js`、`i18n.js` 和六份 CSS 与固定上游 `1a50db1a8afd0849df2853f92a47da9d5e2f2cc9` 逐字相同。只在原资源共享包增加主模板 embed，在现有 Fusion 页面插入原页头；四行拖动规则外置到 Fusion CSS，严格 CSP 不变。

新增 main.mjs 沿用原 fitTop 规则、原导航按钮和布局类。任一原入口展示明确未开放原因，返回 Fusion 不丢失草稿，也不保存或启动任务。原 Settings 展示说明，更新/同步/新窗口/页内退出禁用。原页面的数据和操作仍待适配受控项目与账号；本工作项只交付真实主框架和 Fusion 消费者，没有声称全部原页面功能接通。

## 验证

| 检查 | 结果与边界 |
| --- | --- |
| 原页头未复用的实际 RED | [main-red.log](main-red.log)；原鸟形标识缺失，真实行为失败 |
| 新页面、模板漂移失败关闭、固定资源 | 三项通过；[targeted.log](targeted.log) |
| Management / Native bridge targeted | 合计 16 个顶层测试通过，含新 main.mjs 的无授权、错误授权、窗口、源撤销和 HEAD/资源约束 |
| Bootstrap 全模块 race | 64 顶层通过 / 5 明确跳过，0 失败；[日志](bootstrap-regression.log)。未提供固定 Native 参数的场景按原规则跳过，未伪造供应商准入 |
| Objective-C 固定导航与拒绝旧入口 | 3 项通过；[native-navigation.log](native-navigation.log) |
| 当前浏览器全部流程 | 79/79 通过，0 失败/跳过；[browser-final.log](browser-final.log) |
| 稳定截图补验 | 两项通过，实际等待原 view-in 动画完成后截图；[browser-main-stable.log](browser-main-stable.log) |
| Go 1.26.3 CLI/GUI/vet | 全部 exit0；[build-results.json](build-results.json) |
| 额外全量旧 GUI suite | 33 项失败，与临时归档基线的失败集合完全一致；[当前日志](gui-regression.log)、[基线日志](gui-baseline.log)。这些旧 Settings/API/更新/导出等测试假设在 Fusion 隔离构建中被拒绝的旧路径，并非本次新增失败，也未削弱保护或修改测试以掩盖失败；不能称此全套通过 |
| 实际 Wails 操作 | 本轮 CUA 返回 Mac locked，未验证此版本实际点击/像素 |

首次浏览器运行仍使用旧合成 test host，其资产与当前源码不符，断言正确拒绝；重新编译 `.fusion-dev/task-ui-fixture` 后通过。[旧宿主失败](browser-stale-fixture.log)保留。第一次新增导航测试使用了组名“设计与规划”作为角色 label，实际可访问名称是“设计”，修正测试定位后通过；没有改变产品或放宽断言。[失败](browser-main.log)、[修正](browser-main2.log)。动画首帧截图曾暂时透明，稳定截图等待真实动画完成，保留原动画。

浏览器是当前真实 ControlHost/合成路线配置与 Chromium 渲染；不代表供应商调用、Gate A、原生 Wails 像素或最终五阶段工程验收。本轮没有供应商或 Jev 调用；私有 key 只用于本地静默检查，确认没有进入本次源码、文档或日志。旧窗口身份、管理权限及主框架规范 URL 保持不变。

## 重现

在 implementation 工作树运行，日志目录必须先创建；使用临时 HOME/XDG 的 runner：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/main-ui/fixture-build.log test -mod=readonly -tags fusion,nogui \
 -c -o "$PWD/.fusion-dev/task-ui-fixture" ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
 node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
```

更多操作与权限边界见[合同](../../../contracts/magpie-main-ui.md)。日志归档仅去掉行尾空白，原 scratch 日志及[原始 SHA256](raw-log-hashes.json)保留，不改变失败记录。

[桌面浅色截图](desktop-light.png)、[桌面深色截图](desktop-dark.png)、[390px 浅色截图](mobile-light.png)、[390px 深色截图](mobile-dark.png)。静态页面没有凭据输入；未开放提示通过 textContent 插入。

WP-16 与整体目标仍 in_progress，原页面功能、实际供应商准入、原生窗口、本机真实工程及最终验收仍需继续。
