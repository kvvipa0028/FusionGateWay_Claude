# WP-16-MAGPIE-UI-01 · 复用 Magpie UI

基线 `c791ec86ecf17b8746dd81bc930b9b48acb678b9`。本组件已实现并验证；父 WP-16 继续 in_progress，最终 T01–T60 保持 not_run，Jev off。

## 改动

按用户“UI 界面借助原 Magpie，在此基础修改，不要大改”的要求，阶段视图直接加载原 `internal/gui/assets/app.css`，不复制配色或字体。复用原顶栏、导航外观、滚动内容区、Profiles、列表/行、字段和按钮类；五角色表单只保留 `.fusion-stage` 范围内的布局补充。原 Magpie 的 `index.html`、`app.js`、`app.css` 没有修改；未将其日常账号初始化流程引入私有 ControlHost。后续任务工作台继续按这个基础增补。

增加固定 `/fusion/app.css` 资源通路，沿用 Management 和严格本窗口 Native 鉴权，不开放通用 assets 或 legacy API。浏览器逐字核对它来自原样式文件，验证深浅色及窄窗口无横向溢出。

成功预览原来对 `bindings` 作数组遍历，真实服务端则返回角色映射。先用真实私有合成宿主复现，再按 `required_roles` 和 `bindings[role]` 展示锁定模型或批准候选；异常成功响应隐藏预览。修改目标、阶段或载入预设隐藏旧预览。页面继续只提供预览，不提交或启动任务。

验证宿主只存在于 `_test.go`，只接受 synthetic-ui/fixture 标识的只读项目，Inspect/Resolve 始终拒绝，不提供真实执行。产品入口及真实路线准入没有改变。

## 验证

| 项目 | 结果 | 证据 |
| --- | --- | --- |
| 共用样式未接线的 RED | Host / Native 均返回 404；接线后通过 | [RED](magpie-ui-red.log)、[GREEN](magpie-ui-green.log) |
| 成功预览 RED | 真实服务端 200，页面无法显示计划 | [RED](magpie-ui-preview-red3.log) |
| 实际产品 CLI / 私有 Store / Chrome + 合成成功预览 | 10 PASS、0 FAIL；含 locked/auto 预览、异常响应及目标/阶段变更失效 | [最终浏览器](magpie-ui-browser-verified.log) |
| 绑定模型逻辑 | 10 PASS、0 FAIL | [模型日志](magpie-ui-model-final.log) |
| bootstrap race | 31 PASS、129 subtest PASS、0 FAIL、3 SKIP | [Go 日志](magpie-ui-bootstrap-final.log) |
| Go1.26.3 CLI / GUI / full vet | 三项 exit0 | [构建报告](build-results.json) |

三项 SKIP：两项明确需要真实 pinned Native 凭据的测试本轮未运行；合成宿主进程测试在普通套件中按设计跳过，它由浏览器验证显式启动。首次合成宿主测试因缺少私有父目录退出；已修正测试设置、保留前三份启动失败日志，并清理本轮三个失败的临时目录。日志仅去除行尾空白，原日志 SHA256 留在[机器报告](test-results.json)。

浏览器视觉证据：[桌面](desktop.png)、[窄窗口](mobile.png)。这是合成项目页面及当前可见区域，本轮没有重跑实际 Native 窗口或取得 Native 截图。

复现命令与权限边界见[阶段页面合同](../../../contracts/stage-editor.md)。任务提交/启动、手动推进、事件/额度、计划修订工作台及真实账号/Factory 准入继续待实施；本组件通过不代表完整工程闭环验收完成。
