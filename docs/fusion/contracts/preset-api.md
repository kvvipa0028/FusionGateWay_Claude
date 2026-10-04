# 执行预设与配置读取合同

这些接口属于 Management 保护的内部 Handler，已由私有 ControlHost 提供；[Native 配置界面](preset-ui.md)按受限允许清单接入预设读写。预设是项目范围内的版本化选择，不赋予路线或额度准入，也不改写日常 CLI 配置。配置读取仅返回可信 bootstrap 登记的 global/project 层、路线元数据及默认预算；HTTP 不得修改 registry、Admitted、账号、credential identity 或 Runtime。

| 方法/路径 | 行为 |
|---|---|
| GET `/control/v1/projects/{project_id}/configuration` | 返回 revision/configuration 与整数 ETag；仅 GET；保存的全局/项目层通过独立 defaults API 写入 |
| GET `/control/v1/projects/{project_id}/presets` | 只读各预设的最新版本，返回 `presets` 数组，最多 128 个名称 |
| GET `/control/v1/projects/{project_id}/presets/{preset_id}` | 浏览当前 head，返回完整 Preset 与 revision ETag |
| GET `/control/v1/projects/{project_id}/presets/{preset_id}/versions/{revision}` | 读取明确、不可变的历史版本；revision 必须为规范正整数 |
| PUT `/control/v1/projects/{project_id}/presets/{preset_id}` | body 仅 name/layer；If-Match `"0"` 创建，`"N"` 保存 N+1，首次提交 201，精确重读 200 |
| POST `/control/v1/tasks/preview` | 原请求可增加 `preset:{id,revision}`；必须明确正版本，不能提交 latest、0 或 hash 来替代服务端读取 |
| GET `/control/v1/tasks/{task_id}/preset` | 返回创建任务时固化的 id/revision/hash；未用预设则为 null，不追随 head |

预设 name 为非空、有效 UTF-8，最多 256 bytes，不能包含 control 字符。layer 使用现有 stage-plan binding 合同；三个 group 先展开，单角色覆盖 group，缺少角色明确保存为 inherit，历史记录始终有五角色，独立 testing/acceptance 不被合并。保存的新版本只引用当前注册的 route revision/model；未获生成准入的已登记路线可保存草稿选择，任务预览仍由 Compile 拒绝未准入路线。保存不会调用模型、查额度或创建任务。

应用预设时先分别展开预设与本次 task 层，再用本次选择替换完整角色绑定；本次显式 inherit 移除相应预设绑定并解析项目/全局默认。不能把一个模型的 effort、账号或 route 片段拼到另一个模型。预览生成冻结 Snapshot 与完整 hash，并额外记录明确 preset id/revision/hash；任务提交把此引用、Task、Snapshot、预算、幂等及 created 事件同事务提交。

修改同名预设不使已经生成的明确旧版本预览追随 latest；修改项目/global 配置仍按原 configuration revision 拒绝尚未提交的旧预览。已提交任务的 snapshot、预算和预设引用独立保留。运行控制器只读取冻结 Plan，不能把预设 head 当作执行目标。任务计划的后续显式修订沿用已有合同，创建时的 preset 引用只是来源，不声称修订后的每个角色仍来自该预设。

PUT 的基版本和规范 payload 构成幂等身份：对原 `"N"`/payload 的重试读取固定 N+1 历史，即使 head 已到更晚版本；不同 payload 不能覆盖它，返回 412 `preset_revision_conflict`。缺少条件为 428，weak/多值/负数/前导零/溢出或不可递增版本为 400。128 个名称的容量已满返回 429；既有名称仍可创建新历史版本。版本记录不删除，不用重试刷新 created_at。Location 对 opaque ID 做 path escaping。

无/错误管理凭据为 401，stage fgs_ 或 query credential 为 403，沿用入口合同。跨项目读/应用预设为 404；非法 body、未知字段、重复字段和不完整 binding 为 400。固定错误不回显输入或依赖异常。路由按资源段判定，项目名称 `presets` 不会捕获 `/quota`。

schema 4 新增 preset_revisions、preset_heads、task_preset_refs，版本记录有 UPDATE/DELETE 保护，任务引用有项目与版本/hash FK。004 checksum 独立，001–003 保持不变；schema 1/2/3 顺序升级后为 4。已验证 schema 3 的提交/启动幂等记录、已消耗预算、held 预留、历史事件与 controller policy 保留；没有为旧任务制造预设来源。向 schema 3 或更早 binary 回滚必须恢复相应一致性数据库备份，不能只换 binary；旧 binary 会拒绝 4，未来 5 与 checksum 异常也拒绝打开。

新增 19 个 stageplan/Store/API 测试，另重跑两项旧版本迁移；覆盖不可变历史、重启保留、并发 PUT、head 写入失败与任务引用失败的事务回滚、跨项目/非法 hash、容量、明确应用版本、五角色与全绑定覆盖、失效路线不准入、预览与已提交任务的版本边界、实际 loopback HTTP 和路由/Location。固定 Claude Code 2.1.287 在 schema 4 上通过既有 Controller/Adapter 的合成上游一次调用、实际 wait/StopProof 与释放回归；它未验证新 preset HTTP 到真实账号的工程执行。本项真实模型/额度调用均为 0，Jev off。

当前 schema 已由后续 [默认层 API](default-layer-api.md) 扩展到 5；上述 schema 4 验证为 PRESETS-01 的历史证据，当前 binary 拒绝未来 schema 6。预设的固定版本应用与任务 FK 合同保持不变。
