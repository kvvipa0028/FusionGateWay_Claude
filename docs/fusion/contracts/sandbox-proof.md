# macOS Worker 沙箱证据

实测宿主 macOS 27.0.1/arm64、Go 1.26.3，原生 sandbox-exec + default deny profile。没有使用完整 system.sb 导入，避免额外权限；读取本机 /System/Library/Sandbox/Profiles/system.sb 与 sandbox-exec man 来核对系统初始化。dyld 需要根目录本身 file-read，允许 literal /，未允许整棵文件树。系统库/可执行文件只读映射，私有 Worker HOME/项目可读；写权限只限 HOME/XDG/TMP 以及已批准可写项目副本。默认拒绝网络、Mach 服务、IPC 与 process-fork。

真实原生 C fixture 有阳性对照：沙箱外 SecurityServer bootstrap lookup、合成 numeric loopback connect 与 fork 成功；同一 fixture/OS 在沙箱内均拒绝。lookup 仅检查可达端口并释放，不查询任何 Keychain item。Go helper 则证明不可继承合成管理环境、不可读取兄弟目录合成 secret、不可越界写/只读写，允许项目写与稳定取消。

记录 PID 与 kernel P_starttime，不把 PID 单独作为身份；错出生时间拒绝 signal。实际 WaitStatus exited/signaled 后才核实退出。禁止所有派生使孙进程不能创建；这不是支持多进程工具的证明。实际 PID reuse 未强制产生，多进程/网络/原生 resume 尚 unsupported，真实 Native 路线另行准入。

本机 man 标注 sandbox-exec DEPRECATED。本能力仅本机已验证版本；环境漂移需重新执行 native fixture。沙箱与签名/模型路由/计费/额度是独立证据，不能相互替代。

WP-14-SYSTEM-DATA-01 增加 `/usr/share/icu` 与 `/private/var/db/timezone` 的只读数据访问。未开放整个 /usr 或 /var，未授予这两个目录写权限或可执行映射。系统 libicucore 的真实时区枚举在旧 profile 返回错误，新增只读权限后在 Supervisor 内成功并获得当前控制器 StopProof；既有 Keychain/Mach、网络、fork、兄弟目录读取和越界写反例继续验证。

固定 Claude Code 2.1.287 的独立无工具诊断进行四组消融：无权限、仅 ICU、仅时区均在首个请求前超时；同时开放两个目录后完成一次本地假上游请求。诊断额外授予唯一 loopback 端口与 synthetic env，不经过生产 Supervisor，因此其 parent wait 不代表生产 Native StopProof 或网络准入。该历史修复本身没有开放生产网络或 fork，详见 [本项证据](../work-items/WP-14/SYSTEM-DATA-01/summary.md)。

WP-14-CHANNEL-01 的本机实验发现 remote tcp 数字 IP 规则被 sandbox-exec 拒绝；localhost:port 实际匹配 127.0.0.1 与 ::1，同端口均通过，对有阳性对照的邻端口拒绝。127.0.0.2 在本机不能绑定，明确不计作沙箱反例。对应生产通道必须同时独占两个 loopback 地址，只增加该一个端口；没有开放任意 localhost、network-bind、fork 或 Mach 服务。

更新后的原生 C fixture 在同一生产 Supervisor 内证明双栈通道均可达、邻端口仍拒绝、SecurityServer lookup 与 fork 仍拒绝。固定 Native CLI 另经生产 Supervisor、真实 Store 和 CallGate 完成四个合成场景及真实 wait/reap/StopProof。证据只覆盖这个宿主和固定 Native 的无工具模式，Bash/多进程工具仍未支持，真实账号/额度/计费与产品 Adapter 准入保持未验证。详见 [通道证据](../work-items/WP-14/CHANNEL-01/summary.md)。

WP-14-TOOLS-01 补充真实 Native 文件工具场景：Read 读取指定项目文件并在第二次假上游请求中携带合成内容，Edit 在三个模型回合中先读后修改，Edit 创建新文件在两个回合中完成，实际文件回读符合预期。三个阳性场景与越界读、越界创建、只读创建负例使用相同固定 Native、生产 profile、Store/CallGate 与许可 flags；只读创建和可写创建仅改变 OS workspace 写权限。负例均不产生禁止的文件变化，且获得真实 StopProof；没有开放 fork、兄弟目录文件读取或外部 network。Native bare 的 Write 工具未出现于 init，保留为 unsupported fixture，不声称 Write 能力已通过。
