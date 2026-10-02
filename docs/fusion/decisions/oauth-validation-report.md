# OAuth 方案 A 验证报告

使用 Go 1.26.3 darwin/arm64，在临时 HOME/XDG 和环境白名单下执行。没有读取真实登录文件、运行真实 Google 授权或申请保护例外。

- 完整 `nogui` 测试：3912 个测试/子测试通过，64 个跳过，0 个失败。跳过项不作为通过证据。
- OAuth 与原生客户端身份边界 race 检查：28 个测试/子测试通过，0 个失败。
- `go vet -tags nogui ./...`、CLI 编译和 GUI 编译：exit 0。GUI 链接仍有上游基线同类 macOS deployment target 警告；没有验证 GUI 交互。
- RED 记录包含缺配置、身份及配置变更、敏感错误、恢复旧配置、显式重导入、ID token 冲突，以及缓存隔离 mutation；对应修复/恢复后 GREEN。
- 首次 targeted regression 的 TestLoopbackSignInsTakePastedAddress 缺少新私有配置，已补假客户端，保留原断言；随后完整测试通过。

| 验收 | 当前结果 | 证据 |
|---|---|---|
| OAUTH-A01/A02 | pass | 缺失/空/部分/格式错误/开放权限及有效缓存 token 仍拒绝，假上游 0 请求 |
| OAUTH-A03 | pass | 两个假 client ID/secret 分别用于 refresh 与 code exchange；同 refresh token 不共享缓存 |
| OAUTH-A04 | pass | 原客户端边界断言保留；新增冲突 aud/azp 与 malformed id_token 拒绝；无 ID 的旧格式可见但拒绝执行 |
| OAUTH-A05 | pass | 修改/删除配置持续阻断至重启；旧账号版本不匹配拒绝，显式导入重新核验 |
| OAUTH-A06 | pass | 缺配置的登录/交换/导入和变更后的回调不发请求；错误正文脱敏 |
| OAUTH-A07/A09 | pass | 完整 nogui suite、OAuth race、vet、CLI/GUI 编译；对应源码 hash 见 JSON |
| OAUTH-A08 | pending | 尚需清理并扫描完整拟推送历史 |

命令、退出码、源码及日志 SHA256、完整 skip 清单见 [机器可读报告](oauth-validation-report.json)。原始日志暂存本机 `.fusion-dev/oauth-validation/`；历史清理和实际推送完成后补充报告。
