# Fusion on Magpie：轻量 Fork 实施计划
## v1.0 · 2026-10-02 · 可用于编码任务交接

| 项目 | 本计划约定 |
|---|---|
| 实施路线 | 以 `yetone/magpie` 为基座，沿用 Go、现有 GUI/CLI、模型接入与代理链路，增加受控工程工作流 |
| 用户资产 | ChatGPT Pro、Grok Heavy；GLM 已确认 CN Coding Plan + API key + Claude Code，实际权益尚需本人账号验证 |
| 最重要的功能 | 设计与规划、实施与测试、审查与验收分别指定模型；展开后五角色独立设置 |
| 产品范围 | 本人使用，首台 Mac mini 独立环境，单机控制服务与受管子进程 |
| 本次交付 | 实施计划、30 个 Agent 工作包、60 类验收矩阵与机器可读任务清单 |
| 实施状态 | 已导入最新源码与三份文档，已安装 Go 1.26.3；应用增强功能尚未实现，未连接账号；基线验证见独立报告 |
| 进度口径 | 工作包均为 `planned`，验收均为 `not_run`；文档校验不等于产品测试通过 |

> **执行顺序：原版基线与隔离 → 五角色配置与严格锁定 → 三条真实执行路线与手动版 → 有证据的阶段闭环 → 可靠性与部署 → 可选自动路由。**
>
> 先实现“由用户决定谁做什么”，再实现“系统可靠交接”，最后才优化自动选择。

## 1. 本计划与原方案的关系

保留《Fusion_最终实施方案_v1.1.md》的五角色、权限、费用、额度和验收约束。采用随后确定的 **Magpie 轻量 Fork** 路线，以下实现选择替代原方案中相冲突的部分；没有列出的需求继续有效。[E01]

| 原 v1.1 的实施描述 | 本计划的替代决定 |
|---|---|
| Magpie 仅作为参考或后续候选引擎 | Magpie 是主代码基座；不再另造同类网关 |
| 独立 Node.js / Fastify / React 主系统 | 沿用 Magpie Go 后端和现有 UI；Node 仅在原有插件/构建确实需要时保留 |
| 额度优先依赖独立 CodexBar | 优先复用 Magpie 与官方 Runtime 的可信额度数据；CodexBar 用于缺口与对照验证 |
| 独立 Fusion 控制服务与网关再串联 | 增强模块优先在同一 Go 服务内；官方 Agent Runtime 仍可作为独立受管进程 |
| 手动组或会话粘性用于近似锁定 | 生成不可变任务快照，并在真实执行出口强制固定模型/账号/effort/计费路径 |

**保留 Model Gateway 与 Agent Gateway 的语义区分，而不是必须部署两个服务。** 模型接口只做推理；Agent 任务接口才能授权工具执行和写项目副本。

本计划版本是独立的实施计划 v1.0，不是把需求方案回退到 v1.0。原两份 v1.1 文件在本地工作区尚未提供；不重建或冒充原文。已知需求摘录见 `../requirements-summary.md`。

## 2. 成功标准与明确不做的事

### 2.1 第一版必须达到

用户能保存一个执行预设，默认看到三个选择区，展开后得到以下五角色：

| UI 选择区 | 角色 ID | 工作 |
|---|---|---|
| 设计与规划 | `design` | 架构、规划、技术方案、复杂问题调查后的设计 |
| 实施与测试 | `implementation`、`testing` | 修改产品代码；生成/组织测试并分析真实结果 |
| 审查与验收 | `review`、`acceptance` | 检查缺陷与设计偏离；核对固定验收标准与证据 |

具体模型由用户选，不预设“必须 GPT 设计、Grok 实现、GLM 测试”。允许同一个模型多角色、五角色全部指定，或仅开放部分角色为 auto。单纯规划/Review 不运行完整五阶段。[E01]

**用户最终能核对：**本次请求了哪个模型，解析后发送给谁，上游声明了谁，使用哪个账号和计费路径，产物是什么，哪些检查真实执行，哪些未验证。

### 2.2 首版范围收敛

不新建独立前端框架、微服务集群或统一账号转售平台；不同时串联 CC Switch、Cockpit、CLIProxyAPI 和 Magpie；不默认读取浏览器 Cookie 或改写日常 CLI 的全局配置。

不自动 commit、push、merge、deploy、烧录、充值、兑换重置券。产品中的 Agent 权限与开发团队在 Fork 内提交代码的权限分开管理。开发 Agent 也不能因为收到本计划，就取得远端写入或生产账号权限。

