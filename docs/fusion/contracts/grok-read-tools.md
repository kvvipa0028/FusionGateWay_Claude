# Grok 受控文本 Read 合同

`ReadTools` 与 `NewReadSession` 是固定 Native `1.0.48/b94d5072c95f` 的 opt-in 只读链，代码在 `internal/fusion/runtime/grok/read_tools.go`。可信 controller 为一个准确 Binding 创建 `NewReadTools(binding,current)`，把同一个 scope 注入 `CallGateConfig.ReadTools` 和 `NewReadSession`，实际 Worker 停止后 `Close`。HTTP DTO 不接受 scope、root、许可回调、工具批准或快照字段。未注入的 Gate 和 `grok.New` 保持文本模式，拒绝实际工具。

Scope 冻结 run/generation/role、新 Native UUID、canonical Cwd、model、version/hash、MaxTurns、唯一 `read_file` 声明。构造时实际打开 Cwd 的 `os.Root` descriptor 并保存目录身份；每次边界重查可信 current、路径/目录身份和已批准的文件。其它 Native session、root replacement、关闭、权限变化或复用不同 Binding 均拒绝。current 回调须快速、线程安全、不重入本 scope；目录必须来自已登记项目/Worker Cwd，不能从模型请求取得。版本/hash 声明仍须调用方实际核验，scope 不是路线/账号/计费准入。

处理顺序：

1. Manager/Scheduler 继续为 title/main/retry 的每次 HTTP 验证冻结 Target、权限、额度、物理 reservation 和持久化预算。只读工具不绕过或替代每次模型许可。
2. SSE 完整缓冲后解析一个 `read_file` function call。index须0，type须function，id合法唯一，name匹配，arguments有界；支持 arguments 分片并拼接，所有 chunk 的 model/id/terminal 继续严格校验。多工具、backend/写工具、未知扩展或不完整 call 拒绝。
3. arguments 仅有 `target_file`，为 clean UTF-8 绝对路径或相对本次 Cwd 的路径。在响应进入 Native **之前**确认 canonical 项目范围，拒绝 `..` 越界、URI、NUL、symlink/目录链接/hardlink、目录及不存在/不可读文件。逐 component Lstat 与 Root.Open 检查；只接受 UTF-8/NUL-free regular text，文件≤64KiB，split-line count≤1000，每 run 最多64 grants且不超过MaxTurns，缓存总 raw bytes≤512KiB。offset/limit/PDF/image/Notebook转换、list_dir/grep 等协议尚未授权；并非默认对它们批准。
4. 保存文件身份、mode/size/mtime、实际 bytes 与固定 Native 的编号文本格式。原始源文本保存在私有内存、不写审计/Outcome/error。已知 stage/provider credential marker 不得出现在文件内容中；JSON转义及嵌入 argument JSON 也按解码字符串检查，不能靠 Unicode escape 跨过 request/response 反射保护。
5. 响应写出前再查 current。Native stdout 的 tool_call 必须匹配批准过的 id/name/kind/input；pending → location update → completed 必须有正确顺序、准确 path、FileContent 的 raw/text/concise/total-lines/offset，且仍匹配实际文件快照。未知 id、重放、额外字段、failed/伪造结果/缺更新均不能成功。Tool trace 不是新的授权来源：没有 Gate 的先前 grant 就拒绝。
6. 下一轮主 HTTP 必须包含已批准 assistant tool_calls 与对应 tool result，核对 id/name/args/text、位置顺序和历史重复；每次都重查文件。`model_id` 是实际 Native 的 assistant 历史字段，只允许等于冻结模型；它不提供模型路由权限。title 不确认 pending tool，也不能携带工具历史。重试允许重送准确历史，但必须再扣模型预算，不重批工具。
7. Gate 接受后续模型响应后标记 continuation，Session 必须看到 completed trace；`end/Finish` 只有 scope ready、headless有效终态和实际 exit0 才给出协议 succeeded。Gate Healthy 同样要求没有 pending/未完成/未确认结果。拒绝后的 scope permanently failed，需要新的 run/scope，不能重置或复用绕过失败。

这些控制只批准模型可见的具体文本 Read，并关联声明与结果。`os.Root` 的 controller 读取检查、metadata/bytes 重查不等于 Native 进程的 OS containment，也不能承诺主机并发换文件时 Native 从未读取其它 bytes。必须结合生产 Worker 的只读项目/受保护 HOME、不可外发认证、取消/进程树与停止证明；发生变化时不得转发不同结果、不得把 Raw stdout 保存成可信产物。此项没有建立这些生产证明，没有启用生产 NativeForwarder/Worker，所有 all-calls/strict-lock/billing/quota/StoppedVerified 等独立 flags 仍 false。

2026-10-04 的实际固定 Native 在合成 loopback/私有 HOME/XDG、自己创建的项目中完成13行文件读取及后续请求；真实 Store/Manager/Scheduler 记录 title+两个主请求三次预算，Native end.modelCalls=2。相同 scope 拒绝读取自己 GROK_HOME config，Native 未产生 tool event即退出1；未使用真实凭据或模型。profile 较宽的 Mach lookup/私有 HOME 权限不是生产隔离通过。

证据与复现：[READ-01](../work-items/WP-13/READ-01/summary.md)。真实订阅、模型/effort/账号/额度/计费、生产 Adapter/Worker、准确 Resume、写工具、完整工程闭环和最终 Gate 仍待完成，WP-13 in_progress，Jev off。
