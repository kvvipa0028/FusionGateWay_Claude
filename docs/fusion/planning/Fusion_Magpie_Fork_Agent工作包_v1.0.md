# Fusion on Magpie：Agent 工作包
## v1.0 · 2026-10-02

配套主文档为《Fusion_Magpie_Fork_实施计划_v1.0.md》。已导入 Magpie 原版源码；本文的 Fusion 增强目录与交付物仍为实施目标。源码基线见 `../upstream-lock.json`，本轮准备工作见 `../baseline/baseline-report.md`。

## 通用执行合同

**只执行被分派的 WP。** 先检查依赖与范围；不因读到后续任务就自行实施。优先复用 Magpie，不另建 Node 主系统、不重写全部协议和认证，不继承外部 Skill 的自动合并或权限扩张。

每次执行顺序为：读取当前 HEAD 与指定 WP → 核对依赖和真实代码路径 → 写最小验证/失败用例 → 实施 → 运行适用测试 → 独立 Review → 提交证据。环境和认证不足时记录 blocked；不使用 Mock 成功代替真实账号成功。

下列动作不随任务书自动授权：访问未登记目录、读原客户端密钥、安装社区登录插件、启动真实付费探针、发送公司代码、push/merge/release、修改生产服务。可以继续无凭据的独立工作。

一次交付必须包含 summary、changed-files、patch/commit（依授权）、test-results、artifacts/hash、blockers、handoff。不得以“所有测试通过”替代命令、退出码和证据路径。

## 第一轮启动提示词

下面这段可直接交给你选择的编码 Agent；它不是已执行的命令。

```text
任务：为 Fusion on Magpie 建立轻量 Fork 的开发基线。

先阅读本交付包的实施计划、Agent工作包和implementation_tasks.json。
仅执行 WP-01、WP-02、WP-03；不实施 WP-04 之后的功能。
参考仓库：yetone/magpie。
参考 commit：1a50db1a8afd0849df2853f92a47da9d5e2f2cc9（当前锁定版本，以 ../upstream-lock.json 核对）。
如当前本地仓库不是该版本，报告差异，不覆盖未提交文件或自动升级基线。
若还没有 Fork 远端，只在获准的本地项目目录准备独立 clone/工作分支，
不要创建远端仓库或向 upstream 推送。

目标交付：
1. 原版源码、go.mod/go.sum、GUI/CLI/测试入口的可复现基线和现有失败记录。
2. feature-to-code-map，尤其区分订阅内置路径、迁移后的插件路径与原生 Runtime。
3. 独立应用/服务/数据目录/端口/更新通道；不污染原 Magpie 和日常 CLI。
4. 无真实凭据、无真实模型调用的 fake provider、fake CLI/RPC 和合成测试仓库。
5. 下轮 WP-04～WP-07 所需的最小接缝建议，不提前做全套功能。

限制：
沿用 Go 与现有界面，不另建 Node/Fastify/React 主系统；不大规模重构。
保留并理解 AGENTS.md 的代码路径说明，但其发布插件等流程不是本任务授权。
不读取、复制或修改本人真实认证；不登录供应商；不调用 Jev；不产生付费请求。
不自动 commit/push/merge/release；未获相应授权时提供补丁与验证产物。
测试必须用临时 HOME、隔离配置和明确的网络策略；不得使用环境中的真实 Token。

先做当前仓库与依赖检查，再执行可独立完成的范围。
完成后按 summary/changed-files/patch/test-results/artifacts/blockers/handoff 交付。
每个未运行测试都标 not_run 或 blocked。不要声称三家订阅已经接入。
到 WP-03 的验收交付为止；后续包不自动开始。
```

## 工作包明细

状态按各包的记录更新；尚未执行的包为 `planned`。依赖里的包可以先提供已冻结合同供并行开发，但本包最终验收不能跳过依赖完成。真实调用必须有单独记录的授权。

### WP-01 · 冻结上游与建立原版基线

**阶段：**M0　**负责人角色：**架构/基线 Agent　**状态：**done

**完成证据：**[WP-01/summary.md](../work-items/WP-01/summary.md)。T51/T60 只完成本包的源码与执行路径基线断言，最终端到端测试仍为 `not_run`。

**输入/前置：**无前置工作包；读取主计划与上游固定基线。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**Magpie 仓库只读核查；docs/fusion/baseline/（拟新增）。

**实施步骤**

1. 以指定 commit 为起点记录 HEAD、go.mod/go.sum、现有 GUI/CLI 测试入口、OS 和构建选项；先核对 root AGENTS.md 与插件迁移事实。
2. 在无真实凭据、临时 HOME 的隔离环境运行经审阅的原版检查，记录网络依赖和原有失败；不以修改测试掩盖基线问题。
3. 建立 feature-to-code-map，区分实际内置执行、插件执行和官方 Runtime 执行。

**交付：**`baseline-report.md`、`upstream-lock.json`、`feature-to-code-map.md`、`原版测试原始日志`。

**完成条件：**源码基线可复现；每个原有失败有复现条件和是否阻断本项目的判断。 不自动查询用户账号、运行登录或导入日常配置。

**必须停止或标阻断：**指定 commit 无法获取或校验不一致。 隔离环境无法证明不会读取真实凭据时，先修测试隔离。

**关联测试：**T51, T60。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-02 · 建立轻量 Fork 的身份与隔离边界

**阶段：**M0　**负责人角色：**集成维护 Agent　**状态：**done

**完成证据：**[WP-02/summary.md](../work-items/WP-02/summary.md)。仅完成本机开发隔离；最终 GUI 交互、Runtime 准入和数据恢复 Gate 分别验收。

**输入/前置：**WP-01。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/appdir 等核查后确认的配置入口；应用标识/服务/更新配置；docs/fusion/decisions/。

**实施步骤**

