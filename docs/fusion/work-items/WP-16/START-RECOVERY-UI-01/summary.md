# WP-16-START-RECOVERY-UI-01 · 原详情区启动恢复

本组件完成，BASE 5727dc8；父 WP-15/WP-16 与整体目标保持 in_progress，最终 T01–T60 not_run。原 Magpie 主界面、样式变量、布局组件、配色、导航与全部 CSS 未改，只扩展现有任务详情的原启动记录与重新核对/明确封存按钮。不增加 UI 框架。

启动先持久准备原 ready Task/Plan/role/key/条件，核对后才发送原 start。浏览器/宿主重开仅回读待解决项目的原冻结记录；committed 通过原 run 与当前 Task 的独立读取确认元数据，不再发送 start。确认/封存回执丢失或被替换时保留同一原身份并锁定其他操作；没有 run 时才允许明确封存，迟到 start 被拒绝。当前默认配置或模型改变不会覆盖原请求，不自动推进角色或运行。

未落库准备的特殊解除须同时满足：从未发送 start、没有可信 journal/run、精确读取 404、同项目/ID/目标的当前 Task 单调前进、再一次精确读取仍 404。准备未确认或随后尝试封存仍未确认，都可按同一证明回读解除；有效 RED 已覆盖封存后的该边界。一次缺失、无法独立读 Task、目标替换或第二次发现迟到准备均继续锁定。这不删除或伪造服务端终态。

最终完整实际浏览器 59 PASS、0 FAIL/0 SKIP，包含 10 项启动恢复测试和原有 49 项页面回归；模型纯函数 10 PASS。Go targeted 共 17 顶层/60 子测试 PASS、0 FAIL/0 SKIP，覆盖现有 Native 通路、原启动记录/重开/来源撤销、静态资源鉴权及 GUI 精确导航。Go1.26.3 CLI、GUI 与 full vet exit0。macOS 编译器既有枚举转换/链接 deployment target warning 原样保留，不影响 exit0，也不作为其他 macOS 版本兼容验证。

有效 RED：旧页面没有持久准备，缺失功能测试失败；特殊解除未实现时 timeout 失败。中间完整 59 项有 58 PASS/1 FAIL：新增测试在旧失败提示仍显示时过早移除 Task 响应拦截，导致生产读取了已恢复的真实响应；修正测试为等待本次回读控件恢复后再移除拦截，断言及生产状态边界未放松。最终 10 项与完整 59 项通过。旧运行测试同步新协议：已 committed 只读/确认，断言 start POST 恰好一次且历史精确 key/条件/role/run 对应；旧条件在 prepare 返回 412，断言零 start 和零 pending。

桌面与 390px 窄窗口截图已查看，完整模型/条件/说明可读，无横向溢出。原 Magpie 三个主资源与 pinned 上游逐字一致；267 个 CSS/后端/Native 文件与 BASE 逐字一致。仅 HTML、工作台 JS、浏览器测试和相关文档/证据有改动；无 Go symbol 变化，UI 资源不在 graph 索引范围，未进行无意义的 graph 刷新。

本次 Native 工具再次报告 Mac 锁屏，实际 Native 窗口点击/像素未验证；浏览器/桥测试不代替它。所有运行、额度和停止证明均来自隔离合成 fixture，真实供应商模型/额度调用为零，Jev off。产品真实 Factory、账号/计费/物理池准入、工程阶段闭环与最终验收仍未完成。本组件不合入 main、不发布发行版。

操作与复现见[合同](../../../contracts/task-start-ui.md)。证据：最终 browser/model/Go 日志、RED 与中间失败日志、build-results.json/原构建日志、test-results.json、artifacts.json 和桌面/窄窗口截图。日志副本只去 ANSI 显示控制与行尾空白，原 scratch hash 与副本 hash 均记录；构建日志原样保存。artifacts.json 记录本组件源码、证据与最终 CLI/GUI 二进制 hash，不包含任何真实凭据。
