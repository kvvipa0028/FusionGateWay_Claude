# WP-12 · PRIVATE-CREDENTIAL-01

本组件完成 Codex 私有官方 OAuth 缓存读取服务，父 WP-12、WP-17、Gate A 与整体目标仍未完成。基线 `64111b851d935ce985e2c733a9ac5ea245ff49a6`；四个新增 Go 文件由同一任务延续完成，没有覆盖其他工作区改动或新增 Agent。

新增 `FileCredentialScope`/`FileCredential.Load` 与私有 Credential。官方 0.160.0 的 AuthDotJson、TokenData、设备登录和实际保存函数已核对；保存模式明确为 chatgpt，account_id 可空。本服务要求缓存账号与独立登记明确匹配，缺失时拒绝，不从 JWT 推导账号或套餐。公共来源记录见 [public-source-info.json](public-source-info.json)，完整条件与生命周期见 [合同](../../../contracts/codex-private-credential.md)。没有公开导入接口或生产注册。

## 实际验证

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 缺少服务 API 的首次 RED | 类型/构造函数缺失，编译失败 | [初始 RED](initial-red.log) |
| 首次实现 GREEN | 合成作用域、schema、文件和目录漂移测试通过 | [初始 GREEN](initial-green.log) |
| 可执行缓存 RED | 初始注册和运行后 chmod 0700 两项未被拒绝，实际 exit 1 | [权限 RED](permissions-red.log) |
| 最终目标 race | 6 个主测试、75 个子场景 PASS，0 FAIL/0 SKIP，exit 0 | [目标日志](targeted-green.log) |
| 受影响四模块 race | 180 个主测试、583 个子场景 PASS，15 SKIP，0 FAIL，4 package exit 0 | [模块回归](dependent-race.log) |
| 有效 mutation | 移除内容摘要检查、workspace 绑定或严格缓存权限后，各一个对应子场景实际 FAIL/exit 1 | [摘要](mutation-digest.log)、[作用域](mutation-scope.log)、[权限](mutation-permissions.log) |
| Go1.26.3 CLI/GUI/full vet | 三项 exit 0 | [构建报告](build-results.json) |

权限 RED 已通过生产代码要求官方 `0600` 修复，拒绝执行位和特殊位，没有删除断言。Mutation 使用 Go overlay 和临时副本，未改生产源码；实际失败均来自对应行为断言，未将编译错误当有效 mutation。详细计数、原始 ignored JSONL 指纹和命令见 [test-results.json](test-results.json)。导出 Go 日志从实际 JSONL 的 Output 事件依序提取，规范化行尾空白；原始事件流仍保留在 ignored 开发目录。

15 项 SKIP 是显式 opt-in 的既有 fixture：14 个固定 Native 生命周期/HTTP/来源/交接测试，本轮未重复运行；1 个需要浏览器显式启动的合成宿主。没有将这些 SKIP 描述为通过。新增凭据测试没有 SKIP，使用临时 owned 私有目录与合成 token，不读取真实缓存、Keychain 或日常账号。

文件/目录遍历为实际 Unix FD 操作，测试覆盖 FIFO、symlink、hardlink、Git 标记、相同内容换 inode、父目录替换、宽松权限、缺失文件和内容变更。解析覆盖官方 pretty cache、可空替代字段、缺省 refresh 时间、显式绑定、模式混用、缺失/null 账号、字段类型、重复/别名、invalid UTF-8、超限 token 和文件、控制字符、尾随 JSON。String/GoString/JSON 脱敏和取消/目标漂移均有断言。

## 交付范围与剩余

本轮没有真实登录、模型或额度请求。独立 Codex HOME 的当前 metadata 检查仍为 cache absent；没有解析真实 JWT、写出凭据摘要、证明本人账号/地区/权益/计费/物理池或设置 Registry/Inspection true。本地 scope/文件读取成功不能替代这些证明。

下一步仍需生产订阅 Forwarder、可信账号及费用/额度核验、Controller/Factory 接线、可信续期、完整工具/写入/恢复和真实工程 Smoke。UI 及原 Magpie 资产未改；Store、路线准入、工作包及 T01–T60 最终状态未改，Jev off。范围、文档与敏感字节排除见 [scope-check.json](scope-check.json)，提交文件指纹见 [artifacts.json](artifacts.json)。
