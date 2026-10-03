# 阶段计划与执行目标合同 v1

WP-04 仅编译计划；路线准入、额度、鉴权和真实执行出口由后续 WP 实现，Fusion master 开关仍关闭。注册表必须来自服务端可信版本化记录，不能从客户端计划 JSON 接受 `admitted` 等路线字段。

## 配置与合并

五个角色固定为 design、implementation、testing、review、acceptance。三个选择区为 design_planning、implementation_testing、review_acceptance。每层先展开 groups，再以 roles 的完整 Binding 覆盖同层角色；随后按 global → project → task 替换完整 Binding。inherit 只向较低层取完整绑定，不拼接字段。同层 role=inherit 遮住本层 group，转向较低层。

`required_roles` 明确本任务要用哪些阶段，不强迫只读设计任务运行五阶段。必需角色缺失、最终 inherit 未解析、重复/未知角色、未知 group 或错误模式均拒绝。其他角色的语法也验证；不使用的角色不获得执行准入。

`locked` 必须包含具体 route id/revision、具体 model 和 effort 选择；不能同时包含候选。`auto` 必须提供批准候选，每个候选包含具体 route id/revision/model/effort，不能夹带外层 locked 目标。候选不能重复 route revision。auto 编译时冻结全部候选，阶段 attempt 的单次选择由策略层完成。

## 未指定、默认和未知

| 输入/记录 | 语义 |
|---|---|
| effort 缺省或 null | 未指定；执行角色拒绝，不借用较低层字段。输入 JSON 的 null 设置均拒绝；inherit 应省略字段 |
| effort.mode=explicit | value 必须是具体模型已验证支持的值；禁止就近映射 |
| effort.mode=default | 默认值必须已知且在支持列表中；编译时冻结具体值 |
| effort.mode=none | 仅用于已验证不接受 effort 参数的模型；冻结 value=null |
| default_effort=null | 注册表未披露默认值，不等于 none/default |
| plugin_version=null | 明确无插件路径；使用插件时必须登记具体版本 |
| upstream_reported_model=null | 上游尚未声明；不能以 requested/resolved 值补写证明 |

JSON Schema 验证结构；Go ParsePlan 额外拒绝重复 JSON 键、尾随数据、null 设置、64KiB 超限和过深结构。Compile 验证注册表语义、精确版本和支持能力；不能用 schema 结构通过代替准入。

## 冻结

Compile 输出 Snapshot：schema_version、revision、required_roles、bindings、hash。每个 FrozenBinding 记录解析来源；locked 的 target 和 auto 的 candidates 都是新复制的 ExecutionTarget，不保留输入切片/指针，不持续引用可变组、预设 latest 或最近 effort。

ExecutionTarget 固定 route revision、requested_model、resolved_model、账号、workspace、credential_identity、具体 effort、billing_path、Runtime/插件版本、已验证能力与 lock_enforcement。账号/workspace/凭据身份来自该 exact revision；改选账号应选择另一个完整 route revision。必需身份、模型或账单路径未知、未准入 route、模型不匹配、能力不足均拒绝。快照不含凭据明文。

`controlled_calls` 覆盖本系统能控制/观测的 Agent 调用，不代表供应商内部权重证明。`primary_only` 只有本任务取得明确接受、登记 accept_primary_only=true 后才可编译；当前用户未接受此例外。`unverified` 永不执行。准入层负责验证/限制摘要和子 Agent 调用，将证据绑定到版本；UI 不能把一个布尔值当作已通过真实准入。

hash 为去除 hash 字段后 Go encoding/json 的 SHA256。角色顺序规范化，映射键由编码器排序；修改任何目标或版本会改变 hash。VerifySnapshot 仅证明数据完整性，不证明发行者身份、当前权限、额度或真实远端模型。入口必须编译服务端计划并通过鉴权、策略和准入出口，不能接收客户端自制快照来执行。

未开始阶段的配置变更使用 If-Match 产生新 plan revision；运行中阶段安全暂停后产生新 attempt。存储与状态机由 WP-05/WP-07 落实，不通过可变指针修改现有快照。
