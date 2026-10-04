# WP-16-NATIVE-UI-01 · 受限 Native 配置入口

基线 `e6353eb76151b0938eb72cdbbe65868ced358702`。实现、桥接回归和基本实际窗口操作已完成，父 WP-16 保持 in_progress，最终 T01–T60 不变。

新增 macOS GUI `fusion-ui` 入口、仅用于 Wails 虚拟资源的管理鉴权桥和该窗口自己的 WK navigation 限制。只开放配置草稿及单阶段预览，凭据不交给页面，不开放通用代理/执行入口。非 GUI/非 macOS 构建拒绝该命令。关闭取消涵盖请求体读取和活动 HTTP 请求。详见[合同及运行说明](../../../contracts/native-stage-ui.md)。

## 已验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 当前受影响 bootstrap/API/root 的 filtered race 回归 | 31 PASS、107 subtest PASS、0 FAIL | [最终日志](native-ui-sdk-final.log) |
| 实际 Objective-C/Foundation 导航谓词 | 1 PASS、0 FAIL | [日志](native-ui-navigation-green.log) |
| unsupported build/dispatcher 拒绝 Native 命令 | 1 PASS、0 FAIL | [日志](native-ui-unsupported.log) |
| Go1.26.3 CLI/GUI 编译及 full vet | 三项 exit0 | [最终报告](sdk-build-results.json) |
| graph 刷新 | 13932 nodes / 123527 edges | [结构化结果](test-results.json) |

桥接测试实际创建私有 ControlHost，通过 Native-shaped 请求保存 defaults，并经授权 HTTP 回读 Store；验证凭据不进入响应或调用方 request、非法网络 peer/Host/Origin/header/路径/方法拒绝、请求大小限制、Source/token 权限撤销、Host/桥关闭。io.Pipe 的未完成请求体证明 Close 可取消读取，且没有提交 defaults。filtered 回归不是完整 Fusion suite，没有重跑真实供应商/Native Adapter 场景。

## RED 与修复

- 最初缺少桥构造函数为编译 RED；实现后真实 Save503 暴露了不应拒绝所有 Location 的问题。defaults 创建成功的 201 合法携带 Location；现在剔除该 header，并仍拒绝/不跟随 3xx。
- Close 原来在无期限的请求体读取上阻塞；io.Pipe 测试复现，再将期限/取消及 body close 安装到读取前。
- 原 NSURL.path 判断实际不保留目录尾斜杠，使阶段根 URL 被拒绝。直接调用 C 谓词的测试复现，改为两个完整规范 URL 的精确匹配。
- 关闭修复后的中间回归暴露 Source 撤销 fixture 的启动竞争。测试先取得真实 200 就绪响应，再修改 Source；保留生产来源校验，没有修改拒绝预期。

失败日志完整保留为失败证据，不能当作最终通过。[结构化结果](test-results.json) 记录原日志 SHA-256，归档副本仅去除行尾空白。

## Native 实际操作

前期 ScreenCaptureKit -3811 使窗口读取失败；用户随后明确看到 Forbidden。检查固定 Wails SDK 的 webViewAssetRequest.Header 发现自动注入 x-wails-window-id/name，实际请求不等于原测试 fixture。新增 SDK-shaped 请求复现 403，再让桥启动时一次性冻结所属窗口 ID，严格核对固定名称；缺失、重复或其它 ID/名称均拒绝。更新 fixture 带真实 SDK 形态，新增 unbound/zero/rebind/closed 拒绝及六项错误 header 回归，没有放开其它 x-*、网络来源或管理鉴权。

重建后实际 CUA 窗口显示已载入，选择 design locked / native-fixture-b / none，点击保存显示版本 1。独立 [Store 回读](native-ui-readback.json) 确认该完整绑定；点击重新载入后仍是同模型和档位。点击窗口关闭，应用消失、宿主 exit0、控制端口关闭。详见[实际操作记录](native-ui-observation.json)。截图因 ScreenCaptureKit -3812 未取得；操作记录是 accessibility 观察及 API 回读，不能称为截图或视觉验收。

尚未实现预设创建保存、执行工作台、真实 Factory/provider/账号/额度准入与五阶段工程闭环。无真实生成请求、凭据导入或上游 push，Jev off。
