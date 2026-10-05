# WP-17 · PUBLISHER-01

本组件完成三条固定 Runtime 的真实发布者及官方文件核验，父 WP-17 和整体工程目标仍为 in_progress。基线 `f9d9054ac35512e5ba51d171f4be4cbfd2814127`，原工作区干净。采用现有 executing-plans/TDD 流程；无需新增 Agent。

新增 `scripts/fusion/verify-runtime-publishers.py`，以原已验证 Runtime 版本、文件 hash 和安装路径为 pin。严格 Apple Developer ID 约束与官方固定发布内容同时满足才生成成功观察报告；Claude 另验证官方固定 GPG 指纹与真实 manifest 签名。下载只读无认证、无环境代理、无自动重试、限制域名/路径/大小与等待，不执行 Runtime/安装器/下载文件，不修改 launcher、模型路线、UI、Go 代码或 Jev off。已有报告在核验/下载前拒绝；下载及私有 GPG 目录结束后清理。

## 真实结果

| Runtime | 结果 | 原始观察 |
|---|---|---|
| Codex 0.160.0 | 本机文件仍为原 hash；官方 release 资产摘要/大小与实际压缩包一致，内部唯一正常文件与本机一致；精确 Apple 发布者约束通过 | [codex-live.json](codex-live.json) |
| Grok 1.0.48 | 本机文件仍为原 hash；官方固定版本 gzip 的完整实际文件与本机一致；精确 Apple 发布者约束通过 | [grok-live.json](grok-live.json) |
| Claude Code 2.1.287 | 本机文件仍为原 hash；官方 manifest 版本/大小/checksum、固定 GPG 指纹/签名、精确 Apple 发布者约束全部通过 | [claude-live.json](claude-live.json) |

三个真实命令均 exit 0，原执行文件未变；工具进程已退出，临时文件已清理。没有读取账号/key/token，没有模型调用、额度请求或 Runtime 升级。Grok 官方 installer 只用于只读研究公共发布路径；没有执行其认证、安装或更新逻辑。GPG 仅导入公共 release signing key 至本次私有临时 keyring。公开 manifest、签名及 key 的测试 fixture 与真实报告中的原始内容摘要一致；不是私钥。

## 验证

- [首次 RED](red.log)：9 个测试因缺少真实 verifier 明确失败。
- [初轮 GREEN](green.log)：8 项通过，1 项因 macOS 临时目录 `/var` 与 `/private/var` 的合法规范路径差异失败；测试 fixture 使用真实 canonical path 修正，保留生产禁止 symlink/canonical guard。[修正结果](green-canonical.log)。
- [扩展回归](expanded.log)及[最终全部脚本测试](final-clean.log)：最终 15/15 通过，0 跳过、0 失败，实际 exit 0。覆盖正确签名及错误 Team、真实 GPG 签名及篡改 manifest/错误 key、精确版本/大小/摘要、重复字段/资产、archive traversal/link/duplicate、gzip 上限、文件替换/篡改/symlink/权限、跨域 redirect、完整 bounded download、到期不联网、已有报告不联网。
- [有效 mutation](replacement-mutation.log)：在隔离测试上下文让最终 identity 检查复用首次结果，真实替换测试不再抛错，预期断言失败；生产文件未改。证明该测试能发现移除最终文件身份核验的错误。
- 探索阶段误用 codesign `-R` literal 导致解析失败；按实际工具语法补充前导 `=` 后核验通过，生产代码从未把解析失败当成功。完整 literal 包含 Apple trust anchor、固定 identifier、Team 和 Developer ID leaf extension。
- Python 源码语法、文档/JSON/链接/diff、原 UI 完整性及任务清单范围已检查；[测试计数](test-results.json)、[范围记录](scope-check.json)、[文件摘要](artifacts.json)。本项不改变 Go 或 UI，没有重复上一组件已通过的 Go 构建/浏览器回归。

复现命令和真实来源见 [发布者核验说明](../../../integration/runtime-publisher-verification.md)。任务状态从权威 Markdown 重提取到 implementation_tasks.json，只有 WP-17 planned→in_progress，依赖/测试 ID/其他状态不变；没有将最终验收标为通过。原 WP-08 2026-10-03 inventory 继续保留历史值，新证明独立保存。

## 阻断与交接

报告只证明对应时点的本机文件与官方发布者，不是 Registry 或 Inspector 的可信导入合同，不授予账号/生成/额度/计费权限，也不证明 Gatekeeper notarization。OpenAI/X 官方私有登录、账号/workspace/地区/权益、生产 Forwarder 与完整控制证据；GLM 上游账号/计费/真实物理额度池与可信 Registry/Inspector；三路线真实模型、工程读写/测试/取消及实际 Wails 验收仍需完成。[完整真实接入矩阵](../../../integration/live-matrix.md)。三条路线本次真实模型调用为 0，不能把发布者核验报成三订阅接入或 Gate A 完成。

回退可停用这个显式工具，保留证据；没有数据库迁移、服务重启、任务重放、认证/launcher/原 UI 改动。继续真实准入时复用当前实际 pin，不更新到未核验 latest，不导入日常认证，也不伪造 pool/Inspection true。
