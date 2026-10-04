# WP-16-EVENT-PAGE-01 · 原 Magpie 任务事件回读

组件完成；父 WP-15/WP-16 与整体目标仍为 in_progress。BASE 47df778。任务详情附近新增默认折叠区、读取新事件/从头读取两个原样式按钮；CSS、原 Magpie三个资源、Store/schema、Controller/Runtime/准入与SSE实现未改。

Native桥的8秒期限和完整缓冲不能用于无界SSE，因此新增受Management保护的固定GET分页companion。每页32+lookahead，连续持久seq、规范int64游标、当前项目登记、末端授权/上下文复核。原SSE独立保留，Native继续拒绝流式通路；不启动模型或追加工作。页面先验证全部元数据再提交游标，保留最近128条，异常/丢失回复同页手动重读，迟到旧任务回复丢弃；事件不采纳run或推进Task。详见[合同与复现](../../../contracts/task-event-page.md)。

有效RED→GREEN分别为未实现分页返回400、Native未允许路径404、页面缺少task-events。首次多页浏览器检查4PASS/1FAIL来自测试字符串用ASCII句号，而产品提示为中文句号；修正定位文本，保留连续序号和状态断言，5项通过。最终扩展至真实141条事件及128条显示上限，完整49项PASS。所有中间日志单独保留，不冒充最终结果；若归档日志尾部空格被规范化，log-provenance记录raw与归档hash，原scratch保留。

最终API race113顶层/81子、Native/Store targeted race12顶层/22子均PASS，无FAIL/SKIP。API全回归包含既有SSE重连、断线、写入期限、capacity和撤销；Native实际owned宿主Close重开后原历史一致，项目撤销/错误窗口/读取后Source变化拒绝披露。Go1.26.3 CLI/GUI/full fusion,nogui vet exit0；现有SDK链接警告原样保留，未宣称修复。构建后只有测试/合同/文档变化，浏览器核对服务资源逐字等于当前源码。

OpenAPI33路径/38操作/85实际Handler样本全部覆盖，新增8个事件页schema反例。schema标准校验不代替连续性、跨字段游标关系或授权时序测试，production_registered仍false。Graph刷新14089nodes/125783edges。

浏览器截图desktop/mobile已人工查看，390px无横向溢出，沿用原布局；不是Native视觉证据。本组件未操作实际Native窗口，没有真实模型/额度调用，Jev off。真实Factory/账号/物理池/计费准入、原启动跨窗口恢复、计划修订UI、实时Native事件订阅与工程阶段闭环继续实施；最终T01–T60仍not_run。

证据：test-results.json为最终结果，build-results.json为构建/日志hash，openapi-final.json和handler-samples.json为离线合同验证，artifacts.json为本组件source/packethash。原始历史组件和最终验收矩阵未改，不发布发行版或合入main。
