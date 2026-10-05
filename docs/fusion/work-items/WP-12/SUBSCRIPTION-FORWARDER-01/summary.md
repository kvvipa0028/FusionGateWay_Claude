# WP-12 · SUBSCRIPTION-FORWARDER-01

完成可信 Codex ChatGPT 订阅 HTTPS Forwarder 和官方实际模型来源接入；父 WP-12、WP-17、Gate A 和整体目标仍未完成。基线 `efb2b0335d8504ef5059f245b203dd212bb24165`，工作区原本干净；未新增 Agent。

实现固定端点、FileCredential、受检公共系统 CA、单次 fresh HTTP1、完整有界响应与交付前凭据重查。官方 0.160.0 的模型来源为 HTTP OpenAI-Model 或 SSE response.headers，而普通 response.model 不作为 ServerModel 证明。原 parser 未接受 nested headers，本次增加有界模型 header 校验、所有后续声明一致性、禁止缺失来源补造；普通 model 存在时仍需匹配。反射标记上限与16KiB token 对齐，原文/JSON解码反射和超限仍拒绝。合同见 [订阅 Forwarder](../../../contracts/codex-subscription-forwarder.md)，来源见 [public-source-info.json](public-source-info.json)。

## 验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 首次 RED | Forwarder 类型/构造函数缺失，编译失败 | [初始 RED](initial-red.log) |
| 首轮取消 fixture | 29 个子场景已 PASS，但测试服务等待自身 Request Context，cleanup 未结束；owned SIGQUIT 后实际 exit1/wait | [保留失败](cancel-fixture-blocked.log) |
| 最终目标场景 | 8 主测试、48 子场景 PASS，0 FAIL/0 SKIP；从本次最终四模块实际事件流提取 | [目标日志](final-targeted.log) |
| 受影响四模块 race | 188 主测试、631 子场景 PASS，15 SKIP，0 FAIL，4 package exit0 | [模块回归](dependent-race.log) |
| 固定 Codex Native 模型 header | 2 主测试、10 子场景 PASS，0 FAIL/0 SKIP，actual exit0；真实 wait/StopProof/release | [Native 日志](native-model-headers.log) |
| 有效 mutation | 缺模型来源时改用请求模型、启用默认 client redirect、取消交付前凭据重查、恢复旧4KiB反射上限，各对应一个行为子场景实际 FAIL/exit1 | [来源](mutation-model-proof.log)、[redirect](mutation-redirect.log)、[交付](mutation-delivery.log)、[长 token](mutation-marker-bound.log) |
| Go1.26.3 CLI/GUI/full vet | 三项实际 exit0 | [构建报告](build-results.json) |

取消 fixture 的原因是 HTTP Connection: close 场景中服务端不一定在 handler 返回前观察客户端断开；它不能只等待自身 Request Context。补上测试独立 release、明确 Send 一秒以内返回断言及60秒总测试期限；最终250ms调用期限场景通过，没有扩大产品网络或取消权限。首次 owned runner 进程匹配因 macOS Python 的显示名称未命中而拒绝 signal，随后按专属脚本参数及完整 parent 链定位唯一测试子进程发送 SIGQUIT，原 runner 实际 wait/reap；未终止其他任务。该阻断不当作产品测试成功。

真实 TLS 回环验证固定 Host/path/Bearer/account/runtime、无 cookies/API key、body 不变、HTTP/SSE模型来源、307/303不跟随、401/429私有错误不透传、fresh连接失败不重试、TLS不可信拒绝、body取消、8MiB界限、原文/JSON秘密反射、目标/effort/权限变更、请求中的秘密和 header 控制字符拒绝、缓存在发送前/发送期间/交付时轮换。组合 CallGate/Manager 通过两次独立 Permit 执行429后新请求，并验证撤销后没有额外 TLS 请求；这是合成权限/计数证明，不是实际账号准入。

Native fixture 按官方来源在 response.created 增加 OpenAI-Model headers，保留原 model/全部状态及断言。相同固定0.160.0真实进程完成原 raw Peer 与类型化 Gateway 的文字、429、异常重试拒绝、预算、取消/interrupt 等10场景，真实停止和预留释放通过；其上游是已有合成 Forwarder，不把它描述成真实供应商 TLS 或本人账号调用。

15项常规回归 SKIP 是14个显式 opt-in 的其他 Native/HTTP/来源/交接 fixture和1个浏览器宿主；本次显式 Native 测试另行执行，两者不混算。Mutation 使用 Go overlay，不修改生产文件；实际失败来自行为断言，不是编译错误。[test-results.json](test-results.json)记录命令、原始 ignored JSONL 指纹和计数。日志从实际 Output 事件顺序导出并规范化行尾空白，原事件流保留；最终目标日志明确为四模块事件流中的筛选，并非重复运行后另造 exit。

## 未完成与范围

本人独立 Codex 缓存当前仍不存在，本轮真实登录、供应商模型与额度请求0，没有解析真实JWT、声明账号/地区/费用/池或打开 Registry/Inspection。产品 Factory、可信账号/套餐/计费/额度、续期和完整工具/写入/恢复、真实工程 Smoke 与最终60类验收仍待完成。

UI、Store schema、依赖、原 Magpie assets 和工作包/最终验收状态未改；Jev off。[scope-check.json](scope-check.json)记录敏感字节排除、范围与文档检查，[artifacts.json](artifacts.json)记录本次提交文件指纹。Graph 已刷新实际新增/变化符号；未以组件测试替代完整目标验收。
