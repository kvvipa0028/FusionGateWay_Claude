# WP-15-EXECUTION-API-01 受管理鉴权的阶段执行接口

状态：子项 done；WP-15 in_progress。内部 Handler 新增 start、run read 和 cancel，调用绑定同 Store 的可信 Controller，禁止热替换。任务读取新增 strong ETag，包含 plan revision/generation/state；运行响应使用安全 RunView，不暴露 Owner/lease/Native session/Spec/输出/秘密。新启动只接受 role、单个原 task If-Match 和 key，重复请求读取原 durable intent，完成或 unknown 后不新启动。

StartAuthorized 用原 issuer 的 ManagementCurrent 在提交前慢预检查及 Inspection 前后重查，Resolver 或 Inspector 撤销管理身份不会留下 startup intent。提交后 execution lifetime 独立于 HTTP，模型权限仍由 Adapter/Permit 逐次检查。取消核对 task/run/generation/当前 Task 条件，不能接管另一个任务/attempt；终态仅幂等读取。失败 intent 返回 409 与当前 RunView/Created，容量继续 held，不隐藏成未提交。

10 个 API tests 覆盖合法链路、强条件/缺失/旧状态、15 类禁止请求字段、stage 与 management 分界和重复 header、预检查期间的撤销、同 generation 下终态 ETag 改变、同 key 冲突与部分失败脱敏、跨 task/gen、控制器 bootstrap 隔离，以及实际 loopback HTTP。初始 RED 为缺少入口/DTO；第一轮唯一失败是测试错误地要求 stage fgs_ 返回 401。依据 WP-06 的已存在 scope contract（Forbidden/403），修正测试到 stage 403、无/错误管理凭据 401，没有改变鉴权实现或放宽失败断言。随后 GREEN。

完整 Fusion tagged race 258 PASS、8 SKIP；CLI/GUI build 和全仓 vet 通过。新接口实际 HTTP 使用合成 Runtime/退出证明，不代表新的实际进程 stop proof 或真实账户；前项 Controller Native 证据保持独立。没有真实模型/额度查询，Jev off。

新增 docs/fusion/openapi-fusion.yaml 是四个已实现执行相关路径的 partial draft，PyYAML 解析通过；不是完整 OpenAPI 标准验证，也不是全 WP-15 交付。已有其他 Handler、尚未实现的 pause/resume/presets/quota 和完整 schema 仍待补全。产品 listener/GUI、实际配置、恢复/返工和完整 Gate 继续实施。
