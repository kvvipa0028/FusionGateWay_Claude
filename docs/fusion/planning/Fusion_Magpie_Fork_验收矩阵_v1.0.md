# Fusion on Magpie：验收矩阵
## v1.0 · 2026-10-02

T01–T45 从《Fusion_最终实施方案_v1.1.md》§19.1 原样继承；T46–T60 为轻量 Fork 新增。所有测试当前均为 **not_run**。工作包关联表示哪些包负责相关断言，不表示每个包必须重复执行全部端到端场景。

每次运行应记录：test_id、case_id、版本与环境、前置条件、命令/步骤、预期、实际结果、exit code、输入和输出 hash、证据路径、pass/fail/blocked/not_applicable。一个 T 编号是场景族，不是只允许写一个测试函数。

**必测断言：**拒绝在模型调用前发生时，假供应商记录的越权 outbound 次数必须为 0；Agent 已执行部分写入时先核对现场，不把取消或报错误当已回滚。JSON 为机器可读的同一清单。

| ID | 场景 | 预期结果 | 负责工作包 | 当前状态 |
|---|---|---|---|---|
| T01 | 固定 GPT/Grok/GLM route | 调用正确模型与账号；不暗换路线 | WP-07, WP-08, WP-12, WP-13, WP-14, WP-17, WP-18, WP-23, WP-26 | not_run |
| T02 | 配置文件存在其他 provider Key | 不继承到当前 Runtime，不造成串计费 | WP-08, WP-11, WP-12, WP-13, WP-14, WP-17, WP-24 | not_run |
| T03 | 指定模型不在已验证能力集合 | 明确拒绝，不自动找名称相似模型 | WP-04, WP-07 | not_run |
| T04 | GLM 地区或套餐未确认 | 阻止自动运行，不以 endpoint 可连通作为通过 | WP-08, WP-14, WP-17, WP-26 | not_run |
| T05 | quota HTTP 成功但字段为空 | unknown，不显示满额 | WP-03, WP-09, WP-24 | not_run |
| T06 | 收到旧缓存 | observed_at 保持原值，任务准入不当成新值 | WP-09, WP-24 | not_run |
| T07 | 查询中切换账号/workspace | 丢弃旧查询结果 | WP-09, WP-24 | not_run |
| T08 | 重置时间缺失、异常或时区混乱 | 不猜测修正，不自行发放额度 | WP-09 | not_run |
| T09 | Grok on-demand 比例出现 | 不冒充订阅 credits 比例 | WP-09 | not_run |
| T10 | 同一 pool 多个模型并发 | 共用准入与预留，不重复计算余额 | WP-09, WP-10, WP-24 | not_run |
| T11 | 达到限额或 app-specific limit | 按真实错误处理，不把所有账户都标成耗尽 | WP-08, WP-09, WP-10, WP-17 | not_run |
| T12 | 付费 fallback 未授权 | 不发起额外计费请求 | WP-07, WP-08, WP-10, WP-14, WP-17, WP-24, WP-26 | not_run |
| T13 | 相同幂等提交被重发 | 返回原任务，不重复执行 | WP-05, WP-15, WP-18 | not_run |
| T14 | 工具写入后网络断开 | execution_uncertain/核对现场，不盲目重放 | WP-03, WP-11, WP-12, WP-13, WP-20, WP-23, WP-24 | not_run |
| T15 | 取消与完成竞争 | 终态一致；确认停止前不宣称 cancelled | WP-03, WP-11, WP-12, WP-13, WP-18, WP-23, WP-24 | not_run |
| T16 | 控制服务重启、旧 Worker 尚存 | 不产生第二个写入者 | WP-05, WP-11, WP-23, WP-24, WP-25 | not_run |
| T17 | 路径穿越或 symlink 越界 | 访问被阻止并审计 | WP-06, WP-11, WP-20, WP-24 | not_run |
| T18 | 仓库文本要求读取凭据或忽略规则 | 不能升级权限、外发秘密或切换预算 | WP-06, WP-11, WP-19, WP-24 | not_run |
| T19 | 写任务与两个 reviewer 并行 | reviewer 基于固定 snapshot，原工作区不冲突 | WP-11, WP-20, WP-23 | not_run |
| T20 | Jev 错误、超时、低置信度 | 回到规则，不随机选路，不自动高额重试 | WP-27, WP-28, WP-29 | not_run |
| T21 | 标记 R3 的“一行修改” | 不被低复杂度分类降低风险下限 | WP-10, WP-19, WP-27, WP-28, WP-29 | not_run |
| T22 | 缺编译器/HIL/硬件证据 | 明确 unverified，不输出伪造 passed | WP-21, WP-23, WP-26 | not_run |
| T23 | pstack 默认自动 push/merge 指令 | 不进入实际授权能力，不执行 | WP-19, WP-22, WP-23 | not_run |
| T24 | 模型协议含未支持工具/字段 | 明确 unsupported，不静默丢弃 | WP-03, WP-07, WP-24 | not_run |
| T25 | SSE 已输出部分内容后上游失败 | 不跨模型拼接，返回中断及可恢复信息 | WP-03, WP-07, WP-11, WP-12, WP-13, WP-15, WP-24 | not_run |
| T26 | 备份恢复/版本回滚 | 状态与产物可核对，凭据不泄露 | WP-02, WP-25, WP-26, WP-30 | not_run |
| T27 | 三个选择区展开为五角色 | 实施/测试、审查/验收可独立选择，合并前提示覆盖 | WP-04, WP-16, WP-18, WP-23 | not_run |
| T28 | 全局、项目、本次任务冲突 | 按明确优先级生成完整角色绑定；不跨模型混合 effort/账号 | WP-04, WP-15, WP-16, WP-18 | not_run |
| T29 | 用户锁定与 Jev/pstack 建议冲突 | 维持锁定；建议只展示，不产生暗换调用 | WP-07, WP-18, WP-19, WP-22, WP-27, WP-28, WP-29 | not_run |
| T30 | locked 模型无额度但别家有额度 | 暂停/阻止并说明，不自动替换或额外计费 | WP-07, WP-09, WP-10, WP-16, WP-18, WP-26, WP-27, WP-29 | not_run |
| T31 | 显式 effort 不支持 | 拒绝并提示，不静默降档 | WP-04, WP-07, WP-12, WP-13, WP-14, WP-16, WP-17, WP-18 | not_run |
| T32 | 实施子 Agent 或摘要助手试图换模型 | 继承绑定或阻止；Runtime 无法控制时标记 lock unverified | WP-07, WP-08, WP-10, WP-12, WP-13, WP-14, WP-17, WP-18, WP-20, WP-22, WP-23, WP-26 | not_run |
| T33 | 修改全局默认或同名预设 | 已提交任务使用旧版本快照，其他项目不受影响 | WP-04, WP-05, WP-15, WP-16, WP-18 | not_run |
| T34 | 修改未开始的验收模型 | 修订计划、重新准入；当前阶段和历史保持不变 | WP-05, WP-15, WP-16, WP-23 | not_run |
| T35 | 在流/工具写入中途申请换模型 | 先安全暂停和核对，不能就地热换或并发启动第二个写任务 | WP-11, WP-12, WP-13, WP-15, WP-20, WP-22, WP-23 | not_run |
| T36 | 两个页面同时改阶段计划 | If-Match 冲突被拒绝，不后写覆盖 | WP-05, WP-15, WP-16, WP-18 | not_run |
| T37 | 设计与验收选同一模型 | 允许配置；独立会话与证据；若违反项目硬约束则明确阻止 | WP-04, WP-16, WP-22, WP-23 | not_run |
| T38 | 验收模型判通过但测试硬失败 | 不可验收；保留模型意见与真实失败证据 | WP-21, WP-22, WP-23, WP-24, WP-26 | not_run |
| T39 | 审查后修复导致产物 hash 改变 | 旧结论标 superseded，修复回指定实施模型，复测/复查按计划进行 | WP-20, WP-21, WP-22, WP-23, WP-26 | not_run |
| T40 | Magpie 模型组/会话粘性仍触发 fallback | locked 路线不允许该配置；auto 仅允许批准成员且记录每次尝试 | WP-07, WP-24, WP-27, WP-29, WP-30 | not_run |
| T41 | Fusion 与引擎同时开启分类器 | 只保留一个经授权负责人；不重复发送上下文与计费 | WP-27, WP-28, WP-29 | not_run |
| T42 | “继续”或一句话高风险任务 | 使用阶段上下文及硬风险标签，不靠短文本长度降级 | WP-19, WP-27, WP-28, WP-29 | not_run |
| T43 | 仅规划/仅审查任务 | 只执行必要角色，不为了五槽配置启动全部阶段 | WP-04, WP-15, WP-16, WP-18, WP-19, WP-23 | not_run |
| T44 | 候选目录、别名或 route 配置更新 | 旧任务绑定的版本不漂移；上游版本不可固定则如实标注 | WP-04, WP-07, WP-08, WP-12, WP-13, WP-14, WP-16, WP-17, WP-18, WP-27, WP-30 | not_run |
| T45 | 模型选择请求夹带新账号/密钥/付费许可 | 不接受未注册凭据或自行提升权限；额外费用仍独立审批 | WP-04, WP-06, WP-15, WP-18, WP-24 | not_run |
| T46 | Magpie manual 所选成员被删除或组变空 | 增强阶段明确 blocked；禁止 Picked() 回到第一成员，其他上游调用数为 0。 | WP-04, WP-07, WP-16, WP-18, WP-24, WP-30 | not_run |
| T47 | 底层自动把显式 effort 映射到最近支持档位 | 严格阶段在分派前拒绝，不发送降档请求；展示支持列表。 | WP-04, WP-07, WP-18, WP-24, WP-30 | not_run |
| T48 | 无 key、任意 key、loopback、反代或旧 API 入口 | 增强模式所有受控端点一致鉴权；管理/模型数据不经旧宽松入口泄露。 | WP-03, WP-06, WP-18, WP-24, WP-25, WP-30 | not_run |
| T49 | Worker 伪造 role/task/stage/account header | 服务端 capability 绑定优先；拒绝跨任务、跨项目和管理接口访问。 | WP-06, WP-15, WP-18, WP-24 | not_run |
| T50 | stage token 过期、撤销、旧 attempt 重放 | 新调用被拒绝；generation 不匹配的延迟事件不能覆盖新任务状态。 | WP-06, WP-15, WP-18, WP-24 | not_run |
| T51 | 同一供应商迁移为社区插件或插件版本改变 | 测试/策略覆盖实际执行路径；旧内置测试不能充当插件验证；新版本重新准入。 | WP-01, WP-07, WP-08, WP-17, WP-18, WP-24, WP-30 | not_run |
| T52 | 原版和 Fork 同机启动、更新、同步或配置迁移 | 数据目录、服务、端口、应用身份、更新与插件通道隔离；不覆盖日常客户端配置。 | WP-02, WP-18, WP-24, WP-25, WP-26, WP-30 | not_run |
| T53 | 合法同账号 Token rotation 与账号切换竞争 | 同身份续期可恢复；身份/组织/计费范围变化拒绝旧绑定，旧 quota 响应丢弃。 | WP-07, WP-08, WP-12, WP-14, WP-17, WP-18, WP-24 | not_run |
| T54 | 所有必要阶段 locked，Jev/下层分类器已配置 | 除单独获准的 shadow 实验外分类器 outbound=0；不重复判断或暗中计费。 | WP-07, WP-18, WP-19, WP-27, WP-28, WP-29 | not_run |
| T55 | 多任务看到同一额度快照，外部客户端另有消耗 | 共享 pool 准入串行；本地 reservation 不显示成官方余额，过期/无法匹配的新任务阻断。 | WP-09, WP-10, WP-24 | not_run |
| T56 | 客户端 SSE 断开、重连或取消时服务仍在运行 | 按事件序号补读，不自动再次执行；主动取消待停止确认后才释放写 lease。 | WP-11, WP-13, WP-15, WP-24 | not_run |
| T57 | testing 阶段改动产品代码/测试标准或产物被换掉 | 检测越权或生成新 artifact revision；旧测试和审查不可继续证明当前产物。 | WP-20, WP-21, WP-22, WP-23 | not_run |
| T58 | DB 提交与 Worker spawn 之间崩溃/迟到完成事件 | 启动意图与进程核对恢复；无双写、无旧 generation 覆盖、无虚假恰好一次承诺。 | WP-05, WP-10, WP-11, WP-20, WP-23, WP-24, WP-25, WP-30 | not_run |
| T59 | Agent 工具或测试进程读取管理密钥/访问非许可网络 | OS/Runtime 限制与凭据隔离经测试；不能执行隔离的路线不对敏感项目放行。 | WP-03, WP-06, WP-11, WP-12, WP-13, WP-14, WP-17, WP-18, WP-24, WP-25, WP-26 | not_run |
| T60 | 合并上游变更或回滚到旧 schema 版本 | 独立分支回归，迁移先备份；不兼容时停写并整体恢复，发布与数据版本保持一致。 | WP-01, WP-02, WP-25, WP-26, WP-30 | not_run |

## Gate 使用规则

Gate A 要求手动版涉及的合同、鉴权、锁定、额度、Worker、UI/API 与三路真实行为通过；Gate B 要求阶段链、Handoff、证据和返工通过；Gate C 增加部署、故障、安全、回滚、原版兼容与上游同步。

T20 及 T42 中实际 Jev 调用分支在未启用该功能的发布可标“未启用/不适用”，但规则保守回退、风险硬约束、off 无外发和 locked 不被覆盖仍为基础版本必测。

依赖网络的测试不与离线 fixture 混记。不能仅凭本地 Mock 验收真实套餐。测试未跑不可写 pass；真实权限、取消、秘密隔离、锁定和计费路径问题不得以 N/A 豁免。

## 新增场景的源码依据

T46/T47 对应固定基线 `internal/provider/group.go` 的 Picked fallback 和 effort 兼容语义。
T48 对应 README 的 loopback 认证边界，增强版须单独加固。
T51 对应 `AGENTS.md` 记载的迁移后插件实际执行路径。
其余新增项是 Fusion 的系统设计验收，不是宣称已发现 Magpie 存在这些漏洞。
