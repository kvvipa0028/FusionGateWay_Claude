# WP-16-EDITOR-01 · 阶段配置页面

基线 `91c799cc2fc88a66686e3233a1d5b91abbc5983e`。组件已实现并验证，父 WP-16 为 in_progress，最终 T01–T60 保持 not_run。

## 改动与行为

新增独立 HTML/CSS/ES modules 和轻量 Go embed 包，沿用现有界面技术与配色。现有私有 ControlHost 下提供 Management 保护的 `/fusion/`；原 API、Native Adapter、Store 格式与日常客户端配置不变。

- 三个区域展开五角色，支持 locked/auto/inherit；收起保留独立 testing/acceptance；显式共用先提示覆盖。
- 全局/项目/本次草稿相互独立，预设载入明确历史版本。模型精确匹配 route/model/revision；删除项与未知 effort 阻止保存，禁止首项回退或猜测默认档位。显示未准入、锁定等级及额度尚未读取。
- 实际 PUT defaults + If-Match，保存和页面重载回读真实 Store；412 保留本地草稿。断线或异常成功回执保留原请求并冻结编辑，重试相同 body/tag，不重复递增版本。
- 修复并验证配置响应与 defaults 响应时序不同导致旧层配新 ETag 的覆盖风险：已配置层只从自身 defaults 响应读取内容/版本。未配置的 revision0/null 层使用可信来源基础层。
- 本次任务只请求一个必要角色的服务端 preview。fixture route 未准入时真实返回 422，不生成成功计划，不启动 Native。
- 外部模型字符串通过 textContent/DOM API 展示，`<svg>` 不形成 SVG 元素；无 innerHTML、客户端凭据输入、URL token、cookie 或本地存储授权。CSP 和来源核验保持关闭式权限边界。

详见[界面合同与本机操作步骤](../../../contracts/stage-editor.md)。

## 验证证据

| 验证 | 最终结果 | 证据 |
| --- | --- | --- |
| 模型/分组/绑定验证 | 10 PASS，0 FAIL | [模型日志](stage-editor-model-final.log) |
| 实际私有 CLI + Store API + Chrome 页面 | 6 PASS，0 FAIL | [浏览器日志](stage-editor-browser-verified.log) |
| bootstrap + API race 回归 | 119 PASS、2 Native SKIP、148 subtest PASS，0 FAIL | [Go 日志](stage-editor-bootstrap-final.log) |
| Go1.26.3 Fusion CLI/GUI 编译、全模块 vet | 三项 exit0 | [构建报告](build-results.json) |
| 当前代码索引刷新 | 13904 nodes / 123174 edges | [结果](test-results.json) |

浏览器流程覆盖独立角色保存/重载、合并确认、两窗口冲突、真实提交后丢失回执的相同请求重试、精确旧预设版本、空 auto 候选/明确批准、单角色预览拒绝、旧 configuration 与新 defaults 交错、异常 2xx 回执、失效模型保持显示、换模型不猜 effort、全局与项目草稿隔离。每次使用临时私有项目/Management token 和新 Chrome profile，服务资源逐字比对当前源文件；服务退出及临时目录清理有断言。外部来源请求被测试 context 拒绝。没有使用真实供应商 key 发起生成。

实际 HTTP 权限验证覆盖全部资源的未授权、错误授权、Stage Bearer、恶意 Host/Origin/Sec-Fetch-Site；来源改变或 token 权限改变后资源返回 503。原产品 API 仍经过现有 Management handler。

已人工查看 [1140px 桌面截图](stage-editor-desktop.png) 与 [390px 窄屏截图](stage-editor-mobile.png)；窄屏无水平溢出，保存栏使用正常文档流，避免遮挡配置字段。截图为合成测试配置。

## 失败与修复记录

保留最初缺少 module/404 的 RED 日志。第一次资源测试把凭据查询误期望 400；既有 Management contract 实际为 403，修正测试并另测普通查询 400。初次浏览器 fixture 模型含 `/`，被可信 Source opaque 规则拒绝；修正 fixture 为合法字面 `<svg>`，未削弱生产校验。

初始三个页面流程通过后，新并发测试实际复现旧层/新版本混用及异常保存回执处理问题。首次修复忽略了未配置 defaults 的 null 层，导致三个初始化流程失败；补齐 revision0/configured=false 的来源层处理，最终六流程在最终嵌入资源上全部通过。中间日志仅记录失败，不能当作最终通过证据。三个浏览器失败日志在归档时仅去除行尾空白；原始日志保留在私有开发记录，原始 SHA-256 记在清单。

[结构化结果](test-results.json) 与 [源文件/证据哈希清单](artifacts.json) 属于本组件，旧包证据未覆盖。前端改动后重建并重新执行浏览器验证；Go 源码未再变化，已通过的 race 回归不重复。Native 进程回归不在本组件重跑范围，先前验证不能作为真实账号准入。

## 尚未完成

Native GUI 鉴权桥仍未实现，普通浏览器直接打开页面会收到 401；不能把临时测试授权视为产品登录功能。预设保存、任务提交/启动、下一阶段手动触发、运行/暂停/取消与事件工作台仍待完成。真实账号/Factory/额度/生成准入、五阶段工程闭环和最终 Gate 保持未完成。Jev 保持 off。

本次没有对上游 push、发布 release 或改变原始项目私有凭据。整个文档需求目标继续 active。
