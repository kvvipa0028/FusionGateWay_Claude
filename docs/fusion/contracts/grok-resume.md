# Grok 准确会话恢复的实现边界

可信内部 `Adapter.ResumeCheckpoint` 已支持成功 stopped/released 会话的准确 UUID 恢复，使用新的 prepared run、私有 Root、grant/端口及 per-HTTP 预算，并校验历史 Read。实际证据见 [RESUME-01](../work-items/WP-13/RESUME-01/summary.md)。没有产品 HTTP API/Worker 注册或真实 X/计费/Quota 准入；没有准确 Spec/ref 的通用 Adapter/Session/Supervisor Resume 继续 unsupported。固定版本 `1.0.48/b94d5072c95f` 的[早期诊断](../work-items/WP-13/RESUME-PROBE-01/summary.md)不能单独替代这些控制。

以下是完整恢复必须满足的合同。归档见 [grok-checkpoint.md](grok-checkpoint.md)，准备与历史 Read 见 [grok-restore.md](grok-restore.md)，实际受管恢复及私有 prepared mapping 见 [grok-managed-resume.md](grok-managed-resume.md)。产品幂等入口、整体 Source/当前项目权限及独立路线准入仍待完成，不能据此登记整个产品的恢复能力：

1. 只选择同 task/project/role 的准确 Native UUID。旧 run 必须已由持有它的可信 Supervisor 完成实际 wait/StopProof，并核对 Store 的 generation、终态及停止/释放记录。unknown、取消副作用未核对、缺停止证据均保持 needs_review，不能直接接管。
2. 从已停止执行的私有会话目录建立完整、不可变归档，记录 run/generation/UUID、版本与 executable hash、frozen Target、canonical cwd/目录身份、权限及所有文件的名称/大小/SHA256。拒绝缺失、内容改变、symlink/hardlink/special file、未知布局及有界范围之外的归档。所有权和可信元数据与 Native 可写的会话内容分开；Native 自报 summary 不是授权来源。
3. 新执行使用新的私有 Root、grant、端口和运行预算。只从经过核验的完整归档复制会话目录，独立生成 config/prompt；禁止复制旧 config/API key/grant/全局日志、agent_id 或日常 HOME。逐 HTTP 调用重新 Permit，旧 budget 不退款，不能因“恢复”绕过配额、容量或 current 检查。
4. frozen model/account/workspace/credential identity/route revision/billing/effort/cwd/权限必须与原授权兼容，源码身份及当前项目权限仍有效；更换账号不能恢复旧会话。缺少或失效时暂停，不选择新模型、最新会话或重建相同 UUID。只传入明确 `--resume UUID`，不用无参 resume、continue、title selection 或 fork 代替原会话。
5. 历史文本作为未独立验证的旧产物；旧 assistant/tool 配对、tool ID/名称/路径/实际输出须与原执行时批准记录、原 Source 身份及归档关联。只验证本次 stdout 不够：历史 Read result 会进入 HTTP，旧 tool_call 不在本次 stdout 重放。已从可信 seal 导入原批准记录到新 ReadTools 并接通实际受管恢复，不能清空关联检查或凭 Native transcript 创建新授权。未完成工具、压缩/记忆/子 Agent 仍需独立协议证明。
6. 建立新 run/generation 和持久恢复映射，重复请求只读 receipt，不重复恢复。新 Native 输出仍须完整协议/当前身份/原 UUID/模型/end/真实 exit/EOF；成功文本不替代工程验收。取消/暂停/关闭使用 owned lifetime，实际 wait/StopProof 和可信 release，未知执行保留容量。应用重启不能用可恢复的 Native 文件推断旧进程已经停止。

诊断还发现：缺两份 transcript 后 Native 可 exit 0 且保留原 UUID，却丢掉旧对话；更换 model alias 可实际切换模型；另一个 cwd 参数可被接受但继续旧目录上下文。归档完整性、目标绑定和项目身份必须在恢复 Native 前落实，逐调用再检查，不能仅依赖 argv 或成功 end。
