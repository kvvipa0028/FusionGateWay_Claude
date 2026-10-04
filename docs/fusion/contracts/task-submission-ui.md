# 原请求恢复 UI

组件 WP-16-SUBMISSION-UI-01。在 Magpie 原布局中增加“原任务提交”列表区，复用原行、按钮、字体和色彩；原 app.css/app.js/index.html 与已锁定上游逐字一致，Fusion editor.css 未改。API/Store/schema 7 和执行准入未变。

## 操作与证明

1. 在已登记项目预览单阶段任务，核对具体绑定及预算后点击“冻结提交任务”。页面生成一次原 key，先 POST 项目 submission，逐项验证原完整预览、目标、项目和 key，再用同 body/key POST Task；准备未证明成功不发送 Task POST。
2. Task 201 的项目、完整目标、计划版本和 ID 通过后，以原身份 acknowledge。只有 acknowledged 回执的原 Task ID 与 Task 回执一致才解除锁定并显示任务详情。确认失败点击“重试任务保存确认”，不会重复 Task POST。
3. 新窗口只读取已登记项目的未解决记录，选择有记录的项目并显示原目标、预览、配置版本、有效期、模型/路线/effort/账号/计费/锁定和预算限制。上方当前配置禁用，不当作原绑定。启动读取不自动提交、确认或放弃。
4. 点击“重新核对原请求”读取同一历史；“重试同一任务提交”保持原 body/key。committed 可读同一 Task；重启后的 prepared 不重新编译丢失的预览，原 Task POST 得到 409，仍保留原记录。
5. 需要放弃时点击“放弃未提交请求”，明确确认后原子封存。只有相同原身份的 abandoned 回执证明成功才允许新预览，迟到原提交仍拒绝。已经提交的任务不能放弃原请求；冲突读回原状态，不猜测成功。

记录校验包括完整计划、预算、预设来源和配置版本；时间的不同 timezone 表示只按同一时刻比较。未知准备/Task/确认/封存回执都保留原身份；成功形状但不匹配的 metadata 不清除记录。缓存的 Task 201 ID 单独显示“任务回执待核对”，只有服务端原 journal 的 ID 作为原任务身份。另一窗口先完成确认后，当前窗口读精确终态并 GET 同 Task，不能根据相同目标、计划 hash 或列表猜 ID。

## 恢复及权限边界

任务输入和认证不写 browser storage。完整原请求由服务端私有 SQLite 保存；原 key 是幂等身份，不是授权。当前 Management/Source/项目登记/Native 窗口权限仍检查，来源撤销不能绕过。prepare、核对、确认和封存不启动 Runtime、不查询外部额度、不改预算计数或 Task 事件。

prepare 本身未达到持久存储的草稿仍可能在关闭窗口时丢失；页面不会在这种情况下发送 Task POST，beforeunload 提示不等于任意未保存草稿恢复。原身份未知且服务不可用时保持锁定，不以无记录猜测第一次未提交。旧版本没有 journal 的窗口输入不伪造迁移恢复。

产品 fusion-ui 仍为未准入 OpenControl 草稿宿主；真实路线预览可返回 422。成功流程仅由独立 synthetic test Factory 验证，Inspect/Resolve 拒绝、执行关闭、Jev off，不能代替供应商 Factory/账号/quota 准入。

## 验证

最终 27 项浏览器回归全部通过，包括真正关闭并重开私有宿主；新增桌面/390px 窄窗口恢复区截图及无横向溢出检查通过。40 项 targeted Go 测试（86 子测试）、Go1.26.3 CLI/GUI/vet 通过。日志、复现命令及边界见 [交付记录](../work-items/WP-16/SUBMISSION-UI-01/summary.md)。

实际 Native 恢复窗口启动曾遭工具错误 “The Mac is locked”。私有 HTTP 回读和 owned launcher 正常退出不是窗口内容/按钮/关闭重开证明；这些实际操作仍待解锁后验证。阶段执行控制、真实供应商准入、工程闭环及最终 T01–T60 继续未完成。
