# WP-15-CONTROLLER-01 可信执行生命周期

状态：子项 done；WP-15 in_progress。新增 internal/fusion/control，服务端 Config/Resolve/Backend 不进入任务 DTO。Start 只接受任务/角色/计划/generation 与 key：持久映射重读在 Runtime 解析之前完成，新建时采用冻结目标、验证候选选择、可信 Spec/能力及当前 Scheduler 准入，提交后独立于 HTTP Context 管理执行。Backend 的绑定包装保留 Handle+error，退出核验仍由同一 Adapter/Supervisor 担当。

取消检查 task/run/generation 与本控制器 job，unknown 不自动接管。没有 Handle 或不能核验停止的执行保留 reservation/预算以待对账；没有新增自动重试/Resume。原生失败后即使释放容量，task interrupted/unknown 也不能自动重新执行。Close 等待实际 owned Handle；超时不等于停止。

11 个离线测试覆盖重试/HTTP 断线、无 Handle、已知 Handle+error、Wait/Stop/Release/错 session proof、跨任务/过期 generation 取消、关闭、10 类非法服务端 launch 配置、重启 unknown、Handle 发布前取消、关闭超时，以及 auto 候选副本与冻结边界。初始 RED 为入口/类型缺失；随后 GREEN。非法时限测试使用 Runtime 公共合同上限十分钟的越界值十一分钟，GLM 四分钟限制由其 Adapter 独立保留。

实际冻结 Claude Code 2.1.287 通过新 Controller 的 PrepareOnce/CheckPrepared、真实 GLM Adapter/Supervisor/owned loopback，得到一个合成 HTTP 和持久 Permit，解析 FUSION_FIXTURE_OK，并以真实 kernel wait 与 Supervisor StopProof 释放预留。断开请求 Context 及同 key 重试未再次启动。该 Native 上游 Transport 为进程内合成响应，没有真实 GLM/model/quota 请求，也不证明真实路线或计费；此前 CNTransport 实际 TLS fixture 的证据继续独立保留。

最终完整 Fusion tagged race 248 PASS、8 SKIP；CLI/GUI build 和全仓 vet 通过。8 个条件跳过含本次必须显式提供 Native pin 的测试，其实际结果见独立 Native log。没有将合成 Inspection 提升为真实准入、没有读取真实 key、Jev off。产品 HTTP 鉴权/endpoint、私有项目与账户配置、暂停/恢复/有限返工、预设/额度、OpenAPI/UI 和真实三路 smoke 尚未完成。
