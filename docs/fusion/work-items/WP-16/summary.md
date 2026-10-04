# WP-16 · 阶段选择 UI 与单阶段工作台

状态：in_progress。

[EDITOR-01](EDITOR-01/summary.md) 已完成管理授权保护的阶段配置页面、三级草稿、三个区域/五角色、明确模型和 effort、精确预设载入、defaults 保存/并发冲突/未确认重试、单阶段服务端预览，并通过隔离 Chrome + 真实私有 CLI/API 验证。

[NATIVE-UI-01](NATIVE-UI-01/summary.md) 已完成受限 Native 鉴权桥、入口和实际窗口保存/载入/退出验证；截图视觉证据尚未取得。父包继续 in_progress。

[PRESET-UI-01](PRESET-UI-01/summary.md) 已完成命名预设创建、版本保存、明确载入、冲突保留和原请求重试；通过浏览器、Native 窗口及独立 Store 回读验证。

[TASK-INDEX-01](../WP-15/TASK-INDEX-01/summary.md) 补齐工作台所需的只读任务分页发现和受限 Native GET 通路，真实 HTTP 提交/列表/来源撤销及 Store 分页验证通过；后续 TASK-UI-01 已加入页面消费者。

[MAGPIE-UI-01](MAGPIE-UI-01/summary.md) 按用户要求改为直接复用原 Magpie 样式和布局组件，补齐成功预览的角色映射读取；浏览器交互、样式来源、权限回归及构建已验证。本轮未重跑实际 Native 窗口操作。

[TASK-UI-01](TASK-UI-01/summary.md) 已接通服务端预览的冻结提交、未确认原请求重试、32+1 任务分页及完整目标/计划/预算读取；复用原 Magpie UI，浏览器和 Native 桥/HTTP 验证通过。当前私有产品入口真实路线仍未准入，合成 Factory 不可代替真实账号验证。

[SUBMISSION-UI-01](SUBMISSION-UI-01/summary.md) 已接入提交前持久准备、打开窗口读取原请求、同请求提交回执核对、保存确认及明确封存。最终 27 项浏览器回归通过，新增恢复区桌面/窄窗口检查通过，40 项 targeted Go 回归及 CLI/GUI/vet 通过；沿用 Magpie 原 UI，CSS 未改。实际 Native 恢复窗口因系统锁屏尚未验证。

服务端 [SUBMISSION-RECEIPT-01](../WP-15/SUBMISSION-RECEIPT-01/summary.md)、[SUBMISSION-JOURNAL-01](../WP-15/SUBMISSION-JOURNAL-01/summary.md) 和 [SUBMISSION-API-01](../WP-15/SUBMISSION-API-01/summary.md) 提供原回执、私有记录与受限桥接。浏览器测试包含真正停止并重开 owned 私有宿主；它不替代 Native 窗口验证。

[CONTROL-BRIDGE-01](CONTROL-BRIDGE-01/summary.md) 已开放精确 Native start/pause/continue/cancel/run 读取与取消；54 项 targeted 回归及 CLI/GUI/vet 通过。该项完成桥接层，后续 CONTROL-UI-01 已接按钮和手动状态回读；事件、真实供应商 Factory 和实际 Native 操作仍待接入或验证。桥接组件当时页面与 CSS 未改。

[CONTROL-UI-01](CONTROL-UI-01/summary.md) 已在原任务详情区接入明确角色启动、暂停、继续、整项取消、运行记录回读及未确认原运行请求重试；复用原样式，CSS 未改。浏览器与合成执行、版本/回执边界通过验证，真实供应商执行与实际 Native 运行按钮仍未验证。

工程流程的下一阶段 Gate、计划修订工作台及实际 Native 恢复窗口仍待实施或验证；有界运行事件与原启动身份浏览器恢复见后续组件。页面测试未证明真实 provider 或账号执行；最终验收矩阵保持 not_run，Jev off。

[QUOTA-UI-01](QUOTA-UI-01/summary.md)以默认折叠列表接入缓存读取和显式供应商刷新，复用 Magpie UI，CSS 不改。空值/历史/共享池/来源时间、跨项目响应和失效授权已验证；Native query-only 产品启动/未知缓存/退出通过，实际窗口点击因锁屏未验证。

[EVENT-PAGE-01](EVENT-PAGE-01/summary.md)新增有界只读事件分页及精确 Native 通路，在原详情附近用默认折叠区按序回读，CSS 未改。完整浏览器49PASS、API113顶层/81子、Native/Store12顶层/22子PASS；关闭重开、授权/来源撤销和游标边界验证通过。实际 Native 点击、实时推送、真实供应商执行和完整工程闭环仍未完成；父包保持 in_progress，最终60类验收 not_run。

[START-JOURNAL-01](../WP-15/START-JOURNAL-01/summary.md)及[START-JOURNAL-API-01](../WP-15/START-JOURNAL-API-01/summary.md)提供 Store 原记录与精确 Native 通路；后续[START-RECOVERY-UI-01](START-RECOVERY-UI-01/summary.md)已在原详情区接入持久准备、浏览器/宿主重开回读、独立运行确认和明确封存。复用原 Magpie 样式，全部 CSS 未改；实际 Native 恢复点击仍未验证，父包保持 in_progress。
