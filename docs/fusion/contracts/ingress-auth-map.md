# Fusion 入口鉴权清单

WP-06 对 Fusion 构建封闭全部旧入口；无 tag 构建保持原版回归语义。旧 handler 在注册 mux 前直接返回 DisabledIngress，不因 loopback、原版 named key、cookie、query key、角色或账号 header 放行。未知路径同样拒绝。管理/任务/模型新 handler 必须通过 policy.Manager 明确包装后由 WP-07/WP-15/WP-16 接入，不能解除旧 guard 以恢复宽松代理。

| 入口 | 源码边界 | 当前 Fusion 行为 |
|---|---|---|
| Gateway 基础信息、models、quota、route、concurrency | gateway.Server.Handler | 401；query 认证字段 403；不注册旧 mux |
| Chat、Responses、Messages、systemone、count_tokens、images、videos、Gemini 与短路径别名 | 同一 Handler | 全部拒绝；记录型模型 Transport 的 outbound/denied 均为 0，未进入模型出口 |
| `/backend-api/codex/*`、WebSocket Upgrade | 同一 Handler 与 codexBackend | 鉴权拒绝先于升级/后端；不进入旧 Codex 路线 |
| `/_magpie/claude-mcp/{token}` | 同一 Handler | 不能以 URL 中旧 token 绕过阶段身份 |
| GUI assets、boot.js、所有 `/api/*` 管理/导入/账号/设置/会话/备份/窗口入口 | gui.Handler | 在 mux 注册前拒绝，覆盖 HTTP 与 Wails AssetOptions native bridge |
| 未来任务/事件/SSE 与未知 GUI 路径 | 同一 GUI Handler | 默认拒绝；新接口按管理或准确 stage audience 接入 |
| 新任务预览、提交、读取、plan 修订、budget 与 SSE 组件 | fusion/api.Server.Handler | policy.Manager.Management 强鉴权；SSE 持续检查当前 issuer/撤销；独立 ControlHost Management HTTP 接入；Native 仅开放逐项核验后的精确通路，不开放旧 GUI/gateway |
| 原版 query-key Web | gui.StartWeb | 在生成 Key、创建 listener 或打印 URL 前拒绝；没有新的监听端口 |
| 原版开发 backend/shell/listen | gui.Run、Handler | Fusion Run 拒绝 devRole；旧 devRoutes/devListen 不执行 |
| URI 导入与单实例导入 | gui.Run、host.Import | 拒绝/忽略旧导入入口，不改变受控路线 |
| 插件 host、自动更新、共享 Keychain、cloud sync | WP-02 开发隔离 | 保持关闭；不提供 Worker 的直接插件退路 |

字面路由登记清单见 WP-06 的 route-inventory.json；动态 CodexPath 前缀及 dev/native 通道在上表补全。管理入口的正向/负向测试通过 policy.Manager；在安全原语可用前关闭旧入口是开发状态，不能把返回 401 的 GUI 宣称为手动版已可用。

GUI startBackend 在 Fusion 中跳过 catalog/provider 发现和日常客户端目录同步；原版 watch/keep-fresh 不运行，stats 不启动。负向 GUI RED 测试使用 macOS deny-network-outbound 防止旧发现副作用；GREEN 不再进入发现逻辑。

证据覆盖 handler、作用域、撤销/过期、跨 Origin/query key、权限 header 清除和编译。尚未启动真实 Wails 窗口交互或受管供应商进程；完整 UI/SSE/原生运行与最终 T17/T18/T45/T48/T49/T50/T59 仍在后续 Gate 验收。

WP-15-EVENTS-01 已验证组件的实际 loopback SSE、持久 sequence 重连补读、断线不取消/启动任务和授权撤销后关闭连接；这不代表生产 UI 接线或真实供应商任务已通过。

WP-15-REVISION-01 新增 GET plan、POST plan/preview 和 PUT plan 组件接口；都使用 Management 身份，强 If-Match、任务/用途/配置绑定的 receipt 和服务端冻结 hash。预览及修订不调用 Runtime；已在独立 ControlHost 的 Management HTTP 接入，[Native 修订桥](native-plan-revision.md)已开放精确 POST/PUT 通路。界面修订控件、实际 Wails 操作和真实供应商准入尚未完成。
