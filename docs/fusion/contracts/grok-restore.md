# Grok 恢复准备与历史 Read 合同

对应 [WP-13-RESTORE-01](../work-items/WP-13/RESTORE-01/summary.md)。本项提供包内可信归档消费者和新的 per-run ReadTools；尚未接通 Native seed/launch 或持久恢复 receipt，不新增 HTTP API，不开放 Adapter/Session Resume capability。

`Archives.prepareRestore` 仅接受完整 [Checkpoint seal](grok-checkpoint.md)，调用真实 Scheduler.CheckPrepared 重核当前准入、quota、reservation 和剩余预算。先深拷贝 caller run，再核对 Store 权威状态；必须是 distinct starting run、非空新 owner、startup intent、未 launch confirmed、尚无 Native UUID。只读恢复要求新 reservation 没有 WriteKey。原 run 必须与密封记录准确一致、成功、launch confirmed、owner 已清空且 reservation 已释放。

新旧执行必须属于同 task/project/role、同 plan revision 和完整 frozen Target，包括模型、账号、route/credential identity、effort 和已有权限字段；新 attempt/generation 均大于旧执行。cwd 必须准确相同，原 Source 目录身份和已批准读取文件必须仍有效。归档持有完整 14 个私有字节副本，原 Native 目录删除后仍可准备恢复；归档/key/Source 漂移则拒绝。准备层不产生外部模型调用，不消耗或退款旧 budget。Inspector/current 权限仍是可信服务责任，不以 Host fixture 代替真实准入。

`restoredCheckpoint.readTools` 只向新的 run/generation/role、原 Native UUID/cwd/model/pin 的准确 Binding 导入原完成 Read。核对当前 Source 的 identity/mode/size/mtime/字节，将原批准 call ID/name/arguments/path/output 和真实 FileInfo 纳入新的私有记录；不读取 transcript 来创建授权。原记录标为 historical/completed/continued，重放 tool event 或复用旧 call ID 拒绝。原始内容及新 stage grant 的 raw/JSON 转义反射不允许外露。

每个 main HTTP 必须完整包含所有导入的 historical assistant/tool 配对，名称、路径、模型（如声明）、ID 和实际输出必须吻合；缺失、孤立、重复、伪造或已变 Source 均在 forward 前拒绝。session_title 辅助请求不消费 Read，也不承认 tool 配对，但仍经原 CallGate/Current/Permit 和 Source 检查。Native 历史自动压缩或遗漏不能绕过完整性检查。

历史 Read 不计入新 run 的 tool-turn 数；新批准 Read 仍受本次 MaxTurns 限制，历史与新记录合计仍最多 64 项/512KiB。每次 HTTP，包括重试和辅助调用，仍通过新的 ModelAudience 和真实 Scheduler.Permit 独立记账。导入不会提升写权限、免除模型或 billing/Quota 核验，也不会将旧 grant 重新激活。

后续必须完成 typed seed 写入全新私有 Root、新 config/grant/端口、准确 `--resume UUID`、当前 Source/项目授权、持久新旧 run 映射/幂等 receipt，并验证完整输出协议、取消、实际 wait/StopProof/Release。当前组件只准备可信状态；实际 Native Resume 和应用重启恢复仍未实现，不能据此登记产品恢复能力。