Windows 编译 Worker、GitLab/Jenkins 集成、千仓知识提取、跨地区调度都列为后续项目，不进入本次发布必要条件。公司敏感代码、真实硬件、台架与发布密钥不进入开发 fixture。

## 3. 开工前必须知道的源码事实

以下是静态阅读得到的事实，只支撑选取改动位置；不是本机运行结论。

| 事实 | 对实施的直接要求 | 来源 |
|---|---|---|
| `Group.Picked()` 在原选项不再属于组时会选择第一成员；manual 只约束选中成员，不自动冻结全部账号/密钥配置 | 不修改所有普通组语义；给 Fusion locked 增加独立的严格解析和分派路径 | [S02] |
| `Group.Levels` 描述了成员可能使用最近支持的 effort | 用户明确指定 effort 的阶段必须前置验证；不静默就近映射 | [S02] |
| 一些订阅已迁移到社区插件，迁移后登录、刷新、请求、usage 由插件执行 | 基线必须记录实际 plugin host 路径；只测试旧内置实现不够 | [S03] |
| README 说明模型接口、账号 pin、route trace、loopback 宽松鉴权 | 复用接口与记录能力；增强模式必须补强所有入口，不能靠 IP 或客户端 header 授权 | [S01] |
| `go.mod` 已声明 Go 工程与 `modernc.org/sqlite` 等依赖 | 复用已有依赖；不另建 Node 主系统或第二套数据库服务 | [S04] |
| Jev 已有 classification/System One 接入代码 | 后期复用接入而非重写，仍需任务上下文、授权和本地评估 | [S05] |

**当前实施 commit：**`1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`。按用户 2026-10-02 的要求，从上游 main 获取最新源码并在本轮锁定。原方案参考版本为 `4afb69863849b44fa5983a483e36baeb29cd3f39`；两者相差 10 个提交。来源、tree 与文件校验值见 `../upstream-lock.json`，差异与验证见 `../baseline/baseline-report.md`；开发中不自动追随 main。

## 4. 里程碑与交付边界

本计划按依赖和验收推进，不按未经实测的工期承诺推进。真实凭据、服务权限、硬件或供应商限制属于外部 Gate，不以“Agent 能继续写代码”冒充已解除。

| 里程碑 | 工作包 | 用户得到什么 | 退出门槛 |
|---|---|---|---|
| **M0 原版基线与 Fork 隔离** | WP-01–WP-03 | 隔离、可复现的开发环境；无真实业务调用 | 原版可复现，Fork 不污染原环境，fake 环境可稳定注入错误 |
| **M1 阶段合同与严格策略** | WP-04–WP-07 | 阶段快照、服务端鉴权和严格出口的离线证明 | 离线证明 locked、配置快照和鉴权无法被普通组规则绕过 |
| **M2 三路线与手动可用版** | WP-08–WP-18 | Gate A：单阶段受控工作台，三家真实验证，v0.1.0-alpha | 三条路线真实核验；单阶段指定、执行、取消、记录均可用 |
| **M3 五阶段工程闭环** | WP-19–WP-23 | Gate B：设计→实施→测试→审查→验收，固定证据与返工 | 五阶段和一次有界返工可复现，证据与产物版本一致 |
| **M4 可靠性、部署与试点** | WP-24–WP-26、WP-30 | Gate C：安全/恢复/上游同步过关，v0.2.0 | 安全/故障/恢复/上游同步回归通过，用户批准本人试点发布 |
| **M5 可选智能路由** | WP-27–WP-29 | off→shadow→低风险 assist；收益不足保持 off，独立于 v0.2.0 | 数据和费用许可明确，shadow 显示收益后才考虑低风险 assist |

### 4.1 三个发布标签的含义

- **`fusion-v0.1.0-alpha`：手动可用版。** 可以保存五角色预设，执行指定的单阶段工作；阶段之间由用户明确推进。只供本人内测，不承诺生产可靠性。
- **`fusion-v0.2.0`：受控闭环版。** 具备阶段交接、真实验证、独立审查、有限返工、完整安全/恢复与部署记录。
- **`fusion-v0.3.0`：可选智能增强。** 只在用户批准的 auto 阶段引入规则/Jev；可以不发布，前两版不依赖其完成。

模拟验证只能形成开发预览。三条真实路线缺任一条，不能把 alpha 标记成“三订阅接入完成”；其它不依赖真实凭据的工作继续。

