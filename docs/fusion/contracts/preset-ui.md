# 命名预设界面

组件 WP-16-PRESET-UI-01，沿用 [预设 API](preset-api.md)、[阶段编辑](stage-editor.md) 和 [Native 入口](native-stage-ui.md)。不新增数据库 schema、模型调用、额度读取或执行准入。

## 操作流程

1. 选择项目及全局/项目/本次任务范围，展开并设置五角色。收起保留独立 testing/acceptance 的选择。
2. 填写预设名称，点击“保存为新预设”。保存的是当前范围的五角色完整层，不保存其它范围、不改写 global/project defaults，不启动任务。
3. 修改已有预设时，选择预设及明确版本，点击“载入此版本”，编辑名称或角色后点击“保存预设新版本”。新版本基于实际载入的版本；选择下拉项或改动版本输入本身不授予新的保存基版本。
4. 保存冲突时保留本地名称和选择；明确载入新的版本后再编辑保存。载入会提示丢弃当前范围未保存的选择。
5. 保存状态未确认时所有编辑和其它保存入口冻结。点击“重试同一预设保存”只重试原项目、ID、body 和 If-Match；重新载入则提示核对并丢弃本地未确认内容。

新预设在浏览器生成随机 opaque ID，名称可编辑，页面不要求用户管理资源 ID。名称为空、超过 API 的 256 UTF-8 bytes 或含无效 Unicode/control 字符时不能保存；模型/route/effort 或 auto 候选无效时也阻止保存。名称通过 textContent/DOM 属性呈现，不插入 HTML。预设数量上限由服务器拒绝并提示，客户端不自行删除旧预设。

## 版本与回执

载入读取精确历史 URL 并核对 project/id/revision、ETag、名称、hash 和层结构。创建 If-Match 为 `"0"`，修订为准确已载入版本 `"N"`。页面不读取最新 ETag 去覆盖旧编辑内容，不在冲突后自动重试或改成最新版本。

成功保存回执必须匹配原项目/ID、期望 N+1 版本、ETag、名称和完整层；不能把 HTTP 2xx 本身当成确认。传输失败或结构/身份异常的 2xx 保留原请求。已有更高 head 时原请求重试仍只读原不可变版本，不新建名称、不新增版本、不追随 head。下拉显示的是“最近读取版本”，精确载入版本与保存基版本分别核对。

本次任务的预设来源只在明确载入时记录 id/revision。把当前草稿保存为新预设或新版本，不会改写本次任务原来源引用、已提交任务的冻结计划或 default layers；必须再次明确载入才能改变本次来源版本。任务预览仍经过服务端 Compile，未准入路线不能借预设获得生成权限。

Native 桥新增已登记项目单个预设资源的 GET/PUT，保持准确窗口绑定、管理授权、来源/权限撤销、大小/期限和有限 header 转送。DELETE/POST、历史版本 PUT、未知项目及其它执行入口仍拒绝。

## 验证与边界

[组件证据](../work-items/WP-16/PRESET-UI-01/summary.md) 包括真实私有 CLI/API/Store + Chrome 九流程、Native-shaped HTTP 桥回归、实际本机 Native 预设创建/修订/历史载入/旧版冲突/独立 Store 回读及窗口退出。全部使用合成模型和私有临时状态，无真实 provider 调用。桌面与独立窄屏 Chrome 截图已检查；截图不代表 Native 截图或真实供应商账号验证。

复现：运行 stage-editor 合同中的 build-dev.py 和 fusion-stage-editor.test.cjs 全流程；Go targeted race 筛选 `TestNativeStageBridge|TestControlHostStageUI|TestPresetAPI`。真实账号/Factory/额度准入、任务提交执行工作台、五阶段闭环和最终 T01–T60 继续待完成，Jev off。
