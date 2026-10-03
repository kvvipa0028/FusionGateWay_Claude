# WP-15-PRESETS-01 五角色版本化预设

状态：子项 done；WP-15 in_progress。

Store schema 4 保存不可变预设版本、CAS head 和同项目 task provenance。五角色先展开 group/角色覆盖、复制独立绑定，缺省保存 inherit。本次 task 的完整角色绑定覆盖明确预设版本，显式 inherit 解析项目/全局默认；历史 preset 与 task 不追随 latest。任务提交把 preset id/revision/hash、快照、预算、幂等与事件同事务保存。

内部 Handler 新增配置只读、预设列表/latest/history/PUT、任务原始来源读取；PUT `If-Match:"0"` 创建或 `"N"` 新增 N+1，原 payload/base 重试只读对应历史，不因 head 移动生成新版本。新保存引用已登记 model/route，保存草稿不赋予生成准入。未知项目/版本、越权、非法/重复字段、明确版本与预览失效均按固定错误拒绝。128 个命名预设达到上限为 429，已有名称可继续修订。

19 个新增 tests（stageplan 2、Store 7、API 10），另重跑旧 schema 1/2 迁移。初始 RED 为缺少类型与实现；第一轮 Store oracle 把格式错误 hash 当作 conflict，依据语法/身份分界增加 malformed ErrInvalid 与合法长度的错误 hash ErrConflict 两项，未降低断言。路由复核揭示名称 presets 抢占 quota 资源，先补测试，修复漏掉 quota import 后得到行为 RED（400），再改为按资源段判定，GREEN。

最终完整 Fusion tagged race 291 PASS、8 SKIP；CLI/GUI build 与全仓 vet 通过。固定 Claude Code 2.1.287 经既有 Controller/Adapter 在 schema 4 上完成一次合成 HTTP/Permit、结果验证、实际 wait/StopProof 和释放。Native 回归不覆盖新 preset HTTP 到真实账号的工程执行；本项实际上游模型/额度调用均为 0。

001–003 checksum 未改，004 独立升级/验证。原迁移回归的最终 schema 断言由 3 更新为 4，数据断言保留，依据本项新 schema 要求；旧任务没有被赋予不存在的预设来源。旧 binary 不能读取 4，回滚必须使用对应一致性数据库备份，不能只换 binary。

OpenAPI draft 补入预设、配置只读与预览应用字段，完整 WP-15 尚有接口/字段待补全。默认层公开写入、生产 bootstrap/listener/GUI、实际路线/账户验证、暂停/恢复与最终 Gate 继续实施，Jev off。