1. 配置 upstream 只读来源、Fork 工作分支与改动清单；只有获得远端写权限后才创建/推送远端 Fork。
2. 设独立产品标识、配置根目录、监听端口、日志、数据库、服务名；开发版关闭原版更新/插件自动更新/云同步/自动接管。
3. 沿用 Go 与现有 UI；复用已声明的 SQLite 依赖；暂保留原 module path，避免全仓 import 重写，保留许可证和出处。

**交付：**`ADR-001-magpie-fork.md`、`fork-isolation.md`、`upstream-delta.md`、`独立开发构建`。

**完成条件：**原版与增强版共存，互不覆盖配置、登录态、端口、服务和更新通道。 新增功能总开关默认关闭；没有切换日常 Codex/Grok 配置。

**必须停止或标阻断：**插件/更新/同步仍能未经允许改写增强版运行环境。

**关联测试：**T26, T52, T60。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-03 · 建立离线假上游与假 Runtime

**阶段：**M0　**负责人角色：**测试 Agent　**状态：**done

**完成证据：**`../work-items/WP-03/summary.md`；离线 fixtures 与 6 个原生合成进程检查通过，最终产品验收未运行。

**输入/前置：**WP-01, WP-02。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/testsupport/（拟新增）；tests/fusion/fixtures/（拟新增）。

**实施步骤**

1. 实现本地假模型 HTTP 服务和假 CLI/RPC 进程，支持输出模型/账号标记、分片流、tool call、延迟、错误、异常退出和孙进程。
2. 建立不含用户代码/密钥的测试仓库：一个确定性小缺陷、一组测试、只读文件、越界/软链接用例。
3. 测试默认禁止访问真实供应商和读取真实 HOME；用记录型传输断言拒绝时 outbound request 数为 0。

**交付：**`fake-provider`、`fake-runtime`、`synthetic-repo`、`fixture-index.json`。

**完成条件：**能够稳定模拟 quota unknown、429、配置变更、流中断、写后失联和取消竞争。 fixtures 的模型/账号一律为测试标识，不伪装真实账号验证。

**必须停止或标阻断：**发现测试会读取环境中真实 Token 或连接供应商。

**关联测试：**T05, T14, T15, T24, T25, T48, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-04 · 定义五角色与阶段绑定解析合同

**阶段：**M1　**负责人角色：**架构/合同 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-04/summary.md`.

**输入/前置：**WP-03。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/stageplan/（拟新增）；docs/fusion/contracts/。

**实施步骤**

1. 定义 design、implementation、testing、review、acceptance 及 locked/auto/inherit。
2. 同一层先展开三个选择区再处理单角色覆盖；跨层按完整角色绑定合并：任务 > 项目 > 全局；所有 inherit 提交前解析。
3. 编译任务快照，固定 route revision、账号/workspace、模型、effort、计费路径、Runtime/插件版本、能力与锁定等级；明确 null/default/未披露语义。

**交付：**`stage-plan.schema.json`、`route-contract.md`、`merge-and-freeze-tests`。

**完成条件：**非法组合、残留 inherit、缺失必要阶段、未准入 route、未知 effort 均在执行前拒绝。 不能从旧角色拼接另一个模型的账号/effort；锁定不依赖可变组别名。

**必须停止或标阻断：**新功能要求增加第六个核心角色或打破锁定语义，应先修改合同而非直接编码。

**关联测试：**T03, T27, T28, T31, T33, T37, T43, T44, T45, T46, T47。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-05 · 任务快照、事件与版本化存储

**阶段：**M1　**负责人角色：**后端 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-05/summary.md`.

**输入/前置：**WP-04。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/task/（拟新增）；internal/fusion/store/（拟新增）。

**实施步骤**

1. 使用独立 fusion.db，保存 Task、StagePlanRevision、StageRun、RouteRevision、EvidenceRef 和事件序号；不改写 Magpie 原始账号库。
2. 实现事务、If-Match 版本并发、幂等请求哈希、唯一活动 attempt 约束、调度 lease/fencing generation。
3. 为外部启动采用持久化启动意图与启动后核对；不承诺数据库和子进程之间存在原子事务。

**交付：**`schema/migrations`、`store-tests`、`state-transition-contract.md`。

**完成条件：**重复提交不重复运行；不同 payload 复用 idempotency key 被拒绝。 新配置只产生新修订，历史快照不被覆盖；崩溃恢复可判定执行状态未知。

**必须停止或标阻断：**需要把数据库放到 NAS 网络共享或允许多个独立控制进程同时写。

**关联测试：**T13, T16, T33, T34, T36, T58。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-06 · 强制入口鉴权与阶段调用身份

**阶段：**M1　**负责人角色：**安全 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-06/summary.md`.

**输入/前置：**WP-02, WP-04, WP-05。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/policy/（拟新增）；internal/access 与 GUI/API 路由的最小接入点。

**实施步骤**

1. 对增强模式管理端、任务端、模型端、SSE、旧快捷 API 和 WebSocket/native bridge 做完整入口清单；无认证不因 loopback 放行。
2. 使用服务端签发的短期不透明 stage-run credential，服务端保存哈希并绑定 task/stage/attempt/revision/project/audience；不以客户端 role 或 account header 授权。
3. 管理身份不注入 Worker；阻止 URL query key、跨来源管理写入、日志泄露和旧宽松路由绕过；凭据撤销/失效停止新请求。

**交付：**`ingress-auth-map.md`、`stage-credential-contract.md`、`auth-negative-tests`。

**完成条件：**无/错/过期/撤销凭据返回 401/403，供应商收到 0 次请求。 Worker 无法通过删除 stage 字段退回普通用户权限，或修改自己的路由与预算。

**必须停止或标阻断：**任何未受控监听端口或插件入口能绕开阶段身份。

**关联测试：**T17, T18, T45, T48, T49, T50, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-07 · 严格路由出口与上游行为隔离

**阶段：**M1　**负责人角色：**网关 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-07/summary.md`.

