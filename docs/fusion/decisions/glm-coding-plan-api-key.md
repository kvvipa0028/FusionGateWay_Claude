# GLM Coding Plan API key 接入决定

2026-10-03，用户先说明“Glm使用api key”，随后确认“Coding Plan 编码套餐”。结合此前的中国大陆地区决定，GLM 路线确定为 CN Coding Plan + API key。用户随后指定“接入claude code里”，GLM 编码宿主确定为 Claude Code，替代上一轮 Agent 选定的 Codex 候选。最初决定只记录选择并提供私有模板；用户提供 key 后的实际执行结果见本文末尾。

## 执行合同

- 认证使用 API key；不将 GLM 真实接入绑定到网页登录。实际 key 在后续运行配置阶段由本人通过本机私有入口提供，文档、日志和 Git 只保存 credential reference 或脱敏标识。
- 编程套餐与普通开放平台 API 分别登记，冻结 billing route，不把 key 的认证方式当作套餐、额度或费用已核验的证据。
- 按用户指定，WP-14 使用 Claude Code + GLM CN Coding Plan API key，调用 Anthropic Messages 端点。复用 WP-11 的进程生命周期与事件合同，新增 Claude Code JSON/stream-json 协议适配；不沿用 WP-12 的 Codex RPC。GLM 与本人日常 Claude Code/ChatGPT 的配置、认证、模型目录及额度池分别管理。宿主选择不代替五角色模型指定，也不代表 Adapter 已实现或通过真实验证。
- Coding Plan 只用于官方支持工具和允许场景。Fusion 的工程任务通过实际编码宿主运行；不能因为端点连通就把自建网关直接模型调用记成获准套餐路线。二次封装用途及实际 Runtime 能力仍须按 WP-08/WP-14 准入。
- 地区使用 CN；不自动换成 Z.ai Global。Coding Plan 额度耗尽、未知、认证失败或真实计费未确认时按原准入合同处理，不默换普通按量 API。
- 不预选具体 GLM 模型或推理档位；继续由五角色配置选择并在实际路线中核验。套餐档位、个人/团队 key、权益与共享池在本人实际接入时据实登记，不能预填为已验证 Pro。

## 官方端点与来源

核对日期：2026-10-03。以下是官方公布的 CN 接口。后续连接诊断只使用其中 Anthropic Messages 端点。

| 路线/协议 | Base URL | 用途边界 |
|---|---|---|
| Coding Plan / OpenAI Responses | `https://open.bigmodel.cn/api/v1` | 官方其他工具接口；本轮 Claude Code 路线不采用 |
| Coding Plan / Chat Completions | `https://open.bigmodel.cn/api/coding/paas/v4` | 官方支持工具的编程套餐接口 |
| Coding Plan / Anthropic Messages | `https://open.bigmodel.cn/api/anthropic` | 用户指定的 Claude Code 路线采用 |
| 普通开放平台 / Chat Completions | `https://open.bigmodel.cn/api/paas/v4` | 独立普通 API 计费路线；本轮没有选择，禁止作为自动 fallback |