### 4.2 关键依赖与可并行位置

```text
WP-01 → WP-02 → WP-03 → WP-04
                         │        ├→ WP-05 → WP-06 → WP-07
                         │        └→ 数据合同冻结
                         │
                         └→ 后续所有行为测试共用 fixtures

合同/鉴权稳定后：
    WP-08 路线核查 ─→ WP-09 额度 ─→ WP-10 准入
    WP-11 Worker ─→ WP-12 Codex
                 ├→ WP-13 Grok
                 └→ WP-14 Claude Code / GLM
    WP-15 API/假 Runtime联调 ─→ WP-16 UI
                     上述合并后 → WP-17 真实核验 → WP-18 Gate A

Gate A → WP-19 工作流 + WP-20 Handoff
       → WP-21 EvidenceGate → WP-22 返工 → WP-23 Gate B
       → WP-24 安全回归 → WP-25 部署 + WP-30 上游同步
       → WP-26 Gate C / v0.2.0

Gate C → WP-27 auto规则 → WP-28 Jev shadow → WP-29 可选assist
```

这是阅读用依赖摘要，精确前置条件以 `implementation_tasks.json` 的 `depends_on` 为准。GUI、额度与 Runtime 可在合同稳定后分不同 worktree 并行；数据库迁移、鉴权入口、网关出口这三类共享改动由指定集成维护者串行合并。

## 5. 30 个工作包总览

每个工作包的详细输入、范围、步骤、交付物、验收与停止条件见《Fusion_Magpie_Fork_Agent工作包_v1.0.md》。工作包不要求一次性做完；一包一个可独立审查的变更，过大时只拆子包，不扩目标。

| ID | 工作包 | 依赖 | 主要负责人 |
|---|---|---|---|
| WP-01 | 冻结上游与建立原版基线 | 无 | 架构/基线 Agent |
| WP-02 | 建立轻量 Fork 的身份与隔离边界 | WP-01 | 集成维护 Agent |
| WP-03 | 建立离线假上游与假 Runtime | WP-01, WP-02 | 测试 Agent |
| WP-04 | 定义五角色与阶段绑定解析合同 | WP-03 | 架构/合同 Agent |
| WP-05 | 任务快照、事件与版本化存储 | WP-04 | 后端 Agent |
| WP-06 | 强制入口鉴权与阶段调用身份 | WP-02, WP-04, WP-05 | 安全 Agent |
| WP-07 | 严格路由出口与上游行为隔离 | WP-04, WP-06 | 网关 Agent |
| WP-08 | 建立三订阅路线及真实接入核查清单 | WP-02, WP-03, WP-04, WP-06 | 接入 Agent + 用户 |
| WP-09 | 额度标准化与来源保真 | WP-03, WP-04, WP-08 | 额度 Agent |
| WP-10 | 执行准入与单机调度 | WP-05, WP-07, WP-09 | 调度 Agent |
| WP-11 | 受管 Worker 隔离、取消与恢复 | WP-03, WP-05, WP-06 | Runtime 基础 Agent |
| WP-12 | 实现 Codex 官方 Runtime Adapter | WP-08, WP-11 | Codex 接入 Agent |
| WP-13 | 实现 Grok Build 官方 CLI Adapter | WP-08, WP-11 | Grok 接入 Agent |
| WP-14 | 实现 GLM / Claude Code Runtime Adapter | WP-08, WP-11 | GLM 接入 Agent |
| WP-15 | 任务 API、阶段预览与事件流 | WP-04, WP-05, WP-06, WP-10, WP-11 | API Agent |
| WP-16 | 阶段选择 UI 与单阶段工作台 | WP-04, WP-06, WP-15 | 前端 Agent |
| WP-17 | 三条路线的真实受控 Smoke Test | WP-07, WP-09, WP-10, WP-12, WP-13, WP-14, WP-15, WP-16 | 验证 Agent + 用户 |
| WP-18 | 手动可用版 Gate A | WP-01, WP-02, WP-03, WP-04, WP-05, WP-06, WP-07, WP-08, WP-09, WP-10, WP-11, WP-12, WP-13, WP-14, WP-15, WP-16, WP-17 | 独立验收 Agent + 用户 |
| WP-19 | 精简工作流与设计检查点 | WP-18 | 工作流 Agent |
| WP-20 | 阶段产物、工作区交接与版本一致性 | WP-11, WP-18 | 工作区/Handoff Agent |
| WP-21 | 真实验证和 EvidenceGate | WP-19, WP-20 | 验证工程 Agent |
| WP-22 | 审查、返工与验收闭环 | WP-19, WP-20, WP-21 | 工作流 Agent |
| WP-23 | 五阶段端到端验收 | WP-19, WP-20, WP-21, WP-22 | 独立测试/验收 Agent |
| WP-24 | 安全、故障和原版兼容回归 | WP-18, WP-23 | 独立安全/回归 Agent |
| WP-25 | Mac mini 部署、备份与回滚 | WP-24 | 运维 Agent + 用户 |
| WP-26 | 个人项目试点与受控闭环发布 | WP-23, WP-25, WP-30 | 用户 + 独立验收 Agent |
| WP-27 | 受限 auto 阶段与纯规则路由 | WP-07, WP-09, WP-10, WP-23, WP-26 | 路由 Agent |
| WP-28 | Jev 旁路评估 | WP-26, WP-27 | 评估 Agent + 用户 |
| WP-29 | 低风险 assist 灰度与退出开关 | WP-24, WP-28 | 用户 + 路由/验收 Agent |
| WP-30 | 上游同步与长期维护演练 | WP-02, WP-24 | 集成维护 Agent |

