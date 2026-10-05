# WP-22 · REWORK-LOOP-01

BASE `ad9ca82df3fc3904b2a79588f210f4994e624197`。已完成一次受控返工的实际执行链，WP-22 整体仍为 in_progress。

## 行为

当前 owned、真实硬检查通过的 changes_required review，在已批准原设计/冻结计划及 MaxReworks=1 下生成不可变返工 receipt；执行 design → implementation → testing → review → implementation → testing → review → acceptance。原公开五角色定义不变，补救使用原冻结模型/账号/路线/effort，独立 Native session 和 exact parent；implementation2 消费原 R1 findings，实际创建修复文件，testing2 写新测试文件并在新树运行真实固定命令。历史旧树 testing 对当前新树显示 superseded。模型 accepted 仍是 advisory_only；明确人工接受后才 completed。

调用和返工预算按 task 累计，角色 intent 最多两次。二次审查问题、复测失败、预算耗尽、父包漂移或撤销都会停止，保留记录。Controller 的可信 AfterRelease 只在原实际 Wait/StopProof/Release 后调用；继续使用原 lifetime、容量与受跟踪 WaitGroup，容量1及 Close 取消/等待已有测试，重启不自动重放。

schema12 增加012不可变表/触发器/校验，001–011原样保留。事务失败回滚至11；旧真实接受记录保留，未给旧任务虚构返工。当前 Open 断言调整为12、未来拒绝为13，历史 fixture 的原版本断言保持；这不是放宽测试。原 review_changes Native 场景明确登记 MaxReworks=0 继续验证不自动返工。

## 验证

- 全 Fusion race：722 个顶层、1303 个子测试 PASS；33 个顶层和11个子测试 SKIP，19 packages，0 FAIL。SKIP 仍不是通过。
- 固定 Claude Code 2.1.287：10 个顶层、64 个子测试 PASS，0 FAIL/SKIP；其中32个完整阶段场景，新增7个返工场景。上游、账号准入和额度为合成输入，实际 Native 进程、Edit、代码副本和直接测试执行为真实执行。
- Store/Controller 定向：11顶层/8子测试 PASS；额外证据标准与管理鉴权边界2顶层/3子测试 PASS。owned/JSON区别、CAS、权限撤销、事件失败、并发只开一轮、重启保持、不可变历史与迁移回滚均验证。
- 有效变异：移除当前 hard passed gate 后，改变标准的负例真实 FAIL；源文件字节精确恢复，最终完整回归 PASS。
- Go1.26.3 CLI、GUI 构建及全量 vet exit0。

7个新 Native 场景覆盖完整补救、二次问题、复测失败、预算耗尽、先撤权、父包漂移和排队继续后撤权。rework_cancel 指生成返工记录后撤销管理权限；Close 取消生命周期由 Controller 单元验证，不冒充真实 Native UI 取消。

初始失败原样归档：native-red 是原实现未返工的行为 RED；store-api-red 只是缺 API 编译失败。native-green/native-debug 暴露 callback 错误变量 shadow，修复后实际8阶段成功；store-regression 暴露旧 schema10 fixture读取新表，修复仅让真实 pre12 原序列读者保持旧语义，生产 Open 必须升级12。初始失败不能描述为通过。

## 交付与限制

[策略合同](../../../contracts/rework-policy.md)、[测试统计与原始日志hash](test-results.json)、[构建结果](build-results.json)、[边界记录](boundaries.json)、[文件清单](artifacts.json)。本次无 UI/CSS 修改，Magpie 原3主资源及6个CSS与上游 pin 完全一致；继续沿用原布局/组件/变量/交互，不引入新界面框架。未重跑浏览器或宣称 Wails 像素验证。

真实供应商 Gate A、项目强制独立性、子进程测试工具链、原主界面完整整合、实际 Native 点击和最终60类验收仍待完成。Jev off，无真实供应商模型请求。回退必须先停止/核对进程，再使用匹配旧 binary 的完整私有数据库与产物备份，不删除表或改 user_version。整体目标继续执行。
