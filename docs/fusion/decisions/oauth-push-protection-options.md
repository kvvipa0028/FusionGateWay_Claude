# 上游 OAuth 常量的推送处理方案

状态：**用户于 2026-10-03 选择 A，按下列合同实施和验证**。原方案日期：2026-10-02。当前配置说明见 [Google OAuth 外部配置](google-oauth-external-config.md)。

本方案只处理基础源码首次推送的 GH013 / Push Protection 拒绝，不替代 WP-01–WP-30 的需求或验收，也不授权真实账号登录。

## 已核实的范围

- 当前工作分支为 fusion/implementation，本地 HEAD 为预审提交 16d0aa8；源代码导入提交为 7abfcb3。远端尚无该分支，没有需要强推覆盖的共享提交。
- GitHub 命中 google.go 的两个 client ID 和两个 client secret。按原值检查了 1,797 个 Git 跟踪文件，四处命中均在 internal/provider/google.go:72/73/81/82；脱敏结果见 [push-protection-report.json](../push-protection-report.json)。该检查不读取 .git、构建缓存、用户登录文件或未跟踪文件。
- google.go 与锁定的上游版本完全相同；本次没有更改 GitHub 保护设置或申请放行，没有把被拦截的值写入本方案。

## 选择 A：移出硬编码，使用外部客户端配置

推荐这个方案。源代码不再提供 Gemini CLI / Antigravity 的内置 OAuth 客户端值；需要使用其 Google 订阅路径时，由用户配置获准的客户端。相关客户端能否用于 Code Assist 仍需供应商/实际账号验证，不能凭配置存在认定支持。

**具体行为：**

1. googleApp 保留 agent、展示信息、scopes、callback 和 API 路线元数据。client ID/secret 从私有配置读取，不从上游常量、日常 CLI 配置、shell 环境或另一个 Google app 自动补齐。
2. 缺配置、只填一半、格式错误、文件权限不合格时，对相应订阅路径返回明确的未配置状态。不能发登录、换 token、刷新、导入、额度或模型请求，也不能改用另一个客户端。
3. 外部配置属于凭据，不进入 Git、日志、错误正文、任务 JSON 或普通备份。拟放在独立产品配置目录的 google-oauth-clients.json；具体目录以 WP-02 的隔离合同为准，开发验证仅使用临时 HOME 下的假配置。
4. 配置加载形成明确版本。初版不支持运行中热换 OAuth 客户端；改变客户端配置需停止相关任务并重新启动/准入，不复用旧身份的 session、quota 或 token cache。
5. 缓存使用前先检查客户端配置及身份。不得因已有有效 access token 跳过未配置判断；token 缓存归属必须包含 app/client 身份，不能仅以 refresh token 判断归属。
6. 保持 Gemini 与 Antigravity 的账号识别边界；不能因两个 client ID 都为空而把账号视为同一客户端。迁移旧账号时缺少客户端归属证据，保持未验证，不猜测补齐。

**预计修改范围：**

| 文件/入口 | 必要修改 | 依据 |
|---|---|---|
| internal/provider/google.go / googleAppOf | 移出四个常量、接入配置快照、区分元数据与可执行客户端 | 多个调用方用该入口识别 app，不能把未配置全部误报为未知 provider |
| internal/provider/google.go / geminiOwnLogin | 配置和身份检查先于借用原生登录态；使用已登记的客户端身份 | 当前以 geminiApp.clientID 检查 id_token 的 azp/aud |
| internal/provider/google.go / googleAccount.token | 未配置拒绝先于 cache 命中；冻结客户端和缓存归属 | 当前先返回有效缓存 token，再构造刷新请求 |
| internal/provider/google.go / googleExchange | 交换前校验，错误不包含凭据值 | 当前直接把 app 的 ID/secret 放入 token 表单 |
| internal/provider/signin.go / begin、googleDone | 创建登录流程及回调时核对同一客户端版本 | 不能在开始登录和回调之间重新解释为另一个客户端 |
| internal/provider/google_import.go / ImportGoogleAccounts、importGoogleAccount | 导入前校验客户端；不擅自刷新另一 app 的账号 | 导入会直接发送 refresh_token 表单，并保存认证结果 |
| provider 的 Google fake/test helpers | 明确配置两组不同的假客户端，保留原账号边界断言 | 原测试依赖全局 app 的不同 client ID，删除常量后不能削弱该断言 |
| docs/fusion 的来源和基线记录 | 记录这项有意偏离上游的改动、实际验证结果与限制 | 修改后不能再声称全部 Go 源码与上游一致 |