## 6. 改代码的位置与边界

### 6.1 尽量复用的部分

已有 `internal/provider`、`internal/gateway`、`internal/plugin`、`internal/gui/assets` 的真实路径，以固定 commit 的代码与 `AGENTS.md` 为依据。[S02][S03] 供应商接入、协议转换、模型目录、用量记录和现有 GUI 尽量沿用；不要为了新增工作流先重构整个网关。

**拟新增结构**如下，均不是当前仓库已具备的功能：

```text
internal/fusion/
  stageplan/      五角色、继承规则、冻结与版本
  policy/         权限、锁定、费用和数据准入
  routes/         将 Magpie/原生 Runtime 映射为版本化 route
  task/           状态、调度、事件、检查点和 fencing
  runtime/        进程公共层 + codex/grok/glm Adapter
  workspace/      副本、单写所有权、固定 artifact revision
  evidence/       验证执行器、报告与 Handoff
  store/          独立 fusion.db、事务与迁移
  api/            任务/配置 API 注册与 DTO
  workflow/       后续阶段推进与有界返工
  quota/         已有采集结果的语义适配
  assessor/      后续规则/Jev；默认无外部调用
  testsupport/   离线假服务、假 Runtime、记录型传输

docs/fusion/
  baseline/ contracts/ decisions/ integration/
  releases/ runbooks/ upstream/
```

这些是同一个 Go 产品内的模块，不是要求每个目录生成独立服务、包管理工程或大量单函数接口。确有简化收益时合并小包，保持测试边界即可。

### 6.2 上游主链路只增加少量接缝

**UI/API 接缝**：注册任务、阶段配置、额度状态和事件页面，不让前端直接写供应商配置。

**执行出口接缝**：请求带服务端解析好的 `ExecutionTarget` 后，在候选排序、重试、认证附加、插件 fetch 之前强制检查；锁定目标不能被后面的普通逻辑再改变。

**事件接缝**：将现有 request/usage/route 关联到 `task_id + stage_run_id + attempt`，保留原始来源，不把缺失值补成假证明。

复用 Magpie GUI 选择器不等于复用其所有配置副作用；增强任务 Worker 的登录、配置和工具权限与日常客户端隔离。

## 7. 在 M1 一次定清的合同

### 7.1 选择、解析和快照

| 项目 | 必须实现的语义 |
|---|---|
| `locked` | 具体 route/model，默认不允许换模型、账号、workspace、凭据身份或计费路径 |
| `auto` | 必须给批准的候选路线；在阶段/attempt 边界选择一次后冻结 |
| `inherit` | 只存在于未解析配置；启动前必须解析为 locked 或 auto |
| 合并优先级 | 本次任务 > 项目 > 全局；按完整角色绑定替换，禁止字段级乱拼 |
| UI 合并区 | 先展开五角色；不同值重新合并时明示覆盖 |
| 预设 | 版本化；应用时展开，不在运行中持续引用 latest |
| 变更 | 未开始阶段可经 If-Match 形成新 revision；正在执行必须先安全暂停后新 attempt |
| 失效 | 选中模型被移除、effort 不支持、route 身份改变立即 blocked，不能回第一组成员 |

