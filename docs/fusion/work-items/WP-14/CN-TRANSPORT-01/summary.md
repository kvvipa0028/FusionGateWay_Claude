# WP-14-CN-TRANSPORT-01：固定大陆 Messages 单次传输

子工作项 `done`，父 WP-14 保持 `in_progress`。新增 `NewCNTransport()`，可信控制器可直接连接已有 CallGate/Adapter。没有接收 caller URL、代理、TLS 配置或 fallback 参数；只有内部测试替换 Dial/trust root 到本机 TLS fixture。实际生成路线仍需完整 Registry 证据与产品注册。

仅允许 HTTPS `open.bigmodel.cn` 的 POST `/api/anthropic/v1/messages`，query 为空或精确 beta=true。拒绝 Global host、显式 port、userinfo/fragment/opaque/RawPath、Host 冒充、其他 endpoint、RequestURI、可重放 GetBody、空/超长 body、Transfer-Encoding/trailer 与非固定 header。两个认证 header 必须为同一合法 key，固定 Content-Type/Accept/Anthropic-Version；不会导入 Cookie 或代理认证。正文的模型/effort/tools/scope 继续由既有 CallGate 验证，传输不替代 Permit。

独立 HTTP/1 TLS transport：系统信任、TLS >=1.2、固定 host 的默认 hostname 验证、无环境代理、无 keep-alive 复用、无 HTTP/2 alternate、无透明解压、无 redirect client、无重试/fallback。单 host 两连接、header 16 KiB、dial/TLS 各五秒；最长一分钟 context 覆盖到 response body EOF/Close。Response EOF/Close 取消本次 context；错误及 body Read/Close 错误均静态，不回显 key 或上游消息。上游 SSE 的八 MiB、完整结构、实际模型与身份检查继续由 CallGate 在交给 Native 前完成。

## 验证

- 缺失 API 先 RED，三个 targeted race tests PASS：十七种拨号前拒绝、四个实际 TLS success/redirect/fresh-connection failure/429、未受信 TLS 不发送 HTTP。
- 实际 pinned Native Adapter 的八个场景改接 CNTransport 与本机 TLS 假上游：design 1 次、implementation/Edit 创建 2 次、SDK retry 2 次、cancel in flight 1 次；三个身份/额度负例与 key rotation 均启动前拒绝、0 次上游请求。每请求数等于持久预算调用数，实际停止/Release 已核对；所有 Native flags=false、真实模型调用数 0。
- 初次 TLS fixture 的取消用例在 Native 实际回收后挂住服务 cleanup：假上游未读完请求 body，Go net/http 只有在 body EOF 后启动 background disconnect read。保留原日志并明确记录：确认 test binary PID/父链、Native 已 terminal 后，仅终止自己的 test binary；这一轮 exit 1，不能标通过。修正 fixture 为限长读取/关闭请求 body，再以 30s test timeout 重跑，八个场景 PASS、cleanup 正常。生产逻辑和断言没有放宽。
- 完整 Fusion tagged race：223 个 top-level PASS、7 SKIP、0 FAIL；Go 1.26.3 CLI/GUI 编译及全仓 Fusion/nogui vet exit 0。完整回归发生在 fixture 接收流程修正前；之后只有该测试接收流程改变，实际 Native suite 已重新编译/运行并通过，产品源码未变。

本项没有真实生成请求；本轮之前 PRIVATE-CREDENTIAL-01 的真实额度 GET 仍是独立诊断。账号/pool/模型/effort/计费/二次封装用途与 generation 准入未完成，不能把 TLS fixture 或 native result 标签作为这些事实的证明。Jev off。
