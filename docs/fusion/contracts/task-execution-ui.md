# 任务详情运行控制

后续 [START-JOURNAL-API-01](task-start-api.md)已提供受鉴权的原启动记录与精确 Native 通路；[START-RECOVERY-UI-01](task-start-ui.md)已在此页面接入持久准备、跨窗口回读、原运行确认与明确封存。浏览器流程已验证，实际 Native 操作仍待验证。

组件 WP-16-CONTROL-UI-01。沿用 Magpie 原 `.profiles`、`.text`、`.field` 等控件和已存在的换行布局；只在原任务详情区增加阶段选择、启动、暂停、继续、整项取消、原运行请求重试以及安全运行记录。原 Magpie app.css/app.js/index.html 与本组件前的 Fusion CSS 均未修改，不增加框架、配色或导航重设计。

## 操作流程

1. 选择登记项目，打开已保存任务，等待完整 Task/冻结 Plan/预算详情读完。前后两次 Task 条件必须一致，计划版本必须匹配；读取失败或切换项目会清除旧详情并禁用动作。
2. 从冻结计划的 required_roles 选择阶段，明确点击“启动所选阶段”。模型、账号、执行路线、effort 使用冻结计划；页面不重新编译绑定，不自行换模型。ready 且已读调用预算未耗尽才启用启动，最终准入、状态和预算仍由服务端判断。
3. 启动先持久保存原 role/body、完整 Task If-Match 和随机 Idempotency-Key，准备成功才发送原 start。首次回执、独立同 run 读取及当前 Task 读取全部核对成功后确认原记录，才清除原启动请求。运行 ID、角色、attempt、generation、plan_revision 与完整 target 必须匹配，结构像成功但未证实的 run 不作为已知执行。
4. 暂停、继续、取消使用 `{}` 与当前完整 Task 条件，不伪造启动 key。继续只恢复可靠 paused 的派单资格，不自动启动。整项取消有明确确认；202 表示意图受理，不能报告已停止。运行暂停收尾可能为 needs_review，此时禁止盲目继续/启动。
5. 点击“重新读取任务”核对实际状态和当前窗口已知 run。独立 run 读取失败时保留已核对的 Task/Plan/预算，清除不可证实的运行展示并报告失败；不猜造新 generation，不自动启动下一角色。

## 未确认、冲突与权限

断线、不可解析回执、错误成功结构、被替换的 run ID 或独立证明读取失败均保留同一原请求。禁用项目/草稿修改、新提交和其他运行动作，仅允许明确重试原运行请求或重新读取同 Task。重试保持原 body、key、If-Match；无自动重试、无新 key 避开冲突。带有效 run 的结构化 409 仍按原意图核对，不丢弃已写入的执行。

首次准备明确的输入/冲突/缺条件/旧条件拒绝，在没有可信 journal、已接收或未知意图时可解除本地请求，但必须重新读取 Task 才能再操作；412 不自动覆盖新版本，401/403 不证明准备没有发生。已经有受理回执却后续 run 404 不等于启动未发生，仍保留原 key。pause/continue/cancel 未确认后，明确回读得到不同 Task 条件可证明原 CAS 已失效，解锁供人工核对，不自动重发动作；start 的确认、封存与未落库特殊解除边界见[恢复页面](task-start-ui.md)。

Management 凭据仍由 [Native 桥](native-task-control.md)注入，页面不接收管理 key/token，不增加浏览器存储。窗口关闭前对未确认请求提示；后续[原启动恢复页面](task-start-ui.md)从私有 Store 回读已准备的身份。[私有提交记录](task-submission-ui.md)恢复的是任务提交，不代替阶段 start 恢复。

## 状态与验证边界

阶段 succeeded 不等于整项验收；取消不证明文件回滚或预算退款。已知运行显示 run ID、角色、state、attempt、generation、模型和路线/effort，不展示凭据。状态由手动回读获得，没有 SSE/自动轮询、额度面板、计划修订、checkpoint 恢复或工程阶段 Gate。

产品 fusion-ui/fusion-control 的 OpenControl 仍为 execution_enabled=false；新增按钮没有注册真实 Factory 或授予准入，产品控制仍可能返回 503。test-only fixture 显式 hold/success 接真实 HTTP/Store/Controller，但运行、额度、session、StopProof 为合成；无真实模型调用。实际 Native 窗口的这些按钮仍未验证，Jev off，父 WP-16/整体目标 in_progress，最终 T01–T60 not_run。测试记录、截图与复现见 [CONTROL-UI-01](../work-items/WP-16/CONTROL-UI-01/summary.md)。

后续 [EVENT-PAGE-01](task-event-page.md)新增精确事件分页 GET 及原 UI 默认折叠记录区；仅持久状态元数据回读，原 SSE 不开放到 Native 桥，不修改运行回执或自动推进 Task。CSS 未改，实际 Native 事件按钮未验证。

后续[原启动记录 Store](task-start-journal.md)、[API](task-start-api.md)与[恢复页面](task-start-ui.md)已补齐持久准备、原子 run 关联、浏览器/宿主重开回读和明确封存；原 Magpie UI 样式与全部 CSS 未改，实际 Native 恢复点击仍未验证。

后续[工作流与设计检查点](workflow-ui.md)在同一原样式任务表单/详情区接入四种有限类型、多角色预览、明确附加/设计冻结/当前计划批准；原手动单阶段仍为默认。批准不自动启动下一阶段，普通 continue 不批准。CSS、主资源和导航未改，实际 Native 工作流操作未验证。
