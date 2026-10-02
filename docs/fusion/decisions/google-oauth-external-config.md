# Google OAuth 外部配置

2026-10-03，用户选择方案 A。本次只处理 Gemini CLI / Antigravity 的 Google OAuth 订阅路径；Fusion 五角色、任务管理与三条目标 Runtime 的工作包仍按实施计划推进。

## 配置位置与格式

配置文件为 `$XDG_CONFIG_HOME/fusion-gateway/google-oauth-clients.json`；未设置 XDG_CONFIG_HOME 时为 `~/.config/fusion-gateway/google-oauth-clients.json`。这是独立的 Fusion 私有目录，当前其余 Magpie 数据目录尚未由 WP-02 隔离。WP-02 实施时统一目录合同，不能复制日常 HOME 或自动提取其它客户端的 OAuth 值。

文件使用以下结构；示例中的值都是占位符，不是可用客户端。只配置需要的应用即可，未配置的应用保持拒绝。

```json
{
  "schema_version": 1,
  "clients": {
    "gemini": {
      "client_id": "YOUR_APPROVED_GEMINI_CLIENT_ID",
      "client_secret": "YOUR_APPROVED_GEMINI_CLIENT_SECRET"
    },
    "antigravity": {
      "client_id": "YOUR_APPROVED_ANTIGRAVITY_CLIENT_ID",
      "client_secret": "YOUR_APPROVED_ANTIGRAVITY_CLIENT_SECRET"
    }
  }
}
```

使用者自行提供获准客户端；具备客户端配置不等于相关订阅 API 已获准或可用，真实 Google 接入尚未验证。当前目标是 OpenAI、Grok、GLM，可以不创建该文件。

## 文件与运行规则

- 配置目录权限为 `0700`，文件为 `0600` 或更严格的仅本人权限。拒绝符号链接、非普通文件、超限文件、未知字段、错误版本、缺少成对字段和两个应用混用同一 client ID。
- 配置在数据根目录首次使用时冻结；每次使用前复核文件。配置变更、删除或变得不可读后，相关路径持续阻断至进程重启。修改配置前先停止相关任务，重启后重新登录或显式导入，不做热替换。
- 未配置时，不开放对应登录流程，不交换/刷新 token，不导入账号，也不以有效旧缓存放行模型或额度请求。Google app 元数据仍可识别，不误报为不存在的 provider。
- 新账号保存客户端版本的摘要，token/project/flags 缓存包含 app 与客户端版本。历史账号缺少归属记录或版本不匹配时保持可见，但不能执行；显式重新登录/导入后形成新记录。
- Gemini 原生文件缺少 id_token 时保留旧格式账号的可见性，其客户端归属仍为未验证，拒绝执行；有 id_token 时须匹配已配置的客户端。
- `azp` 必须是非空字符串；`aud` 可为非空字符串或非空字符串数组，所有已提供的 audience 必须属于同一配置客户端。错误类型、null、冲突或混合 audience 均拒绝，不当作缺失字段回退。
- 当前 Google OAuth 私有配置使用 Unix 权限检查，暂不支持 Windows：Windows 的合成文件权限不能通过此检查，即使实际 ACL 严格也会拒绝。Windows ACL 支持后续单独验证；本轮交付宿主为 macOS。
- 配置文件被 Git 忽略。它不进入任务 JSON、普通归档或错误正文；Google OAuth token 错误正文中的已知客户端及提交凭据会脱敏。备份工具须按 WP-25 将凭据与普通归档分离，该工具尚未实现。

本轮测试仅使用临时 HOME/XDG 和假 HTTP 服务，没有读取真实认证、配置真实客户端、登录 Google 或申请 GitHub 保护例外。验证记录见同目录下的 OAuth 验证报告。
