# Grok 可信会话归档合同

对应 [WP-13-ARCHIVE-01](../work-items/WP-13/ARCHIVE-01/summary.md)。`Adapter.Checkpoint` 是可信内部归档生产端，`NewArchives` / `Archives.Info` 提供私有持久核验；没有 HTTP 接口；可信内部 Native Resume 已由 [RESUME-01](grok-managed-resume.md) 接通，真实路线准入仍待完成。

只能归档同一 Adapter 持有的成功 Handle。必须完成实际 wait、同一 Supervisor 的 VerifyStop、同一 Adapter 的可信 Release；Store 中 run/generation/task/role/attempt/plan revision/frozen Target/Native UUID 必须一致，reservation 已释放，task 未取消。终态 Store 清空 lease owner，因此归档分别保存原 owner 和已完成终态，不能要求终态继续持有 lease，也不能仅凭 Native 文件推断停止。失败、取消、未知 Handle、其它 Adapter 或 generation 均拒绝。

Archives 根目录必须是既有 canonical、本人所有、0700、Git 外私有目录，且与 Native Root、Source cwd 双向不包含。首次用 O_EXCL 建立随机 32byte `archive.key`，0600、regular/single-link，文件和父目录 fsync；重启读取同一 key。每次操作核对根目录/key 身份、权限及 key 内容。key 与 Native 可写目录分离；不写入 Git、公开产物或日志。本项不提供 key rotation、删除/清理或通用备份 API。

固定 Grok `1.0.48/b94d5072c95f`，SHA256 `1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05`。只接受明确 UUID 对应目录的已观察 14 文件布局：chat_history、events、prompt_context、rewind_points、signals、summary、system_prompt、tool_definitions、updates、usage 及四个空 lock 文件。核对原 Native Root 与 cwd 目录身份、summary 的 UUID/cwd/resolved model。缺文件、额外文件、symlink、hardlink、special file、其它用户可写、错误 UTF-8/NUL/JSON、重复字段、尾随内容或原 grant 的 raw/JSON 转义反射均拒绝。config、agent_id、全局日志、SQLite、prompt_history 不进入归档。

单文件最多 2MiB，Native 文件合计最多 16MiB，密封 manifest 最多 2MiB；Native 事件帧仍使用原 1MiB 上限。临时目录 0700、输出文件 0600，逐文件 fsync，复制后再次核对源文件摘要/布局和目录身份，最后 atomic rename/fsync 发布。取消或失败只清理本次临时目录，不发布部分归档，不覆盖既有归档；同一准确 checkpoint 可返回同一 reference。

Read 快照只能来自原 ReadTools 的完整批准记录，要求工具已完成且后续 HTTP 关联已完成、没有 pending。按 call ID 保存原名称/arguments/path、原始输出及文件身份/权限/mtime，最多 64 项、合计 512KiB；仅进入私有密封 payload。归档和 Info 继续核对原 Source 目录身份及这些实际读取文件。此检查不证明整个 Source 不变，也不替代当前项目权限；历史 transcript 自报工具信息不能创建授权。

HMAC-SHA256 密封 payload 包含原 run/owner/project/Target/pin/UUID/cwd/目录身份、停止证明、14 文件大小/SHA256 和批准 Read。`CheckpointRef` 仅有 opaque ID/digest；重算公开 digest 不能伪造 MAC。Info 核验 seal、文件和 Source 后返回深拷贝元数据，不返回原始 Read/key。Info 是内部服务：调用方仍须执行 task/project 权限校验，reference 本身不授予恢复权限。

[RESUME-01](grok-managed-resume.md) 已实现新 prepared run/generation、准确冻结 Target、新私有 Root 的可信 seed、新 config/grant/端口/预算，以及历史 Read 导入和逐 HTTP 核验；实际受管 `--resume UUID` 的协议、取消、wait/StopProof/Release 已通过合成上游验证。私有 HMAC mapping 记录准备关系，不是启动或停止证明。产品 API/幂等响应/Worker 注册、当前项目权限服务及整个 Source 的稳定性仍待完成。没有准确 Spec/ref 的通用 Adapter/Session Resume 继续 unsupported；unknown/cancelled run 不因持有归档而自动恢复。
