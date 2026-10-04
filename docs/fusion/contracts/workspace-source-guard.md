# 来源记录接入受管执行

`workspace.Snapshot.Guard()` 从成功 Copy 的私有来源记录产生 `SourceGuard`。Guard 以值传给可信 `runtime.Spec.Source`，不接受路径/清单/回调构造，不序列化成 HTTP 或持久恢复授权；String/GoString 脱敏。公开 Snapshot.Path/Files、零值或外部重建的 Snapshot 均不能伪造来源。除原来源记录外，Guard 私有绑定实际副本的绝对路径、inode/device、owner/group 和权限。

`ValidFor(cwd)` 要求同一副本目录、当前私有且 Git 外、无 symlink，源树仍符合原记录。源检查前后都重查副本目录；副本中正常的生成文件修改不当作原来源漂移，根目录身份/权限替换仍拒绝。它保留 [来源复核](workspace-source-seal.md) 的有界观察与非原子边界，不能授予项目或外发权限。

可信 Controller Config 的 `RequireSource=true` 要求每次新执行/明确恢复都有实际 Guard；启动前、Probe 后、Inspection 前后和已提交 intent 的 CheckPrepared 后重查。缺失/错误 Cwd/来源变化在 intent 前拒绝，慢服务导致变化同样拒绝。intent 已提交之后才发现变化时沿既有 unlaunched 合同：Store interrupted/needs_review，Completion execution_uncertain，保留 reservation，不用来源失效冒充 kernel stop proof 或返还调用预算。原幂等 key 只读旧 receipt，不重做来源复制或重新执行。

Grok/GLM Adapter 接受并保留同一 Guard。启动和可信 Current 回调前后检查，Current 由 HTTP Gate、工具/历史关联、Native 事件及结果校验消费。Grok 的准确 checkpoint 恢复仍走相同 Spec/Current 检查。传递给 Supervisor 的 Spec 也保留 Guard：启动/intent/spawn 前检查、每两秒 heartbeat 观察，以及成功结果 validator 前后检查。闲置进程在检测失效后按已有取消→TERM/KILL→实际 wait/reap 链停止；这不是即时文件变更通知，也不保证变更被观察前没有操作。

Controller job 保留原 Guard/Cwd，归档前后复核。当前来源改变不能通过重新 Resolve 或公开 manifest 替换旧 job 的来源；私有 producer 已发布后才发生变化时拒绝披露 ref，不声称文件系统自动回滚。

兼容性：RequireSource 默认 false，Spec.Source 的零值沿用历史诊断语义，不能据此宣称来源通过验证。当前本机服务仍为 draft-control，Controller/真实路线尚未由产品 bootstrap 注册。实际项目执行 bootstrap 必须设置 RequireSource=true，并独立绑定 Loaded.Current、准确 project ID/read/write、数据许可、Route/quota/pool/Runtime 的真实准入；本项没有把未准入登记变成执行权。持久重新启动必须重新验证来源，不能从 SourceGuard 的序列化或旧成功 receipt 自动恢复权限。

检测失效不会撤销已发生的 HTTP 外发或副本文件修改；它拒绝继续接受结果，并保留相应预算、记录及核对要求。每次整个来源复核最多读取100MiB，执行时有实际开销。当前实际平台仅 macOS/arm64，Go1.26.3，Jev off。

证据见 [SOURCE-GUARD-01](../work-items/WP-15/SOURCE-GUARD-01/summary.md)。固定 Grok/Claude Native 与合成上游验证执行中的来源漂移；没有真实账号/模型/额度/计费或完整产品 HTTP 端到端结论。

后续 [execution-host.md](execution-host.md) 增加 `SourceGuard.BoundToSource(path)`，仅比较私有来源路径，不披露路径、不替代 ValidFor。可信宿主使用它拒绝另一项目的有效副本，并强制 RequireSource、当前登记 read/write/route 和退出生命周期。实际 Native经此宿主鉴权 HTTP 的合成验证单独归档，不改变上面旧项的证据范围或真实路线状态。
