# WP-15-PREPARE-ONCE-01 Scheduler 幂等启动入口

状态：子项 done，WP-15 in_progress。新增 Scheduler.PrepareOnce，先匹配 task/role/plan revision/generation 的持久请求身份与 exact frozen Target，再决定只读旧 receipt 或按当前准入新建 intent。已有请求不调用 Inspection，不重复预留、不刷新 lease、不退还预算；terminal/unknown 可读但 Created=false，不可启动。Owner 变化不会重新解释原启动意图。

两个相同请求并发时，另一个可在本次准入期间先提交。若本次检查随后发现 task 不再 ready 或 Inspection 不可用，仍只读已提交 receipt；不借此为新执行绕过准入。原 Prepare 的准入路径提取为共用 helper，具体模型、账号、effort、计费、权限、sandbox、验证、quota 与预算要求保留；原 Prepare 拒绝带幂等 key，避免旧调用方把重读当作启动权限。新意图需要 ExpectedGeneration，与事务内 CAS 相互覆盖读写竞态。启动前 CheckPrepared 和发送前 Permit 保持独立强制检查。

6 个新测试先 RED（入口不存在）再 GREEN：终态重试不再查询/不退款/不释放 held、重启 unknown 且 Inspection 不可用、身份/Target 冲突和非法请求、不合格当前额度与旧 generation、16 个并发请求只有一个 Created winner、明确同步的准入失效/另一请求先提交竞态。新增测试和原调度回归共 9 个顶层测试通过。完整 Fusion tagged race 237 PASS、7 SKIP；CLI/GUI build 和全仓 vet 通过。

原生 Claude Code 2.1.287 的 Adapter 8 场景也重新通过（合成 TLS 上游、私有 fixture key、实际 OS stop proof），覆盖本次共用 helper 对原 Prepare→Adapter 路径的兼容性；该 Native 测试不宣称产品控制器已使用 PrepareOnce。没有真实模型/额度查询，fixture Inspection 不提升为真实路线准入。Jev off。

此入口由可信控制端鉴权和解析冻结 Target 后调用。产品 endpoint、控制器生命周期接线、暂停/取消/恢复、返工合同、OpenAPI/UI 和真实三路 smoke 尚未完成；父工作包和全体最终验收保持原状态。
