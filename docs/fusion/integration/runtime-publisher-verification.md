# 固定 Runtime 发布者核验

2026-10-05 已对实际安装的 Codex 0.160.0、Grok 1.0.48、Claude Code 2.1.287 完成官方发布内容及 Apple Developer ID 核验。未更新 Runtime，没有执行供应商 CLI、安装脚本或下载的程序，也没有读取认证、调用模型或查询额度。[实际结果与回归证据](../work-items/WP-17/PUBLISHER-01/summary.md)。

| Runtime | 官方内容核验 | 本机签名约束 |
|---|---|---|
| Codex 0.160.0 | OpenAI 仓库的精确稳定 release，资产 URL、SHA256 和大小一致；压缩包内唯一正常文件的 SHA256 与安装文件一致 | Apple Developer ID，`codex`，Team `2DC432GLL2` |
| Grok 1.0.48 | 官方 x.ai 固定版本 gzip 下载，完整解压后的 SHA256 与安装文件一致 | Apple Developer ID，`xai-grok-pager`，Team `5Y6N3AJ54S` |
| Claude Code 2.1.287 | 精确版本 manifest 中的架构、文件、大小及 SHA256 一致；独立私有 GPG 目录验证 manifest 签名及官方固定指纹 | Apple Developer ID，`com.anthropic.claude-code`，Team `Q6L2SF6YDW` |

核验来源：[Codex 官方 release](https://github.com/openai/codex/releases/tag/rust-v0.160.0)、[Grok 官方安装说明](https://docs.x.ai/build/overview)及[安装脚本](https://x.ai/cli/install.sh)、[Claude 官方完整性及签名说明](https://code.claude.com/docs/en/setup#binary-integrity-and-code-signing)。这里只读取 Grok 安装脚本以确定官方发布路径，没有执行脚本中的认证、安装或更新逻辑。Claude 的固定 GPG 指纹为 `31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`。

## 复现

前置：当前 Apple Silicon macOS、Python 3、本机 `/usr/bin/codesign`；Claude 还需要已经安装的 `/opt/homebrew/bin/gpg`。三个固定版本必须位于脚本记录的当前用户路径，文件所有者、权限、架构、SHA256 和原 inode 必须匹配。不自动安装工具或回退到其他版本。

在 implementation 根执行，每次使用新的报告路径：

```sh
mkdir -p .fusion-dev/publisher-check
python3 scripts/fusion/verify-runtime-publishers.py \
  --runtime all --report .fusion-dev/publisher-check/all-new.json
python3 -m unittest discover -s scripts/fusion/tests -v
```

也可显式选择 `--runtime codex`、`grok` 或 `claude`。脚本只向固定公共 HTTPS 发布源发送无认证 GET；关闭环境代理，不接受 URL/账号/版本覆盖，不自动重试或跟随外部域名。Codex 的 GitHub release 重定向仅允许官方 release-assets 主机，下载内容仍须满足精确官方资产摘要。Grok 只允许官方固定版本路径及原安装器公布的同名公共存储路径。单个下载限制 160 MiB，解压内容限制 512 MiB，每个 Runtime 在下载块之间检查五分钟期限、单次网络等待最多三十秒。

下载文件仅存在于临时文件；不解压安装文件，不替换 launcher。外部校验工具使用临时私有 HOME，GPG 不访问用户 keyring、自动获取 key 或启动 agent。原执行文件在下载前后重新检查 inode、内容、权限与签名。已有报告在任何核验或下载前拒绝；失败退出 1，不生成成功报告、不自动重试。网络失败、版本不符、替换/篡改、错误发布者或签名都应保持未确认；检查环境后由操作者明确重新运行。

成功报告记录固定版本、真实文件 SHA256、签名标识、官方源/内容摘要和时间。报告可保留用于审阅；临时文件在结束后关闭或删除。本机真实成功记录为 [codex-live.json](../work-items/WP-17/PUBLISHER-01/codex-live.json)、[grok-live.json](../work-items/WP-17/PUBLISHER-01/grok-live.json)、[claude-live.json](../work-items/WP-17/PUBLISHER-01/claude-live.json)。

## 证明范围

这是一份发布者与文件来源的时点观察，不是持久准入合同。JSON 没有可信导入入口，不能仅凭报告字段或 hash 设置 Registry Evidence、Inspection、账号、计费或额度池已验证。生产 Runtime 仍须在启动及运行中核对原文件身份和全部路线证据；文件更换、版本升级须重新核验，不自动采用 latest。

Apple Developer ID 约束证明本机文件的签名发布者；本次没有另做 Gatekeeper/在线 notarization 验收。Claude 的发布者为 Anthropic，不能因此推断 GLM 的账号、Coding Plan 权益或计费已验证。账号登录、精确模型/effort、全调用控制、物理额度池、受控工程读写/测试/取消仍需真实核验。[当前三路线矩阵](live-matrix.md)列出剩余缺口；WP-17、Gate A 和整体目标未完成。UI 及 Jev off 保持原状态。