[官方接入工具说明](https://docs.bigmodel.cn/cn/coding-plan/tool/others)列出 Claude Code，并给出三种编程端点；[Coding Plan FAQ](https://docs.bigmodel.cn/cn/coding-plan/faq)区分编程套餐场景与自建应用的标准 API 使用；[HTTP API 文档](https://docs.bigmodel.cn/cn/guide/develop/http/introduction)说明 API key 鉴权及普通 API 接口。团队套餐 key 与平台其他 key 的适用范围也须在实际登记时区分。

## Claude Code 私有配置准备

本机 `/Users/zhaojianzhi/.local/bin/claude --version` 返回 `2.1.287 (Claude Code)`。`--help` 已核对 `--settings`、`--setting-sources`、`--model`、`--permission-mode`、`--input-format`、`--output-format` 和 `--strict-mcp-config` 等接口存在。这不是模型调用或隔离能力验收。

不含真实 key 的模板见 [glm-claude-settings.example.json](../examples/glm-claude-settings.example.json)。真实配置由后续 WP-02/WP-14 放在仓库外的 Fusion 私有目录，文件仅本人可读写，不覆盖日常 `~/.claude/settings.json`。关键项：

- `ANTHROPIC_BASE_URL` 固定为 `https://open.bigmodel.cn/api/anthropic`。
- `ANTHROPIC_AUTH_TOKEN` 注入本人的 GLM Coding Plan key，作为 Bearer 凭据；不写进可提交配置或命令行参数。
- 模板中的 `ANTHROPIC_MODEL` 是显式占位符。启动真实任务前必须替换为阶段冻结的实际 GLM model ID，同时核验其能力；不能把 Claude 的 opus/sonnet/haiku 名称当成实际执行模型证据。
- 实施时隔离 HOME、Claude 配置及凭据来源，过滤其他 provider 的认证、路由和默认模型变量，并处理设置优先级、后台/摘要/子 Agent 的模型调用以及自动 fallback。静态模板和 CLI 参数不能单独证明 strict locked；每条实际调用仍受 WP-07/WP-14 准入。

[Claude Code 环境变量文档](https://code.claude.com/docs/en/env-vars)说明 Base URL 与 Bearer token；[设置文档](https://code.claude.com/docs/en/settings)说明 `env`、`--settings` 及配置优先级。作出宿主决定时尚未写入真实配置或启动 GLM 会话。

## 本轮源码核对与未验证项

- `internal/provider/presets.go:111` 已有 Zhipu GLM 的 `api` 与 `coding` 预设，二者端点分别登记；现有 `coding` 预设同时提供 Anthropic 地址。无需为本次选择重复创建供应商。
- `internal/provider/planquota.go:33` 的 `planQuotaSourceOf` 已识别 CN 与 Global 额度查询来源；这些原版机制不等于 Fusion 的套餐准入、严格锁定或真实计费证明。
- 宿主选择阶段只修改决定、需求和预审记录，没有运行真实 GLM 请求。后续诊断仍不代表 WP-14 完成；WP-14 和相关最终验收仍是 planned/not_run。
- 先前五项预审的设计选择已记录完整。真实 OpenAI/X 登录、GLM 私有 key 提供和三路 smoke 证据属于后续执行输入，缺失时阻断依赖它们的真实验收，不把离线 fixture 验证冒充真实接入。

## 2026-10-03 私有录入与真实连接诊断

用户通过本机隐藏输入窗口录入 key，保存至仓库外私有文件，目录 `0700`、文件 `0600`。元数据和使用说明见 [private-credentials.md](../private-credentials.md)。没有将真实 key 放进聊天、命令行参数或 Git。

使用本机 Claude Code `2.1.287`，以临时 HOME/XDG/Claude 配置目录、环境白名单、空项目目录及 `--tools ""` 执行诊断。该版本 `--bare` 要求 `ANTHROPIC_API_KEY`；诊断将同一个 GLM key 注入 `ANTHROPIC_API_KEY` 与 `ANTHROPIC_AUTH_TOKEN`。本地假服务观察到 X-Api-Key 与 Bearer 头均匹配 fixture，唯一请求模型为 `glm-5.3`，工具数为 0。该观察只约束这次诊断，不替代完整 Runtime 的控制证明。

真实请求固定至 `https://open.bigmodel.cn/api/anthropic`，Claude Code 返回 `FUSION_GLM_OK`，退出码 0。`glm-5.3` 仅为本次诊断选择，不将其写入五角色默认绑定。日常 Claude Code 配置及认证文件的前后 hash 未变化。

脱敏结果见 [GLM-PROBE-01/live-probe.json](../work-items/GLM-PROBE-01/live-probe.json)。Native `modelUsage` 不能独立证明上游实际模型或计费，记录保留 `upstream_reported_model=unknown`、`billing_verified=false`、`quota=unknown`。套餐档位、共享池、额度与 strict locked 仍未核验，WP-14 Adapter、WP-17 三路 smoke 和 Gate A 均未完成。

复现命令只读取私有凭据，输出脱敏结果；执行会消耗一次编程套餐请求：

```sh
python3 scripts/fusion/glm-claude-probe.py --model glm-5.3
```

该命令为无工具连接诊断，不执行项目代码，也不覆盖 `~/.claude/settings.json`。返回认证错误、超时或不符合预期的 Native 结果时停止，不切换模型、地区或普通按量 API。
