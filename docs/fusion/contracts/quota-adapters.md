# 额度读取与调度合同 v1

WP-09 只适配明确提供的 JSON，不发现认证、不导入 Cookie、不写认证文件、不调用旧 SubscriptionUsage 全局轮询。真实只读查询必须先取得对应身份/来源的额度查询准入；生成准入不能代替查询准入。Grok/其他尚无明确数据合同的来源保留 unverified/unsupported，不安装 CodexBar 或启动其全账号发现。

Snapshot 绑定 provider/account/workspace/region/generation，记录 source、observed_at、received_at、完整性、pool 与原始窗口已知字段。供应商未提供/未核验数据时间时 observed_at=null，不以轮询完成时间替代。Magpie 缓存 asOf 覆盖较新的轮询时间；缺失 percentage/used 不按 0 解析。无效百分比只保留在 raw 中，规范字段为 null。响应未知字段及认证/错误文本不写入原始窗口记录。

状态区分 available、zero、unknown、stale、unverified、auth_required、unsupported。公开快照按一分钟 source-age 上限保守分类；实际执行前调用 State 再验证，可要求更短时间。过时数据、未来时间、已过去的 reset 不推算额度已恢复。HTTP 200 空字段返回 unknown；零额度是有明确值的耗尽状态。余额、credits、MCP、本地 tokens 与 subscription/coding_plan 分开，不作为模型额度相加或付费 fallback。

AdaptMagpie 保留 Quota JSON 的 used/remaining/asOf，拒绝矛盾值；unlimited 不自动等于可无限使用。AdaptCodex 优先采用 rateLimitsByLimitId，旧 rateLimits 是 alias，不重复计数。AdaptGLM 保留 Coding Plan 原始 limits、percentage、unit、number 和 reset，MCP/未知类型单独记录；不套用旧读取器把缺失 percentage 写成 0 的语义。

Pool 需明确 verified、provider/region、scope/owner/id。只有供应商/受准入 Reader 提供稳定物理池身份时才合并。窗口名称、reset 相同或百分比相同不用于猜测共享池。GroupPools 同池保留别名，采用较新的 source observation，不相加；较新的不完整数据不能被旧的“可用”别名遮住。没有稳定 pool 时保留独立行并禁止据此调度。

Broker 按 exact provider/account/workspace/region/generation 合并在途刷新，上限八条，超时至多十秒。调用者取消不影响其他等待者；Reader 不响应取消时保留占用，防止反复创建无限 goroutine。Reader 必须支持取消；真正外部进程由 WP-11 终止控制。

SetIdentity 禁止 generation 回退；切换账号/workspace/region 前 Retire 旧身份，再登记新身份。旧响应不入缓存。失败不会刷新 observed_at；保留显示数据但标 stale/auth_required/unsupported，原始错误与包装错误都规范为静态错误，不回显凭据。

配套 schema 约束结构，Go State 约束跨字段、来源年龄和身份池语义。fixtures 与测试均为合成数据。本包不证明本人真实额度、实际计费或最终 T05–T11/T30/T55 验收；Native Reader 在 WP-12–WP-14 接入，WP-10 再消费有效快照进行调度。
