# 实施前审阅记录

日期：2026-10-02。范围：当前仓库的实施计划、全部 WP-01–WP-30、T01–T60、派生 JSON、需求摘录及基线证据。本记录是开工前审阅，不是产品验收报告。

## 当前证据

- 源码固定为 Magpie `1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`。Go 应用实现及 go.mod/go.sum 与该基线相同，Fusion 产品模块尚未实现。
- 本机 Go 为 `go1.26.3 darwin/arm64`；已安装 Codex CLI `0.160.0`、Grok Build `1.0.48 (b94d5072c95f)` 和 CodexBar `0.70.0`。版本和 help 检查没有运行模型、查询账号或证明套餐可用。
- 基线记录中的依赖完整性、CLI/GUI 编译、vet 和五个 targeted test 包均有 exit code 0；六份日志的 SHA256 与记录一致。本次未重复运行未发生变化的检查。完整测试套件和 GUI 交互尚未验证。
- 30 个 WP 和 60 个验收场景族编号完整，工作包依赖无环。所有 WP 仍为 planned，Fusion 验收仍为 not_run；关联场景不能仅凭原版测试改成 pass。
- `origin` 为本人的公开 GitHub 仓库 `kvvipa0028/FusionGateWay_Claude`，当前登录具有 push 权限。`upstream` 为只读来源，push URL 为 DISABLED。提交和推送开发成果已由当前用户任务授权。
- 原基础准备已在 `fusion/implementation` 形成本地 commit `7abfcb3574d79c50c599df841013001121c24c50`；首次推送被 GitHub Push Protection 拒绝，尚未推送成功。新出现的 `.DS_Store`、`docs/.DS_Store` 不属于源码或实施交付，不加入提交、不删除。
- 全量 cached diff 的 whitespace 检查报告 `internal/provider/zcode_team.go:385: new blank line at EOF`。这是导入的上游原文问题，保持原版，不能将全仓 diff 检查写成无问题。

## 开始实现前的确认事项

以下问题已一并提交给用户。答案到达前不开始依赖这些决定的产品实现；认证信息不写入聊天、计划、证据或 Git。

1. **需求基线：已确认。** 用户于 2026-10-03 回复“我逐一回答，1，是。”，确认以当前仓库实施计划、工作包、验收矩阵和需求摘录作为本次完整需求基线，覆盖全部 30 个工作包及 60 类验收。未取得的原 v1.1 / ZIP 不作为等待开工的前置输入；缺少原件时，仍不能声称已核验 T01–T45 与原文逐字一致。其余四项按用户后续逐项答复记录。
2. **真实三路验证：部分已确认。** 用户于 2026-10-03 回复“2，openai和x我可以给你登录授权，glm是中国大陆地区套餐。”，说明可提供 OpenAI / X 登录授权，并确认 GLM 为 CN 套餐。实际官方登录及三路核验尚未执行；GLM 的具体套餐名称、受支持编码工具待补充。额外付费默认禁止，无法验证原生 on-demand 行为时保持 billing_unverified，不发生成探针。
3. **部署与项目边界：已确认。** 用户于 2026-10-03 回复“本机操作，一个文件夹作为项目”，确认使用当前本机进行部署、运行与验证，项目以单个本地文件夹为边界。按此决定，WP-25 的宿主由原计划中的 Mac mini 改为当前 macOS/arm64 本机，采用本地操作与默认 loopback 访问。Agent 选定默认试点目录为 `/Users/zhaojianzhi/Desktop/Fusion Gateway/FusionGateWay_Claude/.fusion-dev/pilot-project`，在对应工作包实施时建立；这是本地默认值，不是用户提供的现有项目路径。目录位置无需另行确认。文件夹本身不要求预先具有 Git 仓库，执行副本和产物版本仍按计划管理。当前尚未部署、建立试点内容或执行真实个人项目试点；空目录或合成项目不作为真实项目试点通过证据，实际覆盖在 WP-26 报告中记录。
4. **Jev 范围。** 确认保持 off 并仅做功能/离线验证，或授权真实 shadow 评估及其数据/认证/预算。assist 仍需要评估证据和用户接受，不能因路线图要求默认开启。
5. **上游 OAuth 常量与推送。** 首次实际推送被 GitHub GH013 / Push Protection 拒绝：`internal/provider/google.go:72/73/81/82` 的两组 OAuth client ID/secret。已按字节核对该文件与固定上游完全相同，没有读取用户认证文件。请选择移出硬编码并配置外部客户端，或明确授权本次保护例外。前者影响 Gemini CLI / Antigravity 订阅登录与刷新，必须补充缺配置拒绝及已有行为回归；Codex/Grok/GLM 的需求不变。源码与历史中的命中都需处理，不能只新增一个删除常量的 commit 后重推。当前没有改 OAuth 实现、修改保护规则、申请例外或强推。

