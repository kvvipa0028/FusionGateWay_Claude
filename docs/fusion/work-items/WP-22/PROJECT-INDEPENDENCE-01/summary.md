# WP-22 · PROJECT-INDEPENDENCE-01

BASE `05f79351d8af375e49c3aa68d7a4729a12999174`。项目角色模型独立性已落实到真实派单边界；WP-22 整体仍为 in_progress。

## 行为与兼容

默认允许同模型多角色，实际 Native 阶段仍使用独立 session/证据。私有项目登记可声明有界、canonical 的角色对 independence；可信配置、预览、任务 Snapshot 和 hash 保留规则。具体同模型换账号/路线/effort 不能绕过，任务 body、Layer/预设或修订不能削弱规则。可行的原批准 auto 候选不变；选择与对方 frozen binding 或任何历史 intent 冲突时明确拒绝，不自行增/换模型。

Controller 在 Runtime/凭据准备前，Scheduler 在 Inspection 前检查，原子 intent 再次检查。拒绝不写 task/run/事件/reservation/key，不消耗调用或返工预算。restart 和幂等回读保持；没有独立性规则时原序列化/hash 不变。Snapshot/ProjectConfiguration 增加可选字段，输入 DTO 继续拒绝覆盖。数据库及001–012 SQL不变，schema12；旧 binary 无法核验带新字段的 plan，回退须使用匹配旧 binary 的停机完整备份，不能忽略规则强行运行。

## 验证

- 全 Fusion race：729 顶层、1305 子测试 PASS；33 顶层、11 子测试 SKIP，19 packages，0 FAIL。缺环境 SKIP 不计通过。
- 固定 Claude Code2.1.287：10 顶层、65 子测试 PASS，0 FAIL/SKIP，190.540s。32个原阶段场景仍通过，新增项目硬约束预览冲突场景在模型启动前阻断；默认同模型的真实5阶段/8阶段代码与独立session验证保持。供应商/准入仍为合成输入。
- 定向8顶层/2子测试 PASS、5 packages：canonical/未知/自身/反向重复、复制/hash、冻结、合法auto选择、不同账号同模型冲突、历史/restart、revision不能删规则、Runtime/Inspection前否决、任务覆盖拒绝与实际 Handler样本。
- 真实行为RED：原 Store 对同模型另一账号允许启动；修复后拒绝并保留完整现场。初始 api-red 只是缺新API编译失败，不能称行为证据。
- 有效变异：禁用 historical opposite-role ResolvedModel 比较，原同模型负例真实FAIL；字节精确恢复，完整回归通过。
- CLI/GUI构建、全量vet：Go1.26.3 exit0。Graph14655 nodes/132287 edges已刷新。
- [OpenAPI](openapi-final.json)：126个当前实际响应样本覆盖41 paths/49 operations；新增8类独立性输入负例通过。首次仅API样本缺人工验收GET/POST，补采当前真实Native响应后覆盖完整，未放宽断言。补采Go子模式 success$ 同时选中 rework_success 和 success，1顶层/2子测试均通过，明确记录而不冒称只跑一项。

## 文件与边界

[策略](../../../contracts/project-independence.md)、[测试统计与raw hash](test-results.json)、[当前构建](build-results.json)、[边界](boundaries.json)、[文件hash](artifacts.json)、[当前合并响应](combined-handler-samples.json)。日志公开归档只去行尾空白，raw保留于私有scratch。

Magpie原3主资源与6个CSS与pin字节一致，所有GUI零修改；本次没有重跑浏览器或声称Wails像素验收。真实key未进入源码/日志/提交，Jev off、真实供应商调用0，主仓库两项DSStore保留。

不同供应商真实端到端、完整原主导航、子进程工具链、真实Gate A、实际Native点击及最终60类验收仍须完成，不将组件通过等同整个工作包或整项目完成。
