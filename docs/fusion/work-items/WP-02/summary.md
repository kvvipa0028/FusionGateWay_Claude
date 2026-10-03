# WP-02 开发隔离验收

完成显式 fusion 构建身份、独立私有 HOME/XDG、loopback 3426、服务名称及启动拒绝。产品构建拒绝旧 portable 状态；旧上游更新、插件执行/安装/更新、云同步、开机启动、客户端自动同步及账号自动切换/预热被关闭。旧共享 Keychain 发现被阻断，假 security 测试确认零调用。

14 项 Fusion 拒绝/隔离测试、24 项 Python 工具测试通过。受影响原版语义回归 2,892 pass / 61 skip / 0 fail；skip 不记为通过。Go 1.26.3 下 Fusion CLI/GUI 构建和 vet 均 exit 0；GUI 的现有编译警告完整保留。真实运行的 CLI 状态与私有目录、关闭开关和端口一致；无隔离环境直接启动时退出 1，没有创建 HOME 文件。日常 Claude 配置与认证文件前后 hash 一致。

开发数据与原 Magpie/日常客户端分离的源码路径、目录拒绝、服务拒绝及元数据已核对；没有启动两套真实 GUI 并行交互，没有安装/升级服务。未证明五角色、strict locked、新任务 API 或实际 Runtime 准入。T26/T52/T60 的最终端到端状态仍为 not_run。

原始 baseline 文档和二进制证据保留。verify-baseline.py 的后续执行结果改存 ignored baseline-validation，无 tag 检查不再覆盖历史报告或 Fusion 产品二进制。
