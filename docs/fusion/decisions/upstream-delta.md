# Fusion 与上游差异边界

锁定上游：`1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`，tree 与依赖 hash 见 `upstream-lock.json`。上游 main 不作为开发期间的自动更新源。

已完成原始快照、许可证及出处导入；Fork workflow 的发布、Docker 与付费预览只允许在上游仓库执行。Google OAuth A 使用私有外部配置，既有验证与推送保护记录保留。GLM 的凭据录入、Claude Code 连接诊断属于独立脚本，不将 GLM 编程套餐转成自建网关的普通生成通道。

WP-02 新增 `internal/fusion/isolation`、Fusion tag、私有构建/启动脚本；原版 appdir、Gateway、更新、插件、DAV、开机启动、GUI 与旧共享凭据入口仅增加相关身份/拒绝接缝。未重写 module path、SQLite、协议转换或现有 UI。未加 tag 时保留原版回归语义；Fusion 产品构建必须加 tag。

新行为：独立目录、loopback 端口、窗口标题、服务命名，启动拒绝不安全环境；关闭会改写执行环境或发现日常账号的自动机制。具体文件及证据见 `work-items/WP-02/changed-files.txt` 和该工作项的 commit。

后续上游同步只在独立分支执行，回归实际执行路径和上述接缝，不以编译通过替代准入。WP-30 再落实同步演练与差异回归。当前不合并 main、不向 upstream 推送、不发布 release。