下游仅携带 server-issued capability，不接受客户端自报 `role=acceptance` 获得权限。普通用户 API 与 Worker API 权限不同；Worker 不持有管理 Key，不能绕过阶段接口重新创建无限任务。

### 7.2 三种信息不要混淆

`requested_model` 是用户期望；`resolved_model` 是实际构造请求的目标；`upstream_reported_model` 是上游响应自报。没有自报值就保持未知。锁定 route 不等于独立证明远端使用了哪些模型权重。[E01]

`lock_enforcement` 记录 `controlled_calls / primary_only / unverified`。其中 `controlled_calls` 仅指本系统能控制/观测的 Agent 发起模型调用，不宣称控制供应商内部实现。

严格全阶段要求下，不能控制摘要或子 Agent 模型的 Runtime 必须禁用相关功能并验证，或者阻止该路线。只有用户明确接受 `primary_only` 的任务，才可在限制清楚的情况下运行；不能把它默认当成严格模式通过。

### 7.3 状态与持久化

任务目标、阶段计划版本、StageRun attempt、原生 session ID 分开。任务完成、硬验证通过、模型建议通过、人工接受分别记录；终态至少能表达 failed、cancelled、interrupted/needs_review 和 advisory_only。

事件顺序、lease 与 generation 持久化。启动子进程前记录意图，重启先对账再继续。数据库事务不能证明供应商请求或文件副作用“恰好一次”；写后失联先检查现场，不盲目重跑。

## 8. 三条路线如何接入和验证

### 8.1 推荐优先验证的路线

| 资产 | 优先候选 | 验证要点 |
|---|---|---|
| ChatGPT Pro | 官方 Codex App Server，由本人登录的独立 home | 模型/effort、账号/workspace、子调用范围、quota、原生状态和取消 [S06] |
| Grok Heavy | 官方 Grok Build headless CLI | 版本支持的结构化输出、会话、sandbox、费用与取消；不用任意 xAI API Key 代表 Heavy [S07] |
| GLM Coding Plan（CN） | 用户指定 Claude Code，独立 API key 配置与 Anthropic Messages 端点 | 实际 key/套餐权益、模型、受控子调用、取消/恢复和费用归属；不暗换普通 API [S11] |

官方 Runtime 可直连供应商，不要求所有请求二次穿过 Magpie 的订阅代理。若采用 Magpie/插件的模型请求路径，则必须通过严格出口验证；不满足者仍禁用。控制和记录共用，执行路线分别准入。

### 8.2 四张表作为真实接入结果

**账号/费用表**：账号别名、workspace、region、认证拥有者、套餐原始值、计费策略、有无 on-demand、验证日期，不包含明文凭据。

**能力表**：具体模型/effort、输入与工具能力、Runtime/插件版本、锁定等级、实际输出证据；模型列表可见不等于推理权限已验证。[S06]

**额度表**：来源、窗口单位、quota pool、原始值、时间、状态。读取成功不意味着该用途的生成也获准。

**执行表**：初始化、只读任务、获准写入、取消、会话恢复、错误和配置更新行为。失败/中断不能凭一段成功文本覆盖。

### 8.3 无凭据的开发继续方式

凭据由用户在宿主按官方流程录入；不粘贴到任务清单、文档或聊天。WP-08 可先产出验证清单，WP-12–WP-16 可用 fixtures 实现；WP-17 的真实项目保持 `awaiting_user`。这不阻断独立代码开发，但阻断三订阅功能的最终验收。

## 9. 额度、费用与分类器准入

采集优先复用现有数据，不为查询 quota 发生成请求。缓存必须有 provider/account/workspace/region/pool 身份；共享池不按实例数重复计算。保留 `observed_at`，不能拿 HTTP 刚返回的时间冒充额度刚刷新。

**unknown、stale、auth_required、unsupported 与真实零额度分别处理。** 预算只记录订阅/credits/API 真实来源；Grok 的按需预算比例不能直接写成 Heavy 剩余额度；GLM 的 MCP 配额不等于推理配额。[E01]

首版策略默认：一个共享 pool 最多一个主动模型任务，全局最多两个受管执行；过期快照先有界刷新，失败则阻断新自动派单。同一任务的重试、子 Agent 和返工合并计算预算。

`allow_paid_fallback=false` 是默认值，但必须确认原生工具自身不会在套餐耗尽后自动使用付费 credits/on-demand。无法确认时是 `billing_unverified`，不是“已保证零新增费用”。