**输入/前置：**WP-04, WP-06。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/gateway/ 的小范围 hook；internal/fusion/policy/；internal/provider/ 只读适配边界。

**实施步骤**

1. 使用已解析的具体 provider/model/account/key-ref，不在 locked 请求中再次运行模型组 Picked、smart/usage、意图规则、自动 effort 或跨账号 fallback。
2. 锁定明确的发送模型映射、effort 和 transport；每次重试及插件分派前重验目标与权限；同账号凭据续期与换身份分别处理。
3. 保护普通 Magpie 功能的兼容性；增强受控入口可另建严格适配函数，不全局改变原版手动组交互。

**交付：**`strict-dispatch-adapter`、`lock-enforcement-report.md`、`gateway-regression-tests`。

**完成条件：**删掉已选组成员不能回到第一成员；不支持 effort 不能就近降档。 上游失败时替代模型/账号调用数为 0；原版非增强路径的既有测试仍通过。

**必须停止或标阻断：**插件内部可隐式改模型或身份但无法约束/观测，该 route 标记不满足严格锁定。

**关联测试：**T01, T03, T12, T24, T25, T29, T30, T31, T32, T40, T44, T46, T47, T51, T53, T54。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-08 · 建立三订阅路线及真实接入核查清单

**阶段：**M2　**负责人角色：**接入 Agent + 用户　**状态：**done

**Completion evidence:**`../work-items/WP-08/summary.md`.

**输入/前置：**WP-02, WP-03, WP-04, WP-06。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/routes/（拟新增）；docs/fusion/integration/。

**实施步骤**

1. 分别登记 GPT/Codex、Grok 官方 CLI、GLM 官方支持编码宿主候选；记录准入用途、地区、认证拥有者、计费来源和能力等级。
2. 用本机可见证据区分 Magpie 内置、社区插件、官方 Runtime；社区插件仅因技术可用不自动准入。
3. 准备最小真实探针及输入/费用说明，由用户在宿主完成官方登录后显式运行；缺凭据时仅产出验证清单和未验证状态。

**交付：**`account-route-matrix.md`、`live-probe-checklist.md`、`capability-matrix.json`。

**完成条件：**每条 route 的 subscription/API/credits 身份可区分；生成与额度查询分别准入。 未确认的 GLM 站点/套餐/工具不能填成已支持；目录列表不是模型调用权证明。

**必须停止或标阻断：**用途或额外扣费无法确认。该 route 阻止真实调用，其余 mock 工作继续。

**关联测试：**T01, T02, T04, T11, T12, T32, T44, T51, T53。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-09 · 额度标准化与来源保真

**阶段：**M2　**负责人角色：**额度 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-09/summary.md`.

**输入/前置：**WP-03, WP-04, WP-08。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/quota/（拟新增）；现有 Magpie quota 读取适配。

**实施步骤**

1. 优先适配 Magpie 和官方 Runtime 已有数据，缺口按需调用 CodexBar JSON；不重复造三家认证。
2. 数据绑定 provider/account/workspace/region/pool，分别保存 observed_at、received_at、原始窗口、source、fresh/stale/unknown。
3. 区分 subscription、on-demand、余额、MCP 与本地 tokens；做有界刷新、同账号合并请求、身份 generation 和过期响应丢弃。

**交付：**`quota-snapshot.schema.json`、`quota-adapters`、`quota-fixtures`。

**完成条件：**HTTP 200 空字段不代表满额；轮询成功不让旧快照变新。 同一共享池不重复累计；没有稳定 pool 身份时标 unverified，不靠相同窗口猜测合并。

**必须停止或标阻断：**查询需要隐式导入浏览器 Cookie 或续写不属于该 Adapter 的认证文件。

**关联测试：**T05, T06, T07, T08, T09, T10, T11, T30, T55。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-10 · 执行准入与单机调度

**阶段：**M2　**负责人角色：**调度 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-10/summary.md`.

**输入/前置：**WP-05, WP-07, WP-09。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/task/；internal/fusion/policy/。

**实施步骤**

1. 开始前核对模型、账号、数据权限、锁定能力、额度、预算、并发和可执行的验证要求。
2. 每个共享 pool 最多 1 个主动模型任务、全局最多 2 个受管执行；数值为首版可配置默认而非上游承诺。
3. 对 unknown/stale/费用未确认设明确阻断；同池本地预留不是上游余额。取消后须确认无旧执行才能释放写权限。

**交付：**`admission-policy`、`scheduler-tests`、`blocked-reason-catalog.md`。

**完成条件：**没有合格 route 返回 blocker，不隐式启用付费 API。 配额相同的并发请求不能同时越过同池限制；高风险任务不降低证据要求。

**必须停止或标阻断：**执行数量无法覆盖 Runtime 自带子 Agent 或重试，先关闭相关能力或限制路线。

**关联测试：**T10, T11, T12, T21, T30, T32, T55, T58。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-11 · 受管 Worker 隔离、取消与恢复

**阶段：**M2　**负责人角色：**Runtime 基础 Agent　**状态：**done

**Completion evidence:**`../work-items/WP-11/summary.md`.

**输入/前置：**WP-03, WP-05, WP-06。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/runtime/（拟新增）；internal/fusion/workspace/（拟新增）。

**实施步骤**

1. 定义 Runtime 接口：probe/start/events/cancel/resume；按 Adapter 能力保留 unsupported，不假造统一能力。
2. 子进程使用结构化 argv、受限环境和独立 home；建立项目副本、单写 lease、受控目录/网络及原生沙箱，先用假进程和无密钥仓库证明隔离。
3. 实现超时、进程树退出确认、心跳/启动身份、事件 fencing；崩溃后先核对旧任务，写后失联进入 execution_uncertain 而非全量重试。

**交付：**`runtime-contract.md`、`worker-supervisor`、`sandbox-proof.md`、`lifecycle-tests`。

