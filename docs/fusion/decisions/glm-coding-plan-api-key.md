# GLM Coding Plan API key 接入决定

2026-10-03，用户先说明“Glm使用api key”，随后确认“Coding Plan 编码套餐”。结合此前的中国大陆地区决定，本轮 GLM 路线确定为 CN Coding Plan + API key。这里只记录接入选择，没有接收、读取或写入真实 key。

## 执行合同

- 认证使用 API key；不将 GLM 真实接入绑定到网页登录。实际 key 在后续运行配置阶段由本人通过本机私有入口提供，文档、日志和 Git 只保存 credential reference 或脱敏标识。
- 编程套餐与普通开放平台 API 分别登记，冻结 billing route，不把 key 的认证方式当作套餐、额度或费用已核验的证据。
- 按现有 WP-12/WP-14，Agent 选择已安装的 Codex 作为受管 GLM 编码宿主候选，复用进程/RPC 层，保持 GLM 配置、认证、模型目录及额度池独立。这个选择不是用户指定的阶段模型，也不代表 Adapter 已实现或通过真实验证。
- Coding Plan 只用于官方支持工具和允许场景。Fusion 的工程任务通过实际编码宿主运行；不能因为端点连通就把自建网关直接模型调用记成获准套餐路线。二次封装用途及实际 Runtime 能力仍须按 WP-08/WP-14 准入。
- 地区使用 CN；不自动换成 Z.ai Global。Coding Plan 额度耗尽、未知、认证失败或真实计费未确认时按原准入合同处理，不默换普通按量 API。
- 不预选具体 GLM 模型或推理档位；继续由五角色配置选择并在实际路线中核验。套餐档位、个人/团队 key、权益与共享池在本人实际接入时据实登记，不能预填为已验证 Pro。

## 官方端点与来源

核对日期：2026-10-03。以下是官方公布的 CN 接口，属于路线合同参考，不是本轮已经配置或发起调用的地址。

| 路线/协议 | Base URL | 用途边界 |
|---|---|---|
| Coding Plan / OpenAI Responses | `https://open.bigmodel.cn/api/v1` | Codex 宿主候选采用的协议；按实际兼容性准入 |
| Coding Plan / Chat Completions | `https://open.bigmodel.cn/api/coding/paas/v4` | 官方支持工具的编程套餐接口 |
| Coding Plan / Anthropic Messages | `https://open.bigmodel.cn/api/anthropic` | 官方支持工具的对应协议接口 |
| 普通开放平台 / Chat Completions | `https://open.bigmodel.cn/api/paas/v4` | 独立普通 API 计费路线；本轮没有选择，禁止作为自动 fallback |

[官方接入工具说明](https://docs.bigmodel.cn/cn/coding-plan/tool/others)列出 Codex，并给出三种编程端点；[Coding Plan FAQ](https://docs.bigmodel.cn/cn/coding-plan/faq)区分编程套餐场景与自建应用的标准 API 使用；[HTTP API 文档](https://docs.bigmodel.cn/cn/guide/develop/http/introduction)说明 API key 鉴权及普通 API 接口。团队套餐 key 与平台其他 key 的适用范围也须在实际登记时区分。

## 本轮源码核对与未验证项

- `internal/provider/presets.go:111` 已有 Zhipu GLM 的 `api` 与 `coding` 预设，二者端点分别登记；现有 `coding` 预设提供 Responses 地址。无需为本次选择重复创建供应商。
- `internal/provider/planquota.go:33` 的 `planQuotaSourceOf` 已识别 CN 与 Global 额度查询来源；这些原版机制不等于 Fusion 的套餐准入、严格锁定或真实计费证明。
- 本轮只修改决定、需求和预审记录。没有运行真实 GLM 请求、配置 key、启动宿主或修改应用行为；WP-14 和相关验收仍是 planned/not_run。
- 先前五项预审的设计选择已记录完整。真实 OpenAI/X 登录、GLM 私有 key 提供和三路 smoke 证据属于后续执行输入，缺失时阻断依赖它们的真实验收，不把离线 fixture 验证冒充真实接入。