Jev 的 off/shadow/assist 后置于 M5。全 locked 不调用分类器；单独批准的 shadow 实验除外。只有一个经批准的分类负责人，不能 Fusion 调一次、Magpie 再调一次。即使 Jev 高置信度，也不能覆盖 locked、风险下限或数据权限。

## 10. 阶段闭环与真实证据

### 10.1 推荐的 change 路径

```text
提交 + 预览冻结计划
    ↓
设计：目标/范围/接口/验证标准
    ↓ 默认等用户批准设计与写入范围
实施：指定模型，在受控项目副本中修改
    ↓ 固定 artifact revision
测试：指定模型组织测试，可信执行器运行
    ↓ 测试集合/命令/环境/报告与代码 hash 绑定
审查：指定模型对固定产物只读检查
    ↓
验收：指定模型核对标准与证据；EvidenceGate 独立判断硬条件
    ↓
人工接受/退回；无自动 push、merge、发布或烧录
```

审查有问题，返回固定的实施模型；测试仍由固定测试角色安排，复查与验收保持原绑定。首版自动返工最多一个回合；超出保留现场并等待用户，不换角色名称后重新获得次数。

### 10.2 不把测试产物当成可信证据来源本身

执行器记录命令、退出码、工具版本、运行环境、工作区 artifact hash、test-suite hash、报告 hash 和解析状态。测试代码本身按不可信代码运行，不能得到供应商密钥、管理 Key、生产设备或任意网络权限。

Agent 可以生成测试，但不能自行把构造的一份 JUnit 文件登记为已执行报告；验证记录必须来自受管执行器。测试为零、测试标准被删改、报告截断和不匹配 artifact 都有明确失败/未验证状态。

对嵌入式项目保留协议、寄存器、时序、硬件约束等必要注释；不得照搬通用“删注释”规则。没有 IAR/GHS、HIL/WCET 或硬件条件的结果只能标未验证，不能发布就绪。

## 11. 开发执行方式与 Agent 分工

### 11.1 开发项目本身的角色

| 开发角色 | 负责 | 不可代替谁 |
|---|---|---|
| 架构/合同 Agent | 五角色、接口、状态与改动边界 | 不替用户决定实际付费模型或数据外发 |
| 实施 Agent | 当前一个 WP 的代码与最小验证 | 不自改验收标准、不扩大到未来阶段 |
| 测试 Agent | 独立 fixture、负面测试、回归 | 不只重复实施者的“已通过”文字 |
| 审查 Agent | 检查 diff 与上游兼容、权限与失败分支 | 不自动改代码或 merge |
| 集成维护者/用户 | 合并冲突、阶段 Gate、真实账号授权与发布 | 不用模型投票替代硬验证 |

以上是**开发这个产品的分工**，与产品运行时的五个角色是两个层次。各角色实际使用哪个模型仍由用户选择；没有指定的模型名不自动补入。

### 11.2 一包一次交接

每个 WP 在独立 worktree/分支中执行。共享接口由架构/集成维护者统一；UI、额度和不同 Runtime 可按稳定合同并行。无需先搭建另一个多 Agent 调度系统来开发 Fusion。

每个包结束固定提交以下内容：

```text
WP-xx/
  summary.md              目标、实际改动、偏离与风险
  changed-files.txt       修改范围及必要性
  patch.diff              待审查补丁或授权后的 commit 引用
  test-results.json       测试ID、命令、退出码、证据路径；未跑明确记录
  artifacts.json          产物hash、输入base、版本
  blockers.md             阻断项与其影响，不伪造解决
  handoff.md              下一包需要知道的接口与未完成项
```

`done` 只能由验收过的交付物支撑。工作包状态流为 `planned → ready → in_progress → review → done`，也可以 `blocked`；测试状态为 `not_run/pass/fail/blocked/not_applicable`，N/A 必须写适用性理由且不能用于跳过本版本核心约束。

### 11.3 提交与合并约束

一包一个小 PR/MR 或补丁；不设没有依据的“少于 100 行”门槛。测试、实现和必要文档尽量可一起审查。若未授权自动 commit/push，只提交补丁和验证产物。不得直接修改 protected main、强推共享分支、自动生成 release。

## 12. 验收门槛与测试执行层次

《验收矩阵》保留 v1.1 的 T01–T45 原场景，新增 T46–T60，全部映射到工作包；当前没有运行任何产品测试。[E01]

### 12.1 四层证据