Google 的 [桌面 OAuth 文档](https://developers.google.com/identity/protocols/oauth2/native-app) 说明安装式客户端不能保持 client_secret 机密；这不替代 GitHub 对本次推送的放行，也不证明 Fusion 获准以第三方客户端身份登录。这里仅记录来源与需要决策的兼容性影响，不保存常量值。

## 已定的执行规则

- 当前任务要求实现完整文档，因此旧启动提示词中的“第一轮仅 WP-01–WP-03”不限制本次总体范围；每包仍按依赖和验收推进。
- 产品 Agent 的 commit/push/merge/deploy 权限与本次开发授权分开。默认产品不能自动提交、推送、发布或烧录；本次开发按工作包形成 commit 并推送 origin，不自动发布 release 或部署生产。
- 保持锁定的源码基线。预审时远端 main 已推进至 `bb42b476637d378981f633175fd48861381a3ef1`；不自动覆盖已核验快照。后续按 WP-30 的独立同步分支、差异分析和重新验证处理。
- 复用 Go、现有 HTML/JS UI 和已有 SQLite 依赖；不新增主系统框架，不改全仓 module path。
- 每个工作包形成独立可审查提交和交付记录。使用工作分支/独立工作区，不在 protected main 上实施或强推；GitHub 平台意义的 Fork 关系不是执行前提，版权及来源仍必须保留。
- `locked` 按完整角色绑定冻结 model/account/workspace/credential identity/effort/billing/route revision；不把 Group.Picked()、最近 effort 或普通重试当作严格锁定。
- 无法控制摘要/子 Agent 调用的 Runtime 默认 blocked。controlled_calls 只覆盖可控制和观察的 Agent 调用，不能承诺供应商内部权重或隐藏内部实现。
- 阶段 token 由服务端签发并绑定 task/stage/attempt/generation/project/audience；Worker 不持有管理密钥。强鉴权须覆盖模型、管理、SSE、旧 API、WebSocket 和 native bridge 接缝。
- quota unknown/stale/auth_required/unsupported/真实零值分别处理，共享 pool 不重复计额；原生套餐和费用没有证据就不能放行真实任务。
- DB 意图、lease 和 generation 不能证明文件副作用恰好一次。失联写任务先核对，取消确认停止后才释放写权限；不把终止进程当作文件回滚。
- 测试和审查绑定固定 artifact、test-suite、命令和报告 hash；模型意见、硬检查结果和人工接受分别记录。测试变更或产品修改使旧证据 superseded。
- 现有通过记录只用于相同基线/输入。每包新增行为用相应 targeted test 和编译验证；公共合同、状态和安全边界增加 regression，最终 Gate 运行其所需完整检查。

## 共享合同预检

下表把生产者与消费者放在同一行；具体依赖来自 implementation_tasks.json。尚未存在的实现接口在生产包冻结后，消费者才能据其完成验收。

| 生产者 → 消费者 | 共同合同 | 预检结论 |
|---|---|---|
| WP-01 → WP-02/WP-03/WP-18 | 固定来源、构建环境和基线失败 | 使用现有真实日志；不能把准备阶段当成全部 M0 通过 |
| WP-02 → WP-03/WP-06/WP-08/WP-30 | 产品目录、端口、更新与插件身份 | 目前只隔离验证环境，应用默认行为仍需实现和共存测试 |
| WP-03 → WP-04/WP-08/WP-09/WP-11 | fake HTTP/CLI/RPC、事件和越权调用计数 | fixture 只供开发验证，不能代替真实套餐或原生终态 |
| WP-04 → WP-05/WP-06/WP-07/WP-08/WP-09/WP-15/WP-16 | 完整角色绑定、route revision、capability 与模型能力 | 先统一解析语义，再实现存储/API/UI；不做字段混拼 |
| WP-05 → WP-06/WP-10/WP-11/WP-15 | Task、PlanRevision、StageRun、事件、lease、generation | 幂等提交、If-Match、启动意图与恢复必须共享持久合同 |
| WP-06 → WP-07/WP-08/WP-11/WP-15/WP-16 | 管理身份、stage capability、入口覆盖 | 原版 identifyCaller 的 local 宽松分支与 webGuard 的 query key 不能直接复用为 Fusion 授权 |
| WP-07 → WP-10/WP-17/WP-18/WP-27 | strict ExecutionTarget、重试与插件出口 | 每次实际分派再次检查，锁定请求不能二次分类或 fallback |
| WP-08 → WP-09/WP-12/WP-13/WP-14/WP-17 | 已登记账号/地区/费用、Runtime 能力、版本 | 官方 Runtime 和迁移后的社区插件是不同路线；能力声明逐路径核验 |
| WP-09 → WP-10/WP-17/WP-27 | pool 身份、观测时间、真实单位、刷新 generation | quota 不是通用百分比，旧账号的迟到结果必须丢弃 |
| WP-10/WP-11 → WP-12/WP-13/WP-14/WP-15/WP-17 | 准入、并发预算、进程生命周期、cancel/resume | 子调用和返工计入 task；暂停/取消/恢复需要真实停止与 fencing 证据 |
| WP-12 → WP-14 | 共用 Codex 协议层与独立认证/计费 | 可以共用 RPC，实现不能继承 ChatGPT home 或把 GLM 调用记到 ChatGPT |
| WP-15 → WP-16/WP-17 | 预览提交一致、版本修订、幂等、SSE event sequence | UI 不能自行拼账号/密钥/授权字段；断线重连只补读事件 |
| WP-17/WP-18 → WP-19/WP-20/WP-24 | 三路真实证据与 Gate A | 任一路未验证则 Gate A 不能通过；独立的离线实现仍可继续 |
| WP-19/WP-20 → WP-21/WP-22/WP-23 | 设计批准、Handoff、artifact revision、写入所有权 | “继续”绑定任务及批准范围；readonly reviewer 只看固定快照 |
| WP-21/WP-22 → WP-23/WP-24/WP-26 | 硬证据、返工次数、人工接受 | 测试失败不能被 LLM 投票覆盖；首次加一次补救仍受同一预算 |
| WP-23/WP-24 → WP-25/WP-26/WP-27/WP-29/WP-30 | 功能/安全/恢复回归及能力声明 | fake、真实 Runtime、实际宿主、个人项目证据分别记录 |
| WP-25/WP-30 → WP-26 | 一致性备份、schema/配置/插件版本、受控同步 | 不兼容回滚先停写并恢复整体；不能只更换 binary |
| WP-26/WP-27 → WP-28/WP-29 | 批准候选、风险下限、off/shadow/assist | M5 是后置可选能力，不影响 locked，也不能自动外发评估数据 |

## 源码核对依据

- `internal/provider/group.go:60`：Picked 在选中成员失效时选择首成员，属于原版行为，Fusion strict 需单独阻断。
- `internal/gateway/lan.go:213`：identifyCaller 对 local 请求保留无认证调用路径，强入口鉴权必须在增强模式覆盖。
- `internal/gui/web.go:155`：webGuard 接受 URL 的 `k` 并转 cookie，不能作为禁止 URL key 的 Fusion 登录方式。
- `AGENTS.md` 的迁移说明：Moved provider 由社区 plugin host 执行；旧内置测试不能证明当前插件出口可控。
- 本地 CLI help：Codex App Server 提供 schema 生成入口；Grok 提供结构化输出、model、reasoning-effort、sandbox、no-subagents 和独立 leader socket 参数。只说明接口存在，不证明实际隔离、恢复或锁定效果。

审阅清单及文档 hash 见 [preflight-inventory.json](preflight-inventory.json)，首次推送的脱敏结果见 [push-protection-report.json](push-protection-report.json)。第五项的具体影响与验证范围见 [OAuth 处理方案](decisions/oauth-push-protection-options.md)。用户答案到达后补充本记录，再开始对应工作包。
