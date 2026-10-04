# Native 阶段配置入口

组件：WP-16-NATIVE-UI-01。macOS `fusion` GUI 构建提供 `fusion-ui --projects <privateSource>`；`nogui` 或其它平台明确拒绝该命令。此入口只提供配置草稿和单阶段预览，execution_enabled=false，Jev off。

## 所有权与鉴权

入口创建一个独立 ControlHost 和一个 Wails 窗口，不调用 legacy gateway 的启动流程。管理凭据留在 Go 内存和原有私有文件；页面不接收 token，不用 URL、cookie 或 JavaScript 保存授权。现有 HTTP Management middleware 及 Source/token 的身份、内容和权限核验继续生效，普通浏览器直接访问仍需管理授权。

NativeStageBridge 仅用于 Wails 的进程内虚拟资源回调，禁止挂到网络 listener。校验 SDK RemoteAddr 标记、localhost Host、相对 URL、wails Origin/Referer 和 fetch-site；拒绝外部来源、null Origin、查询参数、转义路径、调用方 Authorization/cookie/转发及其它 x-* header。Wails 自动注入的 x-wails-window-id/name 只接受启动时冻结的本窗口 ID 与固定名称；缺失、重复或其它窗口值拒绝，绑定前保持关闭，不能重新绑定。这两个 header 不向 HTTP 上游转送。

允许阶段页面 GET/HEAD、项目列表/配置/预设 GET、global/project defaults GET/PUT、精确历史预设 GET、单个预设 GET/PUT 和 tasks/preview POST。另开放 [任务索引](task-index-api.md) 的已登记项目 tasks GET 与 tasks/before/{task_id} GET，供后续工作台使用；页面消费者仍待实施。项目必须已登记。任务提交/执行/取消、legacy API、通用 Wails runtime/binding handlers 不在允许范围内。桥只向自己拥有的数字 loopback 地址发送请求，不使用环境代理，不跟随重定向；只转送 Content-Type/If-Match，凭据由 Go 注入。响应只透传有限元数据，剔除 Location/cookie/auth。成功创建 defaults 的 201 仍保留正常回执。

每请求最多 128 KiB、响应最多 2 MiB、整体期限 8 秒。期限和关闭取消在读取请求体前安装；未完成请求体不能阻挡 Close。关闭时停止新请求、清空凭据引用、取消并等待活动请求，然后关闭连接和 ControlHost。

窗口先载入 about:blank，安装仅属于此窗口的 WK navigation delegate 限制后才打开阶段页。主框架只允许准确的 `wails://localhost/fusion/` 和 `/fusion/index.html`，拒绝子框架、外部/file/data URL、账号/端口/查询/fragment 和非规范路径。没有修改上游模块或其它窗口的 delegate。

## 本机运行

要求 macOS/arm64、Go 1.26.3 和 Xcode SDK。私有项目来源格式沿用 [ControlHost 合同](execution-host.md)，所有目录/文件权限沿用现有核验；不要把 key 写入来源或命令行。构建并通过隔离 launcher 运行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/run-dev.py \
  --root /absolute/private/fusion-ui-state \
  --binary "$PWD/.fusion-dev/fusion-gateway-gui" -- \
  fusion-ui --projects /absolute/private/projects.json
```

launcher 使用私有 HOME/XDG 和环境允许清单。启动 stdout 仅含产品、控制地址、模式及执行/Jev 状态。开发二进制不是发行安装包；实际 Native 自动化测试使用独立的本地 .app 包装以识别窗口，未安装或发布应用。

## 验证边界

真实私有 ControlHost 的桥接保存及 Store 回读、非法来源/权限扩大拒绝、Source/token 撤销、关闭取消未完成请求体，均已有 race 测试。Foundation/Objective-C 测试直接调用生产导航谓词，验证规范 URL 与拒绝集合；CLI/GUI 编译和 full vet 已通过。

2026-10-04 完成实际 Native 窗口载入、模型 B/none 的锁定选择、项目保存、独立 HTTP Store 回读、重新载入和窗口关闭。保存显示版本 1，回读确认完整绑定；关闭后进程 exit0，控制端口关闭。前期工具捕捉失败后，用户指出实际 Forbidden；SDK header 复现测试证实原因并完成严格本窗口修正。最终 CUA accessibility 操作成功，截图仍因 ScreenCaptureKit -3812 未取得，未声称完成截图视觉验收。

执行工作台、真实 provider/Factory/账号/额度准入、完整工程闭环和最终 T01–T60 继续待完成；不能据此宣称用户已能执行真实任务。
