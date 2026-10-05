# Magpie Providers 接入

组件 WP-16-PROVIDERS-UI-01，复用原 Magpie Providers 模板、列表和 modal，沿用原页头、鸟形标识、导航、深浅色与窄屏样式。没有改动原 index.html、app.js、i18n.js 或六份 CSS，不引入 UI 框架。

## 操作

1. 按[Native 窗口说明](native-stage-ui.md)或[阶段编辑页面](stage-editor.md)启动隔离宿主，从既有 Fusion 面板选择登记项目及配置范围。
2. 点击原 Providers 导航。页面显示当前项目登记的模型、账号和路线版本；点击行或用 Enter/Space 打开原详情框。详情显示工作区、Runtime、计费核验、准入、模型锁定与 effort 能力。登记状态不替代登录、额度或启动时的核验。
3. 选择一个阶段，点击“应用到阶段草稿”。只替换当前配置范围中该角色的本地模型选择，保留其他角色，清除该角色旧 effort 并要求重新确认。随后使用既有保存或任务预览流程；选模型本身不保存、不调用供应商、不启动任务。
4. “重新载入模型与账号”使用原重新载入流程，存在未保存草稿时要求明确是否丢弃。保存状态未确认或任务操作未完成时，详情中的应用操作禁用。Escape、关闭按钮或点击遮罩关闭详情，焦点返回原列表行。

## 数据合同

只读取既有 Management GET `/control/v1/projects/{project_id}/configuration`，新增响应字段 `project_id`，与请求项目精确一致；`revision` 为正安全整数，ETag 必须等于该 revision。JSON schema 和 Handler 测试均记录此新增字段。旧宽松消费者可忽略新增字段；旧严格响应 DTO 与缓存样本需一起更新，本版新页面拒绝缺少此字段的旧宿主响应。

模型路线只复制显示所需字段，不显示 credential_identity、key 或未知扩展字段。严格核对路线引用、类型、锁定状态与 effort；通过 textContent 呈现文本。配置缺失、解析错误、跨项目、ETag 不匹配、来源或授权撤销时清空旧行与详情；旧项目迟到响应不能恢复旧信息。显示元数据不产生执行授权。

服务端路由、HTTP 方法、Native 白名单、窗口身份、凭据、Store schema 和准入规则不变。模板锚点漂移时生成失败并关闭入口；不加载原 boot/app 脚本，不开放旧 `/api/*`。原 Add provider 按钮暂时禁用，不能据此声明账号新增/登录或原 Providers 全部功能完成。

验证与浏览器截图见[本组件证据](../work-items/WP-16/PROVIDERS-UI-01/summary.md)。截图使用合成模型、账号和本地真实 ControlHost，不是供应商执行或当前 Wails 像素。WP-16 保持 in_progress，最终验收保持 not_run，Jev off。