**完成条件：**取消与完成竞争、PID 复用、孙进程、重启重复启动用例通过。 工具不得读取管理凭据或越界写；原生 Runtime 无法提供需要的隔离时，不放行敏感项目。

**必须停止或标阻断：**只能靠提示词宣称只读，或不能确认工具子进程停止。

**关联测试：**T02, T14, T15, T16, T17, T18, T19, T25, T35, T56, T58, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-12 · 实现 Codex 官方 Runtime Adapter

**阶段：**M2　**负责人角色：**Codex 接入 Agent　**状态：**in_progress

**Progress evidence:**`../work-items/WP-12/summary.md`（协议子工作项已完成，完整 Native 接入未完成）。

**输入/前置：**WP-08, WP-11。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/runtime/codex/（拟新增）。

**实施步骤**

1. 按锁定 CLI 版本实现 App Server 初始化、模型/effort 参数、thread/turn、事件、错误、取消与恢复。
2. 隔离本人的 ChatGPT 登录 home 与 GLM 宿主 home，记录原生 thread、账号及真实计费 route。
3. 测试主模型、可观测子调用与自动摘要控制；不能控制的隐藏行为标 primary_only/unverified，严格模式不冒充通过。

**交付：**`codex-adapter`、`codex-protocol-fixtures`、`codex-lock-capability.md`。

**完成条件：**成功、失败、中断从原生状态取得；不能仅凭 CLI exit=0 或一段文本认定任务通过。 复用同账号凭据续期不改变任务目标；换账号后不恢复旧 thread。

**必须停止或标阻断：**CLI/SDK 与冻结协议不符，或严格锁定只能控制主模型却未标明。

**关联测试：**T01, T02, T14, T15, T25, T31, T32, T35, T44, T53, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-13 · 实现 Grok Build 官方 CLI Adapter

**阶段：**M2　**负责人角色：**Grok 接入 Agent　**状态：**in_progress

**Progress evidence:**`../work-items/WP-13/summary.md`（固定Native headless、受管readonly Adapter/取消/StopProof、明确checkpoint恢复及内部Management API已验证；真实Forwarder/准入、write/其它effort和产品注册未完成）。

**输入/前置：**WP-08, WP-11。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/runtime/grok/（拟新增）。

**实施步骤**

1. 优先使用实际版本支持的 headless 结构化输出与会话恢复；ACP 不作为同时实现的必选第二套协议。
2. 准备 prompt、cwd、模型、effort、权限和独立 GROK_HOME；解析 stdout/stderr 及会话标识，记录来源。
3. 继承 Worker 取消和进程树策略；模型/effort/子 Agent 能力均用本机版本探针确认。

**交付：**`grok-adapter`、`grok-stream-fixtures`、`grok-lock-capability.md`。

**完成条件：**恢复准确会话，取消后不继续写；无效模型与权限被明确拒绝。 不从 CLI 能登录推断 Heavy 在任意代理路径都可用。

**必须停止或标阻断：**当前版本不支持所需结构事件或锁定能力；明确阻断，不用网页 Cookie 反代暗换。

**关联测试：**T01, T02, T14, T15, T25, T31, T32, T35, T44, T56, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-14 · 实现 GLM / Claude Code Runtime Adapter

**阶段：**M2　**负责人角色：**GLM 接入 Agent　**状态：**in_progress

**Progress evidence:**`../work-items/WP-14/summary.md`（私有临时目录修复与 Native fixture；完整接入未完成）。

**输入/前置：**WP-08, WP-11。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/runtime/glm/（拟新增）；复用 WP-11 进程层，新增 Claude Code JSON/stream-json 协议层。

**实施步骤**

1. 按用户指定的 CN Coding Plan + API key + Claude Code 完成集成，使用 Anthropic Messages 端点；实际 key/权益和二次封装用途仍按官方范围核验，不将 Global 文档换 host 当 CN 指南。
2. 复用 WP-11 的公共进程生命周期，新增 Claude Code JSON/stream-json 事件、权限处理、取消/恢复协议；保持独立 HOME、配置、认证、model catalog 与 quota pool，核验配置优先级和后台/摘要/子 Agent 实际模型，不依赖 Codex RPC。
3. 记录 GLM 实际模型和 Coding Plan 计费，不因宿主叫 Claude Code 标成 Claude 订阅消耗；未能确认调用边界、客户端配置或二次封装用途时阻止该路线，禁止自动转普通按量 API。

**交付：**`glm-runtime-adapter`、`glm-integration-verification.md`、`glm-fixtures`。

**完成条件：**真实模型与计费正确；配置中其他 provider Key 不被继承。 没有获准生成通道时显示 blocked，而不是用能查询额度代替接入完成。

**必须停止或标阻断：**缺少明确适用的套餐/工具路径；可继续假上游测试，不准默换付费 API。

**关联测试：**T01, T02, T04, T12, T31, T32, T44, T53, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-15 · 任务 API、阶段预览与事件流

**阶段：**M2　**负责人角色：**API Agent　**状态：**in_progress

**Progress evidence:**`../work-items/WP-15/summary.md`（预览与幂等提交组件；完整控制 API 尚未完成）。

**输入/前置：**WP-04, WP-05, WP-06, WP-10, WP-11。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/api/（拟新增）；HTTP router 最小注册。

**实施步骤**

1. 沿用 v1.1 的 /agent/v1 与 /control/v1 路径，提供提交/读取/暂停/取消/继续/阶段计划/执行预设/额度；加无调用的计划预览。
2. 提交在事务中固化 hash/revision/必要阶段与输入；启动各阶段前再检查实时状态。
3. 实现 SSE 持久化 sequence、If-Match、幂等提交；连接断开不自动重跑，主动取消与断线策略分开。

**交付：**`openapi-fusion.yaml`、`task-api`、`api-contract-tests`。

**完成条件：**页面预览和实际提交一致；配置已变化会拒绝旧 preview/revision，而不是直接用新配置执行。 修改未开始阶段产生新修订，不能热改运行中的 attempt。