| 层次 | 证明什么 | 不能证明什么 |
|---|---|---|
| 纯函数/合同测试 | 继承、版本、候选、状态和确定性拒绝规则 | 本人的订阅权益 |
| 假上游/假 Runtime 集成 | 请求实际目标、0次越权外发、故障、取消与恢复 | 真实供应商兼容和扣费 |
| 本机真实 Runtime Smoke | 指定版本/账号/模型的一次真实行为 | 永久有效、未测试模型或所有隐藏行为 |
| 真实项目副本试点 | 目标使用环境中的可接受结果与问题 | 生产设备安全、合规认证或统计上的零风险 |

### 12.2 Gate A：手动可用版

必须完成 M0–M2 的交付与实际适用测试；三条路线分别有真实验证；所有指定阶段不暗换模型/账号/effort/计费路径；读写、取消、SSE、预设版本、入口鉴权及锁定失效可重复演示。

M5 尚未启用的 Jev 实际调用测试可以标“本版未启用”，但必须验证 off 时无外发、locked 不被覆盖。不能用“Jev 未实现”跳过锁定约束。

### 12.3 Gate B：闭环功能完成

五阶段及有限返工通过假上游全矩阵，并有获准真实账号的合成仓库端到端记录。测试失败不可被验收意见改成通过；产物或标准更新后旧证据标 superseded；不能仅显示五个绿色步骤就算完成。

### 12.4 Gate C：受控发布

安全、取消/恢复、权限边界、原版兼容、备份/回滚、上游同步演练均无未关闭阻断问题。用户确认本人项目试点的结果与剩余限制后发布。

下列任一出现，一律阻断：越权或秘密泄露、锁定目标被覆盖、未知费用被自动启用、写进程停止无法确认、测试证据伪造/错配、DB 恢复后双写。

## 13. 部署和维护交付

首台 Mac mini 运行 Go 服务、所需的官方 Runtime 和可选额度采集器。默认只对 loopback 监听，跨设备访问使用经过验证的私有加密入口；网络可达不代表有项目权限。增强版所有管理与数据入口必须经 WP-06/WP-24 验证，不能继承“本地任意 key 均可用”。[S01]

服务使用独立数据根目录（设计示例 `~/.fusion-magpie`，不是已有 Magpie 配置项）、服务名和更新标识；开发版关闭云同步、插件自动升级与原版自更新。宿主认证按官方流程完成，不复制整份日常 HOME/Keychain。

DB 只在本地一致性备份；NAS 保存脱敏或加密归档，不作为多进程共享在线数据库。回滚时同时考虑 binary、schema、插件、策略和任务状态，先停写并核对在途进程再恢复。

上游维护固定流程：`锁定版本 → 独立 sync 分支 → diff 分析 → 原版回归 + T46–T60 → 单账号受控探针 → 用户批准替换`。不得在长任务中热升级插件/运行时或重新解释 route revision。

## 14. 阻断项与决策负责人

| 阻断项 | 负责人 | 不等待也能继续的工作 | 禁止替代行为 |
|---|---|---|---|
| 没有目标 Fork 远端/写权限 | 用户/仓库管理员 | 本地隔离 clone、基线分析、补丁和测试 | 猜用户名创建远端、直接向 upstream 提交 |
| 缺本人登录或实际模型选择 | 用户 | fixtures、合同、UI、fake Runtime | 把示例 route 变成真实默认、读取别的客户端凭据 |
| GLM 套餐/地区/用途不明确 | 用户/供应商，接入 Agent 记录 | Adapter 开发、其余两路核验 | 借另站文档换 host、暗换按量 API |
| 原生 Runtime 子模型不可控制 | 架构/接入 Agent，用户决定 scope | 主模型能力记录、其它可控路线 | 宣称全阶段严格锁定；隐藏 primary_only |
| 插件可绕过账号/模型锁定 | 安全/网关 Agent | 禁用插件路线，用获准原生 Runtime | 凭主网关日志声称插件内部也被控制 |
| quota 长期不可查询 | 额度 Agent/用户 | 界面和错误处理；静态未验证路线 | 将 unknown 写成100%、重复轮询刷接口 |
| 没有编译器/测试台架 | 验证 Agent/用户 | 只读/合成项目测试、代码审查 | 伪造真实编译、HIL、WCET 通过 |
| Jev 没有稳定收益 | 用户/评估 Agent | 已交付手动版和纯规则模式 | 为路线图完整强制启用自动分类 |

## 15. 第一轮应交给 Agent 的范围

