# WP-19-WORKFLOW-UI-01 · 原样式工作流页面

在现有任务表单增加类型选择、任务详情增加默认折叠的设计检查点。调查/规划仅 design，独立审查仅 review，变更/修复固定五角色；每项模型/effort 从原用户配置编译。原手动单阶段仍为默认，原冻结提交与持久恢复协议保持；明确附加、冻结完整设计、审阅后批准当前计划均在原详情完成，不自动启动下一阶段。BASE 6cce720ab85db6fdc0d06da8ad6f54e485b1ab95。父 WP-19 和整体目标仍 active/in_progress。

UI 写操作携带当前完整 Task ETag，设计回执须匹配原 run/document，批准须匹配原 Task/计划/两项 hash。等待/失败/停止未证实/设计或批准缺失均禁用启动；多角色任务必须先明确附加流程。未知/私有字段、其他 Task、跨计划批准或迟到回复不显示授权。回复丢失或条件/权限失效时保留表单，禁用启动/后续写入，用户重新读取真实决定后审阅，不自动重试。普通 continue 不批准，不改变模型、预算、文件权限或 Runtime 准入。

有效 RED→GREEN：新增类型选择原先不存在；多角色任务原先可在附加之前启动，现在禁用。前期测试错误曾把 succeeded 当作可靠停止释放、把默认折叠详情当作可见，已按真实服务端 blocker 和显式展开修正，断言仍要求原运行与完整设计/标准。另一次新测试误要求暂停后补挂流程；复核既有合同/Store/schema9 明确要求 ready/generation0，恢复该限制并验证真实 API409/null/状态不变，没有削弱持久保护。原初期日志保留，最终结果不混入中间失败。

最终原工作台浏览器全回归 70 PASS/0 FAIL/0 SKIP，另增五角色丢失提交回执→关闭窗口/宿主重开→同 body/key 恢复测试 1 PASS，总计 71 项独立浏览器用例，其中本组件新增 12 项。覆盖四类角色、真实 HTTP/Controller/SQLite 合成 design 完成/冻结/批准、完整 hash 条件、失去回复/权限、旧 Task 迟到、空/无效批准、跨计划/私有字段、窄窗布局及既有编辑/预设/提交/控制/额度/事件/启动恢复。追加恢复测试不改变生产源码，验证原冻结五角色保留、没有推测 kind 或启动阶段。

Native 桥 targeted regression 11 顶层/22 子测试 PASS，GUI 鉴权与本机导航 3 PASS；Go1.26.3 CLI/GUI/full vet exit0。最后两项 UI 启动/附加控件条件变更后重新构建及全浏览器回归；Go/Native/API/Store/policy/Runtime、Go module 和 schema9 没有修改。原 GUI deployment-target/compiler warning 保留，不声称其他平台或实际 Native 窗口通过。Go 图符号保持原索引；UI 资源不在图范围，以实际源码核验。

四个 UI/测试文件改变，其他 471 个 internal/gui 文件与 BASE 一致；三个原主资源与 pinned Magpie 一致，全部 CSS 与 BASE 一致。桌面/390px 窄窗[截图](workflow-ui-desktop.png)和[移动尺寸截图](workflow-ui-mobile.png)逐项检查，内容、hash 换行正常，无横向溢出，HTML 注入样本显示为文字。截图来自隔离浏览器与合成只读宿主，不是实际 Wails 窗口操作或真实供应商结果。

复现和回退见[页面合同](../../../contracts/workflow-ui.md)。任务类型只决定预览角色；用户保存后明确附加固定 kind，关闭后按原冻结角色核对，不猜原 kind。真实三路线 Gate A、原主界面全部页面整合、实际 Native 工作流操作、真实 OS 写范围、模型结果 Handoff/测试审查证据/有限返工及最终 T01–T60 仍未完成。真实模型/额度调用零，Jev off。独立提交/推送到 origin fusion/development，不合入 main 或发布。
