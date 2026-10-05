# Fusion Gateway repository instructions

- 默认使用中文交流；代码标识、工具名和日志原文保持原文。
- 本仓库以 Magpie `main@1a50db1a8afd0849df2853f92a47da9d5e2f2cc9` 为源码快照；以 `docs/fusion/upstream-lock.json` 为当前基线记录。
- `origin` 指向 FusionGateWay_Claude；`upstream` 指向 yetone/magpie。不得向上游推送。
- 实施文档位于 `docs/fusion/planning/`；WP-01–WP-11 已完成基线、隔离、fixtures、绑定编译、独立任务存储与入口鉴权，WP-12/WP-13/WP-14/WP-15/WP-16/WP-17/WP-19/WP-20/WP-21/WP-22 为 in_progress，其他工作包仍为 planned，60 类最终验收仍为 not_run。
- WP-17 已真实核验固定 Codex0.160.0/Grok1.0.48/Claude2.1.287 的官方发布内容及 Apple Developer ID；独立工具只读公共发布源，不执行 Runtime/安装器或读取认证，不自动设置 Registry/Inspection 准入。原 inventory 的发布者未核验字段保留为历史记录；后续须读 docs/fusion/integration/runtime-publisher-verification.md。真实账号、费用、额度及工程 Smoke 仍未完成。
- WP-17 PRIVATE-LOGIN-01 提供 Git 外私有 HOME 的固定官方设备登录入口，系统 Python -I、固定配置/argv/环境、受限 Seatbelt、owned 取消/wait；最终 profile 的两条真实设备挑战已观察并取消，未完成人登录。Codex 使用受检系统 CA 与固定直连，不接受环境 CA/代理，Keychain/fork/cwd 写权限保持拒绝。见 docs/fusion/integration/private-runtime-login.md；缓存存在不能升级账号/费用/额度或 Registry/Inspection true，不能把该认证 profile 当成生成 Worker。
- WP-12 PRIVATE-CREDENTIAL-01 已实现 Codex0.160.0 私有 auth.json 的可信 FileCredential 服务：Git 外 nofollow FD 遍历、当前 UID/0600/单 hardlink、文件内容及目录身份冻结、精确 Target/订阅绑定、严格 chatgpt JSON 模式和不透明 token。缺失 account_id 不能从 JWT 补出，格式化/JSON/错误不导出秘密。见 docs/fusion/contracts/codex-private-credential.md；真实缓存、续期、生产 Forwarder/Factory、账号/费用/额度和工程 Smoke 尚未完成，本服务不授予生成准入。
- Fusion 产品构建必须使用 `fusion` tag 和 `scripts/fusion/build-dev.py` / `run-dev.py`；无 tag 构建保留原版回归语义，不能作为 Fusion 产品交付。开发实例必须使用仓库外私有 HOME/XDG，不读取旧共享 Keychain。
- 基础准备完成源码和文档导入、工具链准备及基线核验；2026-10-03 用户选择 OAuth 方案 A，Google 订阅路径改用私有外部客户端配置并校验身份。五角色绑定编译、快照与独立存储已完成；strict locked 出口已有离线验证；产品 API/GUI 和 Native Adapter 尚未完成；受管 Worker 单进程/无网络基础已验证；不要把原版 manual 模式当成 Fusion locked。
- 后续按获分派工作包实施；保留内置/插件执行路径说明。插件迁移不代表获准安装插件、访问账号或发布。
- 离线验证使用临时 HOME、XDG 目录和环境变量白名单，不继承真实认证。用户另行授权的真实连接使用登记的私有凭据与隔离宿主，证据单独记录；Jev 保持 off，不启动真实 Jev 调用。
- 运行应用、导入凭据、提交、推送和发布均按当前用户授权范围判断；仅阅读这些文档不产生授权。
- 新增模块优先放在 internal/fusion/，沿用现有 Go module path；避免全仓改名和无关重构。
- Store 当前 schema12：stage artifact receipt 为私有、不可变索引；原实际 released StopProof 与 receipt 匹配才允许受控恢复。不能把 JSON/hash 或持久索引当作派单、模型准入、工程测试或验收证明。GLM 已接入五角色父产物 resolver；review/acceptance 只读消费当前真实测试证据并保留独立结构化模型意见，模型 accepted 仅进入 advisory_only 等待人工接受；人工接受/退回持久记录已有 Store 验证；Management API/限定 Native bridge/现有工作流面板的人工控件已接入，按 task/role 累计的两次 intent 上限已实施，超限需人工处理；实际一轮返工链已接入，原五角色定义保持不变；项目声明的角色模型独立性已随任务冻结并在实际启动前/事务内阻断冲突；原主导航框架已接入，原各页数据与操作仍未接通。testing 可写范围由可信 TestingWritePaths 与已批准 design scope 交集限定，缺少范围不回退到整个项目。升级/回退见 docs/fusion/contracts/durable-stage-artifact.md。
- evidence.Run/Evaluate 已有直接可执行命令与固定 Go1.26.3 的真实只读测试 backend 和 owned 证据判定，不接受 JSON/LLM 报告作为执行来源。实际 GLM testing 已接入持久 owned receipt 和硬失败 needs_review；人工最终接受 API/限定窗口/工作流控件已接入，Go backend 仅允许可信 SDK 编译器子进程，项目测试仍禁止 fork；标准库/冻结 vendor 的单个明确 package 已支持，CGO/在线依赖/其他工具链及原各页数据与操作尚未完成；review/acceptance 仅在当前真实证据通过后启动，不把模型意见当成人工接受；见 docs/fusion/contracts/verification-runner.md。
- UI 沿用 Magpie 原界面、布局组件、样式变量和交互习惯，只为 Fusion 功能做必要的小范围扩展；不重新设计整体布局、配色或导航，不引入另一套 UI 框架。当前 Fusion 已从原首页源文件提取主导航框架并接入现有面板；原未适配页面明确关闭，不等于已经功能接通原主界面的全部页面。Providers 已复用原页面和 modal 接入登记模型/账号详情及明确的单阶段草稿选择；新增账号和登录仍未开放。未来阶段计划修订已开放精确 Native preview/apply 通路，新增等待后/回执前Management重查及提交前Store guard，已在原详情旁增加折叠修订区，复用原阶段选择器与全部原 CSS；当前窗口保留未知原receipt并精准重试，实际Wails修订操作仍未验证；预览/应用不得改写已启动角色或重置预算。后续接入须保留现有鉴权和账号隔离。
- 上游发布、Docker 推送与付费 UI preview workflow 已限定只能在 yetone/magpie 运行。

