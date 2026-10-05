# DURABLE-ARTIFACT-01 交付记录

BASE b98610e79c0f7b0635a29f560844b88243a0603a。本组件完成真实阶段产物的私有持久索引与受控重启恢复；WP-20 继续 in_progress，完整目标 active。

Store schema10 增加不可变 receipt，核对真实 Task/Plan/Run/target、成功状态、confirmed intent、预留、current fence 和父阶段输入。登记与 event 原子写入，重试幂等；消费者查询必须匹配实际 released StopProof，held receipt 仍 pending。GLM producer 在真实进程停止与产物发布后登记，再调用原 Release，不把磁盘包或索引当作派单授权。

witness/Reference v1 持久化来源、根目录、完整祖先、代码/manifest/header 的身份与内容摘要。恢复只接受私有 Store receipt 和独立登记 source/root，先检查真实 metadata，再验证字节、canonical 编码、树变化与全部祖先；更换目录/inode、改 header/code/source 或越出登记范围均拒绝。Restore 不复用 Native session、不授予模型权限；Copy 从实际修改代码创建独立副本。

真实固定 Claude Code 2.1.287 经产品 HTTP→Controller→SQLite→GLM Adapter。成功和实际 Edit 场景现在关闭 producer 宿主，重开原数据库，从 receipt 恢复并复制实际代码；预算同样由重开 Store 查询。七个真实进程场景合计八个合成模型请求，取消/撤销无成功包，目录冲突保留预留和已有 foreign 文件。真实 key/模型/额度调用为 0，Jev off。

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 全 Fusion tagged race | 664 顶层/1223 子测试 PASS；32 顶层/11 子测试 SKIP；18 包，0 FAIL | [最终回归](durable-fusion-final.log) |
| Factory + 固定真实 Native | 7 顶层/32 子测试 PASS，0 SKIP/FAIL；含数据库关闭重开和真实 Edit 传递 | [Native 最终](durable-native-final.log) |
| 持久索引/父包/迁移 | 7 顶层/15 子测试 PASS，0 SKIP/FAIL；并发幂等、事务失败、9→10 保留/回滚/checksum、错误父链/输入拒绝 | [targeted](artifact-parent-final.log) |
| CLI/GUI/full vet | Go1.26.3 全部 exit0 | [构建结果](build-results.json) |
| 原 Magpie UI 与样式 | 所有 GUI 零改动，三个主资源与上游固定版本一致 | [范围/hash](artifacts.json) |

有效 RED：[缺少索引 API](artifact-index-red.log)、[缺少恢复 API](reference-red.log)、[实际 Native 产物未索引](durable-native-red.log)。[首次恢复失败](reference-initial.log)发现 Go 的 os.SameFile 只接受真实 FileInfo，现以持久指纹先检验，再保留实际 Lstat 信息；不削弱来源检查。[Native 初版失败](durable-native-initial.log)来自测试在完成真实关闭重开后仍向旧 Store 查预算，已改用重开 Store，预算断言保留。[新父包夹具编译错误](artifact-parent-initial.log)是不存在的常量，按原 finite kind 修正，没有变更 workflow 允许类型。失败记录未覆盖为成功。

归档只移除行尾空白，原始/归档 hash 与结果见 [test-results](test-results.json)。全回归的 opt-in Native 和 APFS 非法 UTF-8 文件名 EILSEQ 等 skip 保持明确记录，不算通过；真实 Native 覆盖另由显式固定 pin 执行。GUI 的未改上游 CGImageAlphaInfo warning 退出码为 0。

[合同与准确复现/故障/回退](../../../contracts/durable-stage-artifact.md)。这次迁移只增加 010，001–009 不改；旧迁移测试的最终 schema 断言从 9 更新为 10，future schema 拒绝夹具改为 11，历史保留与 rollback/checksum 断言保留。既有 Runtime/API/policy、所有 GUI/CSS、原 handoff Document schema 不改。

产品下一阶段 resolver、实际 testing/review/acceptance 权限与硬证据、有限返工仍未完成，GLM 单角色限制继续保留。真实供应商 Gate A、主界面完整整合、实际 Native UI 与最终 T01–T60 未完成。没有合 main 或发布；本组件独立提交并按已有授权推送 origin fusion/development。
