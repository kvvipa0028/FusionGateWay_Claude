# GLM-FACTORY-01 交付记录

本组件完成 GLM 单阶段生产后端组装：NewGLMRuntimeFactory 复用真实 Adapter/FileCredential/固定 CNTransport/私有 Copy/SourceGuard 与同一 Store/Manager/Scheduler。它要求独立 Registry 与当前 Inspector，不能从用户 JSON、key 存在或额度百分比推断准入；公开构造器没有上游/argv override。所有 UI、CSS、原 API、policy、Runtime 与 schema9 均未改，Jev off。

实际固定 Claude Code 2.1.287 经产品 HTTP preview→submit→start、Controller、SQLite 和新 Factory 完成六个场景：成功 1 次模型 HTTP、真实 Edit 创建副本文件 2 次、取消/Registry 撤销/key 轮换/源声明撤销各 1 次。每个调用数与持久预算一致，实际进程停止证明与资源释放通过；原项目内容保持、没有 created.txt。外部模型与准入均为合成 fixtures，真实私有 key 没有进入这些测试；这不是实际供应商账号或计费通过。

有效行为失败及修复：默认 effort 冻结 high 时初版误用配置 Value，编译器拒绝；现在保留 frozen value，默认请求仍为空。Registry 撤销后初版只在前后检查身份，不能取消等待中的请求；新增每个 owned run 的当前身份 watcher，取消真实运行，Handle 终态 Wait 后才退役 watcher，Controller 仍拥有 StopProof/Release。执行根在构造后被替换曾仍可登记，补上 inode/权限再检查。多角色新副本会丢失实施修改，明确拒绝，等待必须实现的 Handoff。初始缺构造器编译失败与这些实际行为 RED 均保留；错误的 api.PreviewReply/Ready 测试假设已按真实接口修正，不算产品行为 RED。

| 验证 | 最终结果 | 证据 |
| --- | --- | --- |
| Factory targeted + 固定 Native | 7 顶层/31 子测试 PASS，0 FAIL/0 SKIP，含六个实际进程场景 | [最终日志](factory-native-final.log) |
| 全 Fusion tagged race | 646 顶层/1176 子测试 PASS，32 顶层/10 子测试 SKIP，17 包，0 FAIL | [最终回归](fusion-regression-final.log) |
| Go1.26.3 CLI/GUI/full vet | 全部 exit0 | [构建结果](build-results.json) |
| 初始全回归中的 clang segmentation fault | 旧 runtime fixture 编译失败；源码/测试未改，单独复核与最终全回归通过 | [原失败](fusion-regression-initial-failure.log)、[单独复核](compiler-fixture-recheck.log) |
| 原 Magpie UI 与 CSS | 本组件零修改，原三主资源与锁定上游一致 | [文件及范围核验](artifacts.json) |

[结果](test-results.json)保存原始日志 hash；归档只移除行尾空白，原始日志保留在 ignored scratch。合成源码和验证产物按[文件清单](artifacts.json)及本组件 owning commit 校验；跳过的 Native/其它环境测试不记作通过。

合同、复现与回退见[GLM Factory](../../../contracts/glm-execution-factory.md)。本机 UI 工具本轮确认 Mac locked，未做实际窗口点击；Orca 旧 runtime metadata 不可用，未启动另一应用。开头误认为 CNTransport 缺失，发现其已在18a57a3完成后撤回重复测试，原 Transport/Native 测试逐字恢复，未重复实现或覆盖旧证据。

父 WP-14/WP-15、整体目标继续 in_progress；完整真实 Registry/Inspector、账号/publisher/地区/计费/物理池/当前额度证据仍缺，产品 CLI execution off。多阶段 Handoff、工程测试审查闭环、原主界面整合、实际 Native UI 与最终 T01–T60 待完成；不合入 main、不发布。授权范围内独立 commit/push origin fusion/development。
