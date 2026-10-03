# macOS Worker 沙箱证据

实测宿主 macOS 27.0.1/arm64、Go 1.26.3，原生 sandbox-exec + default deny profile。没有使用完整 system.sb 导入，避免额外权限；读取本机 /System/Library/Sandbox/Profiles/system.sb 与 sandbox-exec man 来核对系统初始化。dyld 需要根目录本身 file-read，允许 literal /，未允许整棵文件树。系统库/可执行文件只读映射，私有 Worker HOME/项目可读；写权限只限 HOME/XDG/TMP 以及已批准可写项目副本。默认拒绝网络、Mach 服务、IPC 与 process-fork。

真实原生 C fixture 有阳性对照：沙箱外 SecurityServer bootstrap lookup、合成 numeric loopback connect 与 fork 成功；同一 fixture/OS 在沙箱内均拒绝。lookup 仅检查可达端口并释放，不查询任何 Keychain item。Go helper 则证明不可继承合成管理环境、不可读取兄弟目录合成 secret、不可越界写/只读写，允许项目写与稳定取消。

记录 PID 与 kernel P_starttime，不把 PID 单独作为身份；错出生时间拒绝 signal。实际 WaitStatus exited/signaled 后才核实退出。禁止所有派生使孙进程不能创建；这不是支持多进程工具的证明。实际 PID reuse 未强制产生，多进程/网络/原生 resume 尚 unsupported，真实 Native 路线另行准入。

本机 man 标注 sandbox-exec DEPRECATED。本能力仅本机已验证版本；环境漂移需重新执行 native fixture。沙箱与签名/模型路由/计费/额度是独立证据，不能相互替代。
