# WP-16-PRESET-UI-01 · 命名预设创建与版本保存

基线 `657909d2955b6dbb3cbff0b91d27fe22dfb100ae`。本组件完成，父 WP-16 继续 in_progress，最终 T01–T60 不变。

界面新增名称、保存为新预设、保存预设新版本和原请求重试。当前范围的五角色独立保存，精确已载入版本是 If-Match 基准；不隐式保存 defaults，不提升 route/账号/额度准入，不改变本次任务已有 preset 来源。Native 桥只增加已登记项目的单预设 GET/PUT。详见[用户流程与合同](../../../contracts/preset-ui.md)。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 实际私有 CLI/API/Store + Chrome 页面 | 9 PASS、0 FAIL | [最终浏览器日志](preset-ui-browser-verified.log) |
| Native 桥、受保护资源、Preset API filtered race | 20 PASS、48 subtest PASS、0 FAIL | [Go 日志](preset-ui-go-final.log) |
| Go1.26.3 CLI/GUI 编译及 full vet | 三项 exit0 | [报告](build-results.json) |
| 实际 Native 预设创建/修订/旧版载入/冲突/Store 回读/关闭 | 通过已执行的基本流程 | [操作记录](native-observation.json)、[Store 回读](native-readback.json) |
| 代码索引刷新 | 13933 nodes / 123553 edges | [结果](test-results.json) |

新增三个浏览器流程覆盖独立 testing/acceptance、名称安全呈现、明确版本修订、旧历史不可变、任务来源不追随新版本、defaults 不变、两窗口冲突、异常 2xx 回执冻结、创建后断线的相同 ID/body/tag 重试、已有更高 head 时仍读回原版本，以及无效名称/模型选择阻止保存。原六流程在新资源上一起回归；每个 fixture 比对服务资源和当前源文件，使用独立 Chrome profile，并断言宿主正常退出、删除自己创建的临时目录。没有真实 provider 调用。

Native 实际创建版本 1，改名后保存版本 2，载入版本 1 重新显示原名，再改名尝试保存得到 412 冲突且保留本地名称。独立 HTTP 回读确认两个不可变版本、head2、五角色及原项目 defaults revision1/模型 B 保持原值。窗口关闭后 app 不再运行、launcher exit0、控制端口关闭。Native 流程在最后的名称草稿确认/响应层结构补强之前执行；这些补强在最终浏览器九流程中验证，不扩大 Native 实测覆盖范围。

已查看 [1140px 桌面](desktop.png) 与 [390px 窄屏](mobile.png) 的实际 Chrome 截图。首次在桌面 context 改 viewport 后整页截图出现重复区块，保留[异常截图](mobile-capture-repeated.png)。改用独立窄屏 context 重新载入后正常，没有通过修改产品 CSS 隐藏该捕捉问题；该截图不是 Native 截图。

## RED 与修复

创建按钮缺失的浏览器 RED 与 Native preset PUT404 的 RED 分别证明界面/桥尚缺功能。实现后原两个新流程通过，再补创建回执丢失、head 已前进和名称/模型输入边界。

追加名称草稿测试实际复现下拉选择把本地名称变成服务器名称。保留 RED，修正为下拉只选资源/版本并清除写入基准，只有实际载入版本才覆盖名称，并对未保存名称提示确认。拒绝确认保留名称和原角色；确认后读取精确版本。保存回执还要求 layer 为实际对象，不能把缺失层解释为全 inherit。

原始日志的 SHA-256 记录于[结果](test-results.json)，归档仅去除行尾空白。[文件哈希清单](artifacts.json) 绑定本组件源文件及证据。未运行全 Fusion suite 或真实供应商场景，filtered race 不替代它们。

## 未完成

任务提交/单阶段执行/下一阶段推进、运行事件/额度/取消暂停/计划修订工作台、真实 Factory/provider/账号/权益准入、完整五阶段工程闭环及最终 Gate 继续待实现验证。Jev off；本项不表示整体目标已完成。