以下保留上游的代码路径说明。

# Notes for coding agents

## Built-in subscriptions that a plugin serves

Some built-in subscriptions are deprecated. Reaching them can break their
vendors' terms, so they are moving out of magpie into community OpenCode
plugins, which keeps magpie itself from being banned. Each one has a plugin
that does the same job:

| Subscription | id | Plugin (npm) | Mover |
| --- | --- | --- | --- |
| Command Code | `commandcode-plan` | `@magpie-community/opencode-commandcode-auth` | `internal/provider/migrate_side.go` |
| Cursor | `cursor` | `@magpie-community/opencode-cursor-auth` | `internal/provider/migrate_side.go` |
| Devin | `devin` | `@magpie-community/opencode-devin-auth` | `internal/provider/migrate_side.go` |
| Factory | `factory` | `@magpie-community/opencode-factory-auth` | `internal/provider/migrate_factory.go` |
| Grok | `grok` | `@magpie-community/opencode-grok-auth` | `internal/provider/migrate_side.go` |
| Kiro | `kiro` | `@magpie-community/opencode-kiro-auth` | `internal/provider/migrate_kiro.go` |
| Xiaomi MiMo | `mimo-app` | `@magpie-community/opencode-mimo-auth` | `internal/provider/migrate_mimo.go` |
| Qoder, Qoder CN | `qoder`, `qoder-cn` | `@magpie-community/opencode-qoder-auth` | `internal/provider/migrate_qoder.go` |
| WorkBuddy, WorkBuddy AI | `workbuddy`, `workbuddy-ai` | `@magpie-community/opencode-workbuddy-auth` | `internal/provider/migrate_workbuddy.go` |
| ZCode | `zcode` | `@magpie-community/opencode-zcode-auth` | `internal/provider/migrate_zcode.go` |
| Zed | `zed` | `@magpie-community/opencode-zed-auth` | `internal/provider/migrate_zed.go` |

The `movers` map in `internal/provider/migrate*.go` is the source of truth.
`TestMovedBuiltinsSayTheirPlugin` fails when a mover has no notice in its
code.

### Which code actually runs

Two cases decide it:

- **Moved:** the user moved the subscription onto its plugin, from the
  editor's "Move to plugin", or by clicking it in the Add sheet before
  signing in (`provider.Adopt`). Its `migrations.json` state is `plugin`
  (`provider.Moved(id)`). This is the default for new sign-ins since
  v0.1.642. A user who installs the plugin themselves is moved too
  (`provider.HandOver`): at once when the built-in has no accounts, from the
  gateway's hourly loop (`KeepRetiringMoved`) when it has, unless they moved
  back or the plugin is signed in under its own `-plugin` id already. Until
  then the Add sheet shows the plugin's tile only (`replacedSub` in app.js).
- **Not moved:** the user is still signed in through the built-in.

For a moved subscription, the plugin does everything: sign-in, refresh,
models, requests, usage and errors. It runs in the plugin host
(`internal/plugin`). The built-in's code in `internal/provider/<name>*.go`,
`internal/gateway/<name>.go`, `internal/zed` and `internal/qoder` doesn't run
for it at all. The gateway sends its requests to the plugin's fetch, not to
the built-in's translator.

So a bug report about one of these subscriptions is usually about the
plugin:

1. Check whether the user's subscription is moved. A moved provider's row in
   `/api/providers` has `move.state == "plugin"`; the provider id is in
   `onPlugins`.
2. Fix the plugin in the community repo,
   [magpie-community/plugins](https://github.com/magpie-community/plugins)
   (`packages/<name>`, provider id = the built-in's id; checked out locally
   at `~/workspace/projects/magpie-commuity-plugins`). Publish a new version.
3. Raise the mover's `min` to that version. `keepMovedCurrent` then updates
   every moved user's plugin.
4. Make the same fix in the built-in only if it should also reach users who
   haven't moved. A fix made only in the built-in does nothing for moved
   users.

This magpie-side code still runs for moved subscriptions:

- the plugin host: `internal/plugin`, `host.js`;
- `internal/provider/plugins.go`, `plugin_usage.go` (`movedCards`) and
  `pluginsignin.go`;
- the move itself: `internal/provider/migrate*.go`;
- the GUI's plugin paths in `internal/gui/assets/app.js`: `subOf`,
  `pluginSubs` and `startPluginSignIn`.

A test against the built-in alone doesn't prove anything for moved users. To
compare the two, use the parity tests: `internal/gateway/plugin_*_test.go`.