上述是实施边界，函数签名和新文件由实际调用方及测试结果决定。未发现必要性时不扩大到其他 provider、协议翻译或 UI 重构。Google API Key 路线与这两条 OAuth 订阅路线分别验证。

### 选择 A 的验收条件

所有新增行为先用 fake 测试观察失败，再实现并验证。下列测试当前均为 **not_run**。

| case | 场景 | 必须观察的结果 |
|---|---|---|
| OAUTH-A01 | 配置文件缺失、空客户端、只配置 ID 或 secret | 明确未配置；对应假 HTTP 上游的请求次数为 0 |
| OAUTH-A02 | 缺配置，但旧账号有仍有效的缓存 token | 仍然拒绝；不得直接使用缓存放行模型或额度请求 |
| OAUTH-A03 | 不同假客户端分别配置给两个 app | 登录/交换/刷新使用指定 app 的完整字段，不混拼或 fallback |
| OAUTH-A04 | Gemini id_token 的 azp/aud 指向另一客户端 | 保持拒绝；同时覆盖没有 id_token 的旧格式，不能因空 ID 放宽判断 |
| OAUTH-A05 | 客户端配置变更，旧 token/session/cache 仍存在 | 旧身份不当成新身份使用；重启/重新准入后形成新版本 |
| OAUTH-A06 | 未配置时开始登录、处理回调或导入账号 | 不开启可执行授权流程，不交换/刷新 token，错误不包含 ID/secret/token |
| OAUTH-A07 | 普通 provider/gateway 及 Google 协议转换 | 原相应断言继续通过；不能为获得绿灯删测试或关闭验证 |
| OAUTH-A08 | 检查将推送的整段提交历史 | 四个原值在全部拟推送的 commit 中均无命中，而非只在工作区消失 |
| OAUTH-A09 | 更新后的 Go 构建/vet/targeted regression | 使用 Go 1.26.3；原始命令、退出码、日志 hash 与新代码版本关联 |

必要回归范围包含 provider、gateway 及 CLI 登录/导入相关入口。既有重点断言包括 TestGeminiOwnLoginOnlyGeminiCLIsClient、TestGeminiOwnLoginRefreshesInMemory、TestImportGoogleAccounts、Google fake 下的模型/额度测试，以及 Code Assist 协议转换测试。这是测试范围清单，尚未运行或证明通过。

### 选择 A 的 Git 处理

首次推送被拒绝，源导入提交尚未发布。只对本任务的未发布提交处理历史中的常量，保留初始提交、origin 和上游只读历史；不改 protected main、不强推共享分支、不删除用户的未跟踪文件。

重新推送前，用父提交到候选 HEAD 的完整范围检查原值出现位置。更新锁记录的有意差异，再运行受影响验证。若后续保护检查发现新的命中，先核实来源及影响，不改用编码/拆字符串隐藏凭据。

## 选择 B：保留上游原文，申请本次保护例外

这个方案保持当前源码和默认 Google 登录行为，但必须有用户明确授权，并由 GitHub 按其规则允许本次命中。不能关闭仓库整体保护、伪造“测试数据”分类或把审批链接当成已经放行。

放行后只推送已经核验的工作分支；它不授权访问 Google 账号，不替代 Fusion 的严格路由、凭据隔离或真实套餐核验。被扫描值的公开来源也不能证明其适用于 Fusion 的自有产品身份。

## 等待的决定与继续位置

用户只需选择已提出的第五个问题，无需再次发送凭据。选择 A 后按以上范围做变更与验证；选择 B 后按明确授权申请本次例外。其余逐项答案已在 [预审记录](../preflight-review.md) 留证；GLM 的具体套餐及编码工具仍待补充。本文件表格的 not_run 为方案编写时的状态，实际结果以 OAuth 验证报告为准。