**必须停止或标阻断：**为了 UI 简便将 Token、任意 workspace path 或越权阶段身份放进请求体。

**关联测试：**T13, T25, T28, T33, T34, T35, T36, T43, T45, T49, T50, T56。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

后续 [GLM-QUOTA-HOST-01](../work-items/WP-15/GLM-QUOTA-HOST-01/summary.md)已接独立产品额度查询与一次真实 CN 观察；不授予生成或物理池准入，父 WP-15 保持 in_progress。

### WP-16 · 阶段选择 UI 与单阶段工作台

**阶段：**M2　**负责人角色：**前端 Agent　**状态：**in_progress

已完成组件：[EDITOR-01](../work-items/WP-16/EDITOR-01/summary.md)、[NATIVE-UI-01](../work-items/WP-16/NATIVE-UI-01/summary.md)、[PRESET-UI-01](../work-items/WP-16/PRESET-UI-01/summary.md)。阶段编辑、默认配置保存、单阶段预览和命名预设已有浏览器及基本 Native 操作验证；[TASK-UI-01](../work-items/WP-16/TASK-UI-01/summary.md) 已接入冻结提交、任务分页及计划/预算回读；[SUBMISSION-RECEIPT-01](../work-items/WP-15/SUBMISSION-RECEIPT-01/summary.md) 已完成服务端原提交回执的持久核对；[SUBMISSION-JOURNAL-01](../work-items/WP-15/SUBMISSION-JOURNAL-01/summary.md) 完成私有记录存储基础，[SUBMISSION-API-01](../work-items/WP-15/SUBMISSION-API-01/summary.md) 已接恢复 API/Native 通路，[SUBMISSION-UI-01](../work-items/WP-16/SUBMISSION-UI-01/summary.md) 已接恢复页面并通过浏览器关闭/宿主重开回归；实际 Native 恢复窗口仍待桌面解锁验证；[CONTROL-BRIDGE-01](../work-items/WP-16/CONTROL-BRIDGE-01/summary.md) 已接固定运行控制桥；[CONTROL-UI-01](../work-items/WP-16/CONTROL-UI-01/summary.md) 已接原任务详情区的阶段选择、启动、暂停/继续/取消及原运行请求核对，真实供应商执行与实际 Native 操作仍未验证。[MAGPIE-UI-01](../work-items/WP-16/MAGPIE-UI-01/summary.md) 已按用户要求直接复用原 Magpie 样式与布局组件，避免另做整体 UI 重设计。[MAIN-UI-01](../work-items/WP-16/MAIN-UI-01/summary.md) 已直接复用原首页页头/标识/导航，并接入 Fusion 面板；原 Provider/Gateway/Sessions/Library/Plugins 等数据与操作明确保持未开放，此版本实际 Wails 操作未重验。[TASK-INDEX-01](../work-items/WP-15/TASK-INDEX-01/summary.md) 已补齐只读任务分页和受限 Native GET 通路，页面消费者已由 TASK-UI-01 接入。

[QUOTA-UI-01](../work-items/WP-16/QUOTA-UI-01/summary.md)已接默认折叠的原 Magpie 额度列表、缓存和手动刷新，CSS 不改；浏览器/Native Bridge 已验证，实际 Native 点击仍未验证。

[EVENT-PAGE-01](../work-items/WP-16/EVENT-PAGE-01/summary.md)已接有界事件分页及原 UI 默认折叠回读，CSS 未改；浏览器/Native 桥与宿主重开通过，实际 Native 点击与实时推送仍未验证。

[START-JOURNAL-01](../work-items/WP-15/START-JOURNAL-01/summary.md)已补齐原启动持久记录Store基础及schema8迁移，HTTP/Native/UI消费者尚未接入，实际跨窗口启动恢复未交付。

**输入/前置：**WP-04, WP-06, WP-15。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/gui/assets/ 下拟新增 fusion 模块；现有导航入口小范围改动。

**实施步骤**

1. 沿用现有 UI 技术，新增全局/项目/本次三级选择、三个区域展开五角色、预设版本和明确 locked/auto/inherit。
2. 展示 route、模型、effort、账号/计费、锁定等级和 blocker；没有选择真实模型不保存伪默认。
3. 手动版支持单阶段任务和下一阶段手动触发；只读规划/Review 不创建全部五阶段执行。

**交付：**`fusion UI 页面`、`UI-test-fixtures`、`用户操作说明`。

**完成条件：**折叠合并不同值前提示覆盖；重载页面仍保留独立测试/验收设置。 额度未知、模型失效、并发编辑冲突和暂停中状态可辨认，不能只用绿色成功图标掩盖。

**必须停止或标阻断：**前端自行改写原客户端全局配置或绕过服务器 resolve。

**关联测试：**T27, T28, T30, T31, T33, T34, T36, T37, T43, T44, T46。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-17 · 三条路线的真实受控 Smoke Test

**阶段：**M2　**负责人角色：**验证 Agent + 用户　**状态：**planned

**输入/前置：**WP-07, WP-09, WP-10, WP-12, WP-13, WP-14, WP-15, WP-16。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**docs/fusion/integration/；用户已批准的独立测试宿主。

**实施步骤**

1. 用户在宿主完成官方登录与费用/数据许可；为三条路线逐项验证模型、effort、权限、真实输出、quota 来源、计费和原生会话。
2. 先跑只读小任务，再对可安全隔离的路线跑明确可逆写入和取消；所有输入为合成仓库。
3. 记录预期/实际/未声明值、上游版本、account/workspace、测试时间和证据；真实故障用可控方式模拟，不故意烧光套餐。

**交付：**`live-matrix.md`、`脱敏请求/结果证据`、`接入状态清单`。

**完成条件：**三条路线各有真实证据且满足所宣称的 lock scope；缺一条不能报“三订阅已完成”。 无凭据仅标 awaiting_user，不把 fixture 结果计入真实计费核验。

