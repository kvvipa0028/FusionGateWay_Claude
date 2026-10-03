# ADR-001：以显式构建标识隔离 Magpie 与 Fusion

状态：已采用，2026-10-03。范围：WP-02，本机 macOS/arm64 开发构建。

保留现有 Go module、SQLite、Wails 界面和原版协议实现。Fusion 使用 `fusion` build tag，产品名为 `Fusion Gateway`，二进制名为 `fusion-gateway-cli/gui`。未指定该 tag 的构建保持原版回归行为，不能作为 Fusion 产品运行或交付。

使用仓库外的私有 state root 和独立 HOME/XDG。启动程序先校验目录、权限、Git 边界与环境；不符合条件即退出。开发构建固定 `127.0.0.1:3426`，忽略旧 `MAGPIE_ADDR` 和 LAN 设置，避免接管原版默认 `3425`。原版 portable 标记也不会被采用；开发启动暂拒绝 portable 模式。

开发构建关闭原版更新、插件执行/安装/更新、云同步、开机启动、协议注册、客户端自动目录同步及账号自动切换/预热。共享 Keychain 不随 HOME 改变，因此同时阻断旧 Claude/Cursor/Copilot 凭据发现。三个官方 Runtime 后续通过独立准入与明确的 credential reference 接入，不能用旧内置或插件路径绕过该决定。

新增工程工作流总开关保持 off。当前源码提供隔离基础，未提供五角色执行、strict locked 或新任务 API。WP-06/WP-07/WP-08 等仍需各自落实鉴权、执行合同和准入，不能以开发隔离替代它们。

代价：构建与启动必须使用项目提供的脚本；普通 `go build` 会产生原版语义的程序，launcher 将拒绝。暂不提供 Fusion 开机服务或 Windows 私有目录准入。后续发布若开放插件、同步、其他平台或服务，需要独立验证并更新本决定，不能直接重新启用原版更新通道。
