# Magpie 原主界面接入

组件：WP-16-MAIN-UI-01。遵从用户要求，复用 Magpie 原页头、鸟形标识、八个导航入口、按钮和 app.css，只增加“阶段模型配置”入口及受限导航接线。

`magpieassets.MainShell` 直接 embed 原 `internal/gui/assets/index.html`；`fusionassets.mainPage` 从该源文件提取页头并插入现有 Fusion 页面模板。没有另维护一份复制的原页头，也没有改动原 index.html、app.js、i18n.js 或六份 CSS。原 `.top`、`.brand`、`.seg`、`.actions`、`.view` 和深浅色变量保持原样。新增 main.mjs 沿用原 fitTop 的逐级缩紧与窄屏导航规则；editor.css 仅增加原拖动属性的四行外置规则，避免放宽 CSP。

首页仍使用 `/fusion/` 或 `/fusion/index.html`，主框架 URL 限制不变。Native bridge 仅增加精确 `/fusion/main.mjs` GET/HEAD；不开放 `/`、boot.js、app.js、旧 `/api/*`、runtime.js、查询参数或外部页面。没有向页面注入管理 token 或供应商凭据。原页头或模板锚点漂移时拒绝生成页面，不退回未受控原首页。

## 操作

1. 按 [Native UI 说明](native-stage-ui.md)构建并从私有项目来源启动 `fusion-ui`。原窗口身份和受限鉴权桥不变；新窗口标题为 `Fusion Gateway Development · Magpie`。
2. “阶段模型配置”默认打开三个选择区，展开独立测试/验收设置；保存、预设、预览、冻结提交、任务记录及现有运行/人工决定控件沿用原行为。工作台位于同一面板下方。
3. 点击任一原 Magpie 导航入口，会在同一原布局中显示未开放原因。点击“返回阶段模型配置”保留当前项目、草稿、预览、任务和滚动位置；导航本身不保存配置、不启动阶段、不调用供应商。
4. 原 Settings 图标显示范围说明。同步、更新、打开其他窗口和页内退出按钮保持禁用；关闭此开发窗口仍使用系统原生窗口关闭按钮。不是因为模型选择而获得新的客户端配置、插件或账号权限。

## 已实现与边界

已接入原主界面框架、导航和实际 Fusion 面板，浏览器验证九个入口、未开放说明、草稿保持、无旧 API 调用、桌面/390px 和原深浅色样式。Provider 登录、原 Gateway、原客户端 Sessions/Library、Plugins、原设置与更新等页面的数据和操作尚未适配受控项目，因此没有声明原 Magpie 所有页面已功能接通。

完整 79 项现有及新增浏览器流程通过；另两项稳定截图复验等待原 view-in 动画真实结束。CLI/GUI 编译与 vet 通过，Management 及 SDK 窗口鉴权、源撤销和固定导航回归通过。额外全量原 GUI 测试仍有基线失败，详见 [证据](../work-items/WP-16/MAIN-UI-01/summary.md)；不能写成全量 GUI suite 通过。

本轮 CUA 再次返回 Mac locked，未取得此版本实际 Wails 点击或像素证据。浏览器截图是合成配置下的真实 Chromium 渲染，不是供应商调用、供应商准入或原生窗口验收。整体 WP-16 及工程最终验收继续进行。