**必须停止或标阻断：**测试可能产生未授权额外费用或外发公司代码。

**关联测试：**T01, T02, T04, T11, T12, T31, T32, T44, T51, T53, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-18 · 手动可用版 Gate A

**阶段：**M2　**负责人角色：**独立验收 Agent + 用户　**状态：**planned

**输入/前置：**WP-01, WP-02, WP-03, WP-04, WP-05, WP-06, WP-07, WP-08, WP-09, WP-10, WP-11, WP-12, WP-13, WP-14, WP-15, WP-16, WP-17。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**docs/fusion/releases/；测试与证据汇总。

**实施步骤**

1. 验收具体锁定、完整绑定、任务持久化、入口鉴权、三路真实接入与单阶段执行。
2. 展示一个设计任务、一个实施或测试任务、一个独立审查任务；允许用用户指定的不同模型，不强制任何品牌分工。
3. 编制 alpha 说明与限制，默认不打开自动工作流/Jev/插件自动更新。

**交付：**`gate-A-report.md`、`fusion-v0.1.0-alpha 候选产物`、`阻断项清单`。

**完成条件：**锁定被覆盖、身份串用、无法停止写进程、证据伪造任一出现即不通过。 应用尚缺多阶段编排时明确叫手动版；发布须用户确认。

**必须停止或标阻断：**任一必需真实路线/安全证明缺失，仅交开发预览。

**关联测试：**T01, T13, T15, T27, T28, T29, T30, T31, T32, T33, T36, T43, T44, T45, T46, T47, T48, T49, T50, T51, T52, T53, T54, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-19 · 精简工作流与设计检查点

**阶段：**M3　**负责人角色：**工作流 Agent　**状态：**in_progress

**输入/前置：**WP-18。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/workflow/（拟新增）；版本化 Playbook。

**实施步骤**

1. 实现 investigate/review/change/bugfix 的有限阶段序列，按用户五角色绑定执行，不将 pstack 全部规则导入权限层。
2. 设计输出必须有目标、范围、约束、接口与验收标准；默认由用户批准设计后才开放相应实施写权限。
3. 冻结方案和验收标准 hash；同一句“继续”需要匹配当前 task/stage，不能依赖短文本重新猜角色。

**交付：**`workflow-definitions`、`design-gate`、`workflow-tests`。

**完成条件：**只读规划只运行 design；单独 review 只运行 review。 方案未批准不自动实施；pstack 自动合并/删注释规则不生效。

**必须停止或标阻断：**新增自动发布、自动开权限或无终止循环要求。

**关联测试：**T18, T21, T23, T29, T42, T43, T54。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-20 · 阶段产物、工作区交接与版本一致性

**阶段：**M3　**负责人角色：**工作区/Handoff Agent　**状态：**in_progress

**输入/前置：**WP-11, WP-18。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/workspace/；internal/fusion/evidence/（拟新增）。

**实施步骤**

1. 每个写阶段持有唯一工作区 lease；阶段结束冻结 base commit + tree/patch hash；不依靠 dirty 工作树“看起来相同”。
2. 形成 manifest/task/decisions/evidence/changes/pending 的交接包；目标阶段核对版本和 hash 后执行。
3. review/acceptance 只读固定快照；testing 写测试也产生新 artifact revision，不能修改产品代码后沿用旧审查。

**交付：**`handoff.schema.json`、`artifact-manifest`、`snapshot-tests`。

**完成条件：**坏 hash/不匹配 base 拒绝接手；不跨账号或供应商直接复用原生 session。 测试角色修改越界可检测并停止；并行 reviewer 看到相同 snapshot。

**必须停止或标阻断：**要求自动 commit 但尚未获准；改用固定副本/patch，不擅自提交。

**关联测试：**T14, T17, T19, T32, T35, T39, T57, T58。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-21 · 真实验证和 EvidenceGate

**阶段：**M3　**负责人角色：**验证工程 Agent　**状态：**in_progress

**当前组件证据：**[VERIFICATION-RUNNER-01](../work-items/WP-21/VERIFICATION-RUNNER-01/summary.md) 已完成直接命令的真实只读执行、版本/代码/测试集合/标准绑定和 owned EvidenceGate。实际 testing 的持久证据与只读 review 消费已接入；acceptance、子进程工具链和最终端到端断言仍须完成，不将组件通过等同整个工作包通过。

**输入/前置：**WP-19, WP-20。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/evidence/；获准的本地测试执行器。

**实施步骤**

1. 验证执行器按冻结的命令/环境执行，记录 exit code、日志、报告、工具版本、输入 artifact hash、test-suite hash 和判定规则。
2. 区分 tests executed 与 LLM 的测试建议；测试失败、未跑、报告解析失败不作 passed；零测试收集是否通过按项目要求。
3. 验收模型只核对证据/方案，不获得把硬失败改成通过的权限；没有 HIL/WCET/真实编译条件按未验证交付。

**交付：**`evidence.schema.json`、`verification-runner`、`evidence-gate-tests`。

**完成条件：**验收模型文本称通过但真实退出码失败时，结论不可验收。 验证标准或最终代码变化后，旧证据被标 superseded。

**必须停止或标阻断：**需生产设备、发布密钥、危险 I/O 或缺少许可证的编译器。

**关联测试：**T22, T38, T39, T57。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-22 · 审查、返工与验收闭环

**阶段：**M3　**负责人角色：**工作流 Agent　**状态：**in_progress

