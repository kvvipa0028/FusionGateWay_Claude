# WP-17 · PRIVATE-LOGIN-01

本组件完成本人独立官方设备登录的受限入口与准备，父 WP-17、Gate A 和整体工程目标保持未完成。基线 `c593d137f546a252872e5bf3ebcb438ff96673b7`；此前工作区干净，本组件已有未跟踪文件由同一实施任务继续完成。没有新增 Agent。

新增 `private-runtime-login.py` 和两个 Terminal `.command`，复用固定 Runtime 的发布者/文件核验。最终生产 profile、环境的真实设备挑战见 [Codex](codex-live-final.json)、[Grok](grok-live-final.json)：官方 hostname 与代码提示已观察，随后取消，实际 wait/reap，缓存未产生、模型调用 0。原始 Native 输出和一次性代码未导出。两份导出失败日志的行尾空格已规范化，原测试日志留在 ignored 开发目录，错误与断言未变。该观察证明取得挑战，不证明本人已经登录、上游最终模型或任何生成准入。

## 实现与验证

认证目录位于 UID HOME 的 Git 外路径；冻结配置、环境白名单和固定 argv；系统 Python `-I`；既有缓存/漂移/不安全文件拒绝覆盖；profile 保留空 cwd 只读、外部文件/Keychain/fork 拒绝，仅在线登录允许 TLS/DNS。Codex 使用受检 root-owned 系统 CA，固定直连，保留 TLS 校验。具体步骤、错误处理和边界见 [操作说明](../../../integration/private-runtime-login.md)。UI、原 CSS、Go 和上游源码未改。

- [首次 RED](red.log)：缺少登录入口，8 个测试失败。[首次 GREEN](green.log) 8 项通过；中间一轮测试使用 `private` 作秘密 sentinel，误命中 macOS `/private` 路径，修正为独特 sentinel，没有放宽生产断言。
- [kernel RED](kernel-red.log) 实际 exit 1：两个 profile 均允许写入空 cwd，真实 C fixture 返回 72；改成仅私有 HOME/缓存/临时目录可写后 [GREEN](kernel-green.log) exit 0。无沙箱正控制实际外部 RD/WR、fork、loopback TCP、preferences 均可用；同一 fixture 在离线/在线 Codex profile 中拒绝越界读写、cwd 写、fork、非认证端口、Keychain 服务和无关 preferences。
- [参数/取消 RED](argv-stop-red.log)：新固定 argv 断言实际失败；取消 fixture 最初受本机 `/usr/local/include/stdio.h` nullability 错误影响，未作为产品失败或通过。改成直接 write 系统调用后 [GREEN](argv-stop-green-final.log)。真实限时 C 进程验证 group signal 拒绝时，精确 owned signal 后 wait 且 exit -SIGTERM；终态不再重发信号。
- 真实 Grok 1.0.48 拒绝 `--oauth --device-auth` 组合（exit 2），改用官方单独 `--device-auth`。早期探针遇到 macOS group signal EPERM；后续明确处理并 wait，没有以超时或异常声称进程停止。
- [DNS RED](dns-red.log) 实际 exit 1，在线登录无法连接系统 DNS socket；[GREEN](dns-green.log) 包含 Codex/Grok 离线拒绝/在线允许与无沙箱正控制。额外真实 HEAD 诊断先证明相同公开请求沙箱外成功、沙箱内 DNS 失败，单加精确 mDNSResponder socket 后成功，不需要新增全网权限。
- Codex 仅加系统 CA 或仅固定直连仍请求失败；两者同时设置后，原最小 profile 即取得挑战。期间额外 trustd、dnssd、netsrc、全 outbound 的诊断权限均未进入产品。最终 [CA/environment RED](ca-env-red.log)→[GREEN](ca-env-green.log)，实际系统 CA 身份、用户自带 CA 不继承、symlink/普通用户 CA 拒绝通过。
- [stderr RED](stderr-red.log) 证明重定向 stderr 会进入准备流程；修复后 stdin/stdout/stderr 必须全部为 TTY，不能通过 stderr 将 Native 登录输出写入文件。最终脚本回归覆盖该拒绝且不会准备目录或启动 Native。
- [最终脚本回归](final-python-tests.log)：28/28 PASS，0 FAIL、0 SKIP，actual exit 0，其中本组件 13 个、发布者组件 15 个。系统解释器 `-I` 下固定 Native 离线 help 两条均 exit 0，见 [Codex help](codex-help-final.json)、[Grok help](grok-help-final.json)。终端 shell 语法通过；实际双击及本人登录/保存未验证。
- [有效 mutation](cwd-write-mutation.log)：隔离测试上下文再加入 cwd 写权限，两个真实 kernel 子场景断言失败，生产源码未修改。证明断言能发现权限回退。

本项没有改 Go、UI 或依赖，不重复上一组件已通过的 Go 构建或浏览器测试。Python AST/JSON/文档链接/diff、限定文件范围、私有 GLM key 原字节排除及原 UI 完整性检查见 [scope-check.json](scope-check.json)；[文件摘要](artifacts.json)。Graph 以 `repo_path` 刷新成功；首次使用不适用 `path` 参数的刷新报错未当成功，不阻断真实源码检查。

## 剩余与回退

两个本人官方登录仍需要在本机终端/浏览器完成，之后才能实施可信身份与生产凭据服务。本次没有借用日常账号、没有解析私有 token、没有模型/额度请求、没有账单/物理池证明、没有设置 Registry/Inspection true。GLM 原 key 与历史连接/额度证据保持原边界；三路线实际工程 Smoke、生产 Forwarder、最终 60 类验收未完成。

不自动 logout、删除缓存或更新 Runtime。回退可停止使用入口；私有 HOME 留在 Git 外，原 UI、Store schema、客户端配置和 Jev off 均未变化。WP-17 状态继续 in_progress，所有依赖及 T01–T60 状态未改。
