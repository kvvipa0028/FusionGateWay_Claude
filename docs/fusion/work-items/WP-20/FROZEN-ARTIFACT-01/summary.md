# FROZEN-ARTIFACT-01 交付记录

BASE b3b14922348b5b20bc5b53dabab2b6f76322ff75。本组件完成实际代码产物冻结、验证与独立副本交接；父 WP-20 继续 in_progress，完整目标 active。

`workspace.Freeze` 使用原私有 provenance 选择真实 producer，忽略可改写的公开 Path/Files；原始 Git base、原来源树、本次实际代码树和变化摘要分别绑定。新增/删除、二进制内容和脚本 owner execute 位保留，Copy 不再丢掉执行语义。`FrozenArtifact.Copy`/`Bundle.Copy` 校验内容、manifest/header、完整 Task/Plan/Run/role/target binding 和祖先来源链；不能由 JSON/hash 字符串冒充授权。原项目和冻结祖先漂移拒绝，下游每次收到独立副本；后来修改 producer 不改变已冻结产物。不复用认证或 Native session，不提交用户项目。

GLM Factory 对每次 launch 使用独立 backend，在真实 Handle 终态、原 Supervisor StopProof、Store run/target 和当前项目对应后发布包，再调用原 Release。失败/取消不发布成功包；冻结或发布失败保留 reservation，不提前声称 StopVerified/Released。包的模型输出只保留 hash；tests_executed=false、evidence=unverified，缺少的设计决策、工程测试、独立审查与验收明确 pending。

实际固定 Claude Code 2.1.287 经过产品 HTTP→Controller→SQLite→GLM Adapter，完成七个真实进程场景：成功 1 个合成模型请求、真实 Edit 写副本 2 个、交接目录冲突 1 个、取消/Registry 撤销/key 轮换/source 撤销各 1 个。实现文件进入冻结 code，输出和真实停止 report hash 与包对应；取消场景没有成功包。目录冲突时真实进程已停止，Controller 不声称释放，Store reservation 仍 held，已有 foreign 文件保持。总计 8 个合成请求，与 SQLite 调用预算相同，真实供应商 key/模型/额度调用均为 0。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 全 Fusion tagged race | 655 顶层/1198 子测试 PASS，32 顶层/11 子测试 SKIP，18 包，0 FAIL | [最终回归](frozen-fusion-final.log) |
| Factory + 实际固定 Native | 7 顶层/32 子测试 PASS，0 FAIL/0 SKIP，含七个进程场景 | [Native 日志](handoff-targeted-final.log) |
| CLI/GUI/隔离 full vet | Go1.26.3 全部 exit0 | [构建结果](build-results.json)、[最终 GUI-tag vet](frozen-vet-final.log) |
| 实际 API 序列化格式 | producer API 输出 fixture 通过 schema，9 个结构反例拒绝 | [fixture](serialized-fixture.json)、[schema 结果](schema-results.json) |
| 原 Magpie UI/CSS | 零修改；原三个主资源与上游 1a50db1 一致 | [范围与文件 hash](artifacts.json) |

有效 RED 保留：缺少 Freeze/Bundle API；脚本 execute 位丢失导致错误变化条目；原 Factory 实际成功后没有 handoff 文件。APFS 外层目录新增 header 改变 nlink，初版误判篡改；只对允许增加元数据的 outer bundle 目录使用 inode/mode/owner 比较，固定代码树和文件 nlink 校验保持。见[脚本/变化失败](artifact-initial.log)、[header 失败](handoff-library-green.log)、[真实 producer 失败](handoff-native-red.log)。

首次扩充全回归的非法 UTF-8 文件名夹具在 APFS 的 open 阶段返回 EILSEQ，尚未到达 Freeze；runner 又无法解码该原始日志。现在只对这个明确的文件系统能力缺失 skip，保留生产 UTF-8 拒绝逻辑和其余断言。[失败记录](frozen-fusion-apfs-fixture-failure.log)将非法字节转为可见转义，不修改失败原因；原始 ignored bytes 与归档 hash 见[test-results](test-results.json)。该夹具在支持任意字节文件名的文件系统仍需执行，不能将 skip 算作通过。其余归档只移除行尾空白。GUI/vet 的原 CGImageAlphaInfo 枚举转换 warning 来自未改的上游文件，退出码为 0。

复现、hash 定义、schema/权限/故障与回退见[合同](../../../contracts/stage-handoff.md)。可在仓库根重新生成合成序列化样本（不调用模型）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/handoff-schema.log run -mod=readonly \
  docs/fusion/work-items/WP-20/FROZEN-ARTIFACT-01/capture-schema.go \
  "$PWD/docs/fusion/work-items/WP-20/FROZEN-ARTIFACT-01/serialized-fixture.json"
python3 docs/fusion/work-items/WP-20/FROZEN-ARTIFACT-01/check-schema.py
```

本包没有 durable artifact index、受控重启恢复或下一阶段 resolver，因此 GLM 多角色限制继续保留。原 Runtime/API/policy/Store/schema9、所有 UI 与 CSS 不改。真实供应商准入、完整工程测试/审查/验收与有限返工、原主界面完整整合、实际 Native UI 和 T01–T60 仍未完成；Jev off。本组件独立 commit/push origin fusion/development，不合 main、不发布。
