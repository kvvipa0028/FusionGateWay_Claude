# WP-15-QUOTA-API-01 受管理鉴权的项目额度视图

状态：子项 done；WP-15 in_progress。

新增项目 quota cache-only GET 与显式 route refresh POST，使用可信 SetQuotaSources 登记。登记绑定项目 configuration revision、RouteRef 与 account/workspace 等精确 quota identity，HTTP 不接受 URL/token/任意 workspace 等配置；热替换同 revision 登记被拒绝，项目修订使旧来源/缓存失效。查询当前用途、Management issuer 与来源身份在采集前后及响应前重查；真实采集器还必须在出站边界独立检查。

缓存读取重评估 source age，不刷新时间，不自动轮询。首次查询失败保留 auth_required 等状态，无观测不制造百分比；失败信息固定且不返回原始异常。GroupPools 只合并已验证物理池，不累加 aliases，更晚的不完整数据不会被较早 available 覆盖。最多八个全服务 Fetch Worker，与 Broker 十秒 deadline/同 Identity 合并配合，HTTP 断开不释放活跃采集名额，采集器实际返回才释放。Broker 额外保留既有固定 ErrBusy，保证全服务容量拒绝为 429，不回显依赖错误。

12 项 API 测试覆盖实际 loopback HTTP、无调用 GET、来源时间和 stale、越权请求、配置/身份撤销、失败脱敏、首次授权失败、来源登记/能力/热替换、刷新合并与 Management 撤销、HTTP 取消后的实际 Worker 名额、全服务容量上限、共享池部分观测和采集器 Context 中的 issuer/deadline。初始 RED 是未实现 DTO/入口。第一轮未知项目误返回 500，修正为固定 404；query token 预期误写为 400，依据既有 WP-06 requestBoundary Forbidden/403 合同纠正测试，未修改或放宽鉴权。另补空观测 pools 数组合同断言，RED 揭示 null 编码，最终 GREEN 修正为空数组。采集器出站授权的 Context 测试 RED 揭示 Broker 后台 Context 没有 issuer 值；用 Broker 取消/deadline 与已验证 issuer 值组合修复，不继承 HTTP 断开。

完整 Fusion tagged race 272 PASS、8 SKIP；CLI/GUI build 与全仓 vet 通过。新 quota 采集器均为 fixture，实际 HTTP 指管理 Handler 的 loopback transport，不代表真实 GLM quota 出站；本项真实模型/额度调用均为 0。GLM Reader 的真实 CN 单次查询证据仍独立保留于 WP-14。Jev off。

OpenAPI draft 增加两条 quota 路径、状态与视图结构，YAML/路径参数检查通过，完整标准/字段合同和其余 WP-15 endpoints 尚待补全。产品 bootstrap/listener/GUI、真实来源注册、预设/暂停/恢复与最终验收继续实施。