**第一轮只做 WP-01–WP-03，不直接实现全部产品。** 先建立可重复的原版基线、独立 Fork 环境和假上游，这是之后讨论“没有暗换模型”的证据基础。

期望收到：上游锁文件、原版构建/测试报告、真实执行路径地图、Fork 隔离清单、假上游与假 Runtime、M1 的最小接缝建议。没有凭据不阻止这些交付。

下一轮才做 WP-04–WP-07 的严格合同和策略；其验收不是画完三个下拉框，而是：**修改组、删模型、改变 effort、伪造账号、重试和插件分派都不能让锁定目标悄悄改变。**

编码启动提示词、每包任务卡、全部测试与 JSON 依赖清单已放入同一交付包。提示词的范围限制需要保留，不能只给 Agent 一句“照着方案全部做完”。

## 16. 本次核查范围与资料

原计划编制时读取已有 v1.1 文档与 Magpie 固定 commit 的 `AGENTS.md`、`internal/provider/group.go`、`go.mod` 等，在线核对了官方 Runtime 文档。未运行 Magpie 或任何账户探针；不是完整源码安全审计。

**[E01] 需求基线**：《Fusion_最终实施方案_v1.1.md》§9、§14、§19 与简化说明；以及用户随后确认的 Magpie Fork 路线。需求文档保留，技术实施冲突按本文 §1 调整。

**[S01] Magpie README**：接口、账号 pin、route trace、loopback、GUI/CLI 等产品说明。README 页面为在线核对资料，执行基线仍锁定下述 commit，不混称为同一最新版本。
`https://github.com/yetone/magpie`

**[S02] Magpie Group**：manual、Picked fallback、Levels 与规则行为。
`https://github.com/yetone/magpie/blob/4afb69863849b44fa5983a483e36baeb29cd3f39/internal/provider/group.go`

**[S03] Magpie AGENTS.md**：迁移后的插件执行路径及测试边界。其订阅支持风险是仓库作者的说明，不构成本文对服务条款的法律结论。
`https://github.com/yetone/magpie/blob/4afb69863849b44fa5983a483e36baeb29cd3f39/AGENTS.md`

**[S04] Magpie go.mod**：沿用 Go 与已声明依赖；具体工具链在 WP-01 核验，不能凭运行机器当前版本覆盖。
`https://github.com/yetone/magpie/blob/4afb69863849b44fa5983a483e36baeb29cd3f39/go.mod`

**[S05] Magpie Jev 连接**：前序对话已核对的同基线代码，本次沿用其接入事实，不宣称完成质量评估。
`https://github.com/yetone/magpie/blob/4afb69863849b44fa5983a483e36baeb29cd3f39/internal/gateway/classify.go`
`https://github.com/yetone/magpie/blob/4afb69863849b44fa5983a483e36baeb29cd3f39/internal/gateway/decide.go`

**[S06] OpenAI Codex App Server / 模型访问验证**：结构化会话、运行事件与配置；模型目录不是权益证明。原文可能重定向至官方 ChatGPT Learn。
`https://developers.openai.com/codex/app-server`
`https://developers.openai.com/siwc/token-sharing-open-source/codex-app-server`

**[S07] xAI Grok Build Headless & Scripting**：CLI 自动化入口与结构化输出。
`https://docs.x.ai/build/cli/headless-scripting`

**[S08] Z.ai Coding Plan FAQ 与 Codex 集成**：候选工具路径和使用范围；不能自动推导本人 CN 套餐适用性。
`https://docs.z.ai/devpack/faq`
`https://docs.z.ai/devpack/tool/codex`

前序 CC Switch、Cockpit、CodexBar、pstack 的细节来源继续以 v1.1 附录为核查记录。实施如选择复用代码/插件，必须在实际采用 commit 上再次确认许可证、适用范围、接口与测试。

---

**计划完成，不代表执行完成。** 下一阶段的授权可以逐工作包进行；任何真实账号导入、外发、付费或部署都在对应 Gate 单独记录。

**[S11] 中国大陆 GLM Coding Plan / Claude Code 接入（2026-10-03 核对）**：用户指定 Claude Code 与 API key；按 CN 官方工具范围及 Anthropic 端点实施，具体合同见 [GLM 接入决定](../decisions/glm-coding-plan-api-key.md)。WP-14 复用 WP-11 公共进程层，独立适配 Claude Code 协议，不依赖 WP-12 的 Codex RPC。