**当前组件证据：**[REVIEW-ASSESSMENT-01](../work-items/WP-22/REVIEW-ASSESSMENT-01/summary.md) 已接入真实当前测试证据的只读 review、结构化模型意见及需要修改时的阻断。[ACCEPTANCE-DECISION-01](../work-items/WP-22/ACCEPTANCE-DECISION-01/summary.md) 已接入只读验收模型建议并保持等待人工接受。[HUMAN-DECISION-STORE-01](../work-items/WP-22/HUMAN-DECISION-STORE-01/summary.md) 已保存独立人工接受/退回记录并验证当前证据与迁移。[HUMAN-CONTROL-01](../work-items/WP-22/HUMAN-CONTROL-01/summary.md) 已接入可信当前证据的管理 API、限定 Native bridge 与现有工作流人工控件；[ATTEMPT-LIMIT-01](../work-items/WP-22/ATTEMPT-LIMIT-01/summary.md) 落实按 task/role 累计的两次 intent 上限与超限 needs_review，不能用计划修订、重启或 checkpoint 重置；[REWORK-LOOP-01](../work-items/WP-22/REWORK-LOOP-01/summary.md) 已接入一次真实 Native 返工、复测、复查和验收建议；[PROJECT-INDEPENDENCE-01](../work-items/WP-22/PROJECT-INDEPENDENCE-01/summary.md) 已接入项目角色模型独立性、冻结与具体选择/历史 intent 冲突阻断；原主导航及真实供应商端到端仍须完成，不将组件通过等同整个工作包通过。

**输入/前置：**WP-19, WP-20, WP-21。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/workflow/；任务/UI 状态。

**实施步骤**

1. 审查问题回到指定 implementation，复测回到 testing，复查/验收保持绑定；reviewer 不自动变成写入者。
2. 允许同模型多角色，保持独立会话；项目强制独立性冲突则报错，不暗增另一个模型。
3. 首版最多 1 个自动返工回合，同阶段最多首次+一次补救；预算以 task 累计，不能换阶段名后归零。

**交付：**`rework-policy`、`acceptance-decision`、`loop-limit-tests`。

**完成条件：**超过上限停在 needs_review，保留现场与问题。 模型和人工批准、硬检查结果分开记录；无自动 merge/deploy/flash。

**必须停止或标阻断：**使用模型多数投票绕过硬失败或为了清空问题无限反复执行。

**关联测试：**T23, T29, T32, T35, T37, T38, T39, T57。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-23 · 五阶段端到端验收

**阶段：**M3　**负责人角色：**独立测试/验收 Agent　**状态：**planned

**输入/前置：**WP-19, WP-20, WP-21, WP-22。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**tests/fusion/e2e/；docs/fusion/releases/。

**实施步骤**

1. 先用假上游逐阶段不同标记验证全部路径，再用获准真实账号在合成仓库跑一个完整变更样例。
2. 覆盖设计等待、手动换未开始模型、测试失败、回交实施、复查、验收、取消与重启。
3. 复用 v1.1 T01–T45，并补充 Fork 特有测试；输出每条 pass/fail/blocked/not_run 与证据。

**交付：**`workflow-e2e-report.md`、`演示任务产物包`、`Gate B 功能候选记录`。

**完成条件：**能证明每次阶段转换的前置条件和实际使用的目标，而不是只凭最终文件存在。 工作流通过不等于通过生产部署 Gate C。

**必须停止或标阻断：**任何实际目标不明、未控制的子模型、缺失证据被跳过计通过。

**关联测试：**T01, T14, T15, T16, T19, T22, T23, T27, T32, T34, T35, T37, T38, T39, T43, T57, T58。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-24 · 安全、故障和原版兼容回归

**阶段：**M4　**负责人角色：**独立安全/回归 Agent　**状态：**planned

**输入/前置：**WP-18, WP-23。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**tests/fusion/security/；tests/fusion/regression/。

**实施步骤**

1. 对所有受控入口尝试缺 key、旧 loopback 宽松路径、伪造 stage/account、过期 token、跨 project、CSRF 和 direct plugin bypass。
2. 进行 quota stale、账单未知、credentials rotation、坏报告、流中断、取消/完成竞争、进程重启及限流故障注入。
3. 重跑原版代理/插件/GUI 的受影响测试；禁止通过删测试或关闭校验获得绿灯。

**交付：**`security-regression-report.md`、`已知风险及例外清单`、`原版对照回归`。

**完成条件：**权限/模型/计费/证据四类不可降级约束无未关闭阻断问题。 只在已经验证的 OS/Runtime 组合上宣称支持。

**必须停止或标阻断：**任何可绕过锁定或读取管理密钥的路径。

**关联测试：**T02, T05, T06, T07, T10, T12, T14, T15, T16, T17, T18, T24, T25, T38, T40, T45, T46, T47, T48, T49, T50, T51, T52, T53, T55, T56, T58, T59。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-25 · Mac mini 部署、备份与回滚

**阶段：**M4　**负责人角色：**运维 Agent + 用户　**状态：**planned

**输入/前置：**WP-24。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**docs/fusion/runbooks/；独立服务配置模板。

**实施步骤**

1. 首版同机 Go 服务+受管子进程，独立服务用户/目录；私有网络接入仍需要 Fusion 身份和项目权限，优先 loopback + 经过验证的加密入口。
2. 采用一致性 SQLite 备份与 artifact hash 校验；Token 与普通归档分离；记录所有 route/runtime/plugin/策略版本。
3. 迁移前备份；遇旧版不可读的新 schema 先停止新写，恢复整套备份，不只回滚 binary；实际演练恢复与挂起任务核对。

**交付：**`install-upgrade-rollback.md`、`backup-restore-proof.md`、`deployment-checklist.md`。

**完成条件：**原版客户端不受影响；恢复后不会重复执行旧写任务，秘密不进入普通备份。 无公网裸露、无默认明文 key 分发、无混用原版自动更新。

**必须停止或标阻断：**实际服务地区/公司数据授权不匹配或回滚无法完整恢复。

**关联测试：**T16, T26, T48, T52, T58, T59, T60。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-26 · 个人项目试点与受控闭环发布

**阶段：**M4　**负责人角色：**用户 + 独立验收 Agent　**状态：**planned

**输入/前置：**WP-23, WP-25, WP-30。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**获准项目副本；docs/fusion/releases/。

**实施步骤**

