# Grok 受管准确恢复合同

对应 [WP-13-RESUME-01](../work-items/WP-13/RESUME-01/summary.md)。可信内部 ResumeCheckpoint 接受 distinct prepared run、受限 Spec、Archives/ref，复用 [只读 Adapter](grok-adapter.md) 的准入、protocol/HTTP/Read、实际 wait/StopProof/Release。没有产品 HTTP 恢复入口或 Worker 注册；通用 Resume 缺少 prompt/cwd/ref，仍明确 unsupported。Probe 不提升产品恢复或真实路线证明。

先复制 prompt，再核对 Scheduler.CheckPrepared、权威 Store/预算/readonly reservation、固定 executable hash、显式 none effort，使用 [恢复准备层](grok-restore.md) 的准确 seal、成功已释放旧 run、相同 frozen Target/项目/cwd/Source。冻结新 Root device/inode/UID，所有 Current 调用前后复核 canonical/0700/owner 和目录身份，防止服务 callback 替换 Root；每 HTTP/event 继续检查新 run/current、Archives root/key、原批准读取文件。

只生成新的 ModelAudience grant 与独占双栈端口。新 grant 的 raw/JSON 转义反射在导入 Read 中拒绝；旧 grant/config/global logs/agent_id/daily HOME/Keychain 不复制。GrokResumeChannel 的 typed seed 不来自 Spec/HTTP，没有任意 callback；构造时复制固定 14 文件、核对 UUID/cwd/resolved model，2MiB/file、16MiB total、UTF-8/NUL/空 lock 边界。准确 archive/seal/历史授权由可信消费者负责，seed 构造器本身不签发授权。

Supervisor 先创建全新私有 HOME/XDG/GROK_HOME 和新 config，seed 只在新的 sessions tree 中以 0700/0600/O_EXCL 写入、逐文件 fsync、atomic rename/fsync 发布。已存在树拒绝合并。Seed 单次消费；安装完成或失败后清除内存副本，不能在另一 Root 重放，避免历史字节长期附着于 Handle。整个执行继续 default-deny、read-only Source、仅 owned loopback；不增加 fork、Mach/Keychain、额外网络或 write 权限。

argv 只传明确 `--resume UUID` 和新的私有 prompt-file；不用无参 resume、continue、title、fork、restore-code 或重新生成相同 UUID。新输出仍按 frozen model/原 UUID/full protocol/真实 exit0/EOF/CallGate.Healthy 核验。旧批准 Read 每 main HTTP 完整匹配，新的 Read 仍独立批准和 trace/HTTP 关联；历史项不占新 tool-turn，合计 64 项/512KiB 限制和逐调用 Permit 不变。已有 title 的恢复实测只发一个 main HTTP；新增 Read 后两次 main HTTP，均记账，不按 Native end.modelUsage 推算花费。

启动前 Archives 用独立 HMAC key 密封 prepared mapping：新 run/generation/Target、旧 run/generation、ref/UUID、project/cwd、新 Root 身份和 prompt SHA256，不保存新 prompt 或 grant。0600/O_EXCL/fsync，link 发布避免覆盖其它 opener 的已有映射；准确重试可以读取相同 mapping，更改 prompt/Root 拒绝。RestoreInfo 只读私有元数据，调用方仍须 task/project 鉴权；mapping 不是启动或停止证明，也不是自动重放许可。

同一 Adapter 的 observation/owned Supervisor 防止同 run 再启动，重复低层调用拒绝，产品 receipt/API 幂等响应仍待接入。取消撤销实际 inflight HTTP 并等待 Native，只有本 Supervisor 的 StopProof 经本 Scheduler 验证才释放。失败/取消不返回成功 text、不归档成功 checkpoint；未知/interrupted 保留容量和 needs_review，持有 mapping 不改变这些状态。

实际测试包括完整 Store/Archives/Manager/Adapter 重开后从已成功且已释放旧 run 重新 Prepare 并恢复准确 UUID。这不证明应用启动、产品 auth/Worker 注册或 unknown 运行的恢复；后者继续禁止自动接管。所有 Inspector/Quota/upstream 仍合成，真实账号/池/计费/Source整体稳定性/当前项目权限由后续产品服务独立落实。