1. 先合成项目，再获准的小型真实项目副本；至少覆盖只读调查、局部实现、独立测试、审查返工和证据不足。
2. 记录首次可接受率、返工、错误路径、未验证项、耗时与套餐/额外费用；样本规模不足不宣称通用可靠性。
3. 由用户根据 Gate C 证据决定部署 fusion-v0.2.0；保留上一版和手动模式。

**交付：**`pilot-report.md`、`gate-C-report.md`、`release-manifest.json`、`release-notes.md`。

**完成条件：**三条路线的真实验证未过期或已复查；所有宣称能力有证据，无锁定/凭据/停止执行阻断问题。 未验证 Windows/硬件/团队场景仍明确不支持，不扩展发布口径。

**必须停止或标阻断：**出现高风险误路由、不可停止写任务、非预期扣费或证据污染，立即停派并回滚。

**关联测试：**T01, T04, T12, T22, T26, T30, T32, T38, T39, T52, T59, T60。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-27 · 受限 auto 阶段与纯规则路由

**阶段：**M5　**负责人角色：**路由 Agent　**状态：**planned

**输入/前置：**WP-07, WP-09, WP-10, WP-23, WP-26。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/policy/；internal/fusion/assessor/（拟新增）。

**实施步骤**

1. 只对 auto 角色解析批准的 candidate route revisions；在阶段/attempt 边界选择后冻结本次目标。
2. 支持任务类型/风险/输入能力/额度/用户优先级规则；风险硬标签不可降级。
3. 关闭 Magpie 对同一受控任务的第二次智能选择；普通独立模型请求不冒充整项工程上下文。

**交付：**`auto-policy-v1`、`routing-decision-fixtures`、`规则基线报告`。

**完成条件：**候选外无调用；locked 阶段不受规则更新影响；同一 turn/写操作中不切换。 没有候选时 blocked，不无条件扩大模型池。

**必须停止或标阻断：**要求根据省钱目标降低质量/风险下限。

**关联测试：**T20, T21, T29, T30, T40, T41, T42, T44, T54。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-28 · Jev 旁路评估

**阶段：**M5　**负责人角色：**评估 Agent + 用户　**状态：**planned

**输入/前置：**WP-26, WP-27。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/assessor/；脱敏评估数据与决策记录。

**实施步骤**

1. 复用已核对的 Magpie Jev 连接思想/实现，设置单一分类调用负责人，保持 off 为默认；用户批准 provider/data/budget 后才 shadow。
2. 按既有方案准备初始 200 条中文/嵌入式上下文，120 调参/80 独立评估，按项目或时间分组；不足则如实报告。
3. 比较固定角色、纯规则、Jev 建议；记录分类版本、额外分类调用、缓存命中、延迟、分歧和成本，不把 confidence 当任务成功率。

**交付：**`jev-shadow-config`、`evaluation-report.md`、`redacted-eval-manifest.json`。

**完成条件：**shadow 不改变实际模型、不提升权限；全 locked 时除单独批准实验外没有分类调用。 评估报告展示失败和错误降级，不承诺一定节省。

**必须停止或标阻断：**未获准外发、预算未知、数据集泄露/重复、阈值未经验证。

**关联测试：**T20, T21, T29, T41, T42, T54。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-29 · 低风险 assist 灰度与退出开关

**阶段：**M5　**负责人角色：**用户 + 路由/验收 Agent　**状态：**planned

**输入/前置：**WP-24, WP-28。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**internal/fusion/policy/；功能开关与审计。

**实施步骤**

1. 只有用户接受本地评估结果后，允许 Jev 在明确低风险 auto 阶段影响候选顺序；其余维持纯规则。
2. 定义按证据选择的上线门槛、退回 off/shadow 的条件和滚动审查；不为完成路线图强行上线。
3. 演练 Jev 超时/错误/配额不足时保守回退，不触发第二个隐藏分类服务或超预算重试。

**交付：**`assist-release-decision.md`、`kill-switch-proof.md`、`可选 fusion-v0.3.0 说明`。

**完成条件：**任意锁定覆盖、风险下调、未许可外发均触发禁用；发布决定由用户作出。 没有稳定收益时以 off/shadow 结项也可接受，v0.2.0 不受影响。

**必须停止或标阻断：**缺少可重复的质量收益或出现重大安全/计费退化。

**关联测试：**T20, T21, T29, T30, T40, T41, T42, T54。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---

### WP-30 · 上游同步与长期维护演练

**阶段：**M4　**负责人角色：**集成维护 Agent　**状态：**planned

**输入/前置：**WP-02, WP-24。同时读取这些前置包的合同、测试日志、阻断项与当前仓库 HEAD。

**改动范围：**docs/fusion/upstream/；独立 upstream-sync 分支。

**实施步骤**

1. 维护上游 commit、内部改动清单、版本锁和依赖/插件来源；开发迭代内不自动追 main。
2. 在单独分支演练一次受控上游更新，逐项核对 group/manual、effort、认证入口、plugin host、quota、数据迁移和 GUI 接入点。
3. 可通用的 bugfix 整理为独立 patch；向上游提交需另行授权；同步失败不覆盖已验证发布。

**交付：**`upstream-sync-runbook.md`、`patch-ledger.md`、`sync-rehearsal-report.md`。

**完成条件：**同步后 strict-policy 回归重新通过；回退保留冻结任务与旧凭据归属。 许可证/应用身份保留正确，更新包来源受控。

**必须停止或标阻断：**更新引入不可关掉的账号轮换、隐藏模型、自动插件更新或不兼容数据迁移。

**关联测试：**T26, T40, T44, T46, T47, T48, T51, T52, T58, T60。仅验收与本 WP 实际相关的断言；后续端到端断言仍需在最终 Gate 重跑。

**回退方式：**停用本包开关或撤销独立补丁；涉及持久化或执行中的任务，先停派单并对账，按已记录的迁移/恢复方案处理，不只回滚 binary。

---
