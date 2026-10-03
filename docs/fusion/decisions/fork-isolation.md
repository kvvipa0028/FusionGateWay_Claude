# Fusion 本机开发隔离

适用当前 macOS/arm64，Go 1.26.3 和已安装的 Xcode Command Line Tools。执行位置为当前开发分支仓库根目录。构建脚本使用临时 HOME/XDG、环境白名单、Apple clang 与显式 SDK；日志和二进制保存在忽略的 `.fusion-dev/`，不覆盖原始基线日志。

```sh
python3 scripts/fusion/build-dev.py
python3 scripts/fusion/run-dev.py -- fusion-status
python3 scripts/fusion/run-dev.py -- --version
```

两种构建及 vet 必须为 exit 0。`fusion-status` 应返回 `product=fusion-gateway`、`gateway_address=127.0.0.1:3426`，工作流、更新、插件、云同步、开机启动均为 false。GUI 编译警告保留在日志中，不视为编译失败，也不当作 GUI 交互验证。启动 GUI：

```sh
python3 scripts/fusion/run-dev.py --binary .fusion-dev/fusion-gateway-gui
```

GUI 仍为原版能力加隔离的开发界面；新增任务界面与严格执行入口未实现。直接启动 Fusion 二进制而未通过隔离检查时会拒绝，不能用日常 HOME 补齐环境。旧 `magpie://` 注册、安装目录和日常 Codex/Grok/Claude 配置不由启动程序自动接管。

| 对象 | Fusion 开发位置/策略 |
|---|---|
| state root | 默认 `~/.config/fusion-gateway`；设 `XDG_CONFIG_HOME` 时由入口解析该私有位置 |
| 客户端 HOME | `state root/runtime-home` |
| 配置、账号、日志、SQLite | `state root/config/fusion-gateway` 下的现有文件布局 |
| 缓存 | `state root/cache/fusion-gateway`；系统缓存也在隔离 HOME 下 |
| TMPDIR | `state root/tmp` |
| GLM key | `state root/credentials/glm-coding-plan.key`，不注入网关全局环境 |
| Gateway | 固定 loopback `3426`；原版默认 `3425` |
| 服务标识 | `local.fusion-gateway`；当前开机启动入口关闭，不安装服务 |
| portable 标记 | `fusion-data` / `.fusion-portable` 与原版区分；当前开发启动不开放 portable |
| 更新、插件、云同步、账号预热 | 服务端及低层入口拒绝或停用 |

目录 `0700`，由本人拥有，不能为软链接或位于 Git checkout；凭据文件 `0600`。启动环境不继承其他 provider key、原客户端目录或原版地址变量。HOME 隔离不能隔离系统 Keychain；旧共享凭据发现有单独的拒绝测试。

失败时查看 `.fusion-dev/build-dev/results.json` 和对应日志。拒绝启动时核对构建 tag 和私有目录权限，保留已有配置，不通过 chmod 公共目录或切回日常 HOME 绕过拒绝。不启动上游更新或插件安装来修复 Fusion。

回退：停止本开发实例，保留私有 state root；切回已记录的源码/构建。不要运行 `git clean -fdx`、删除私有凭据或覆盖 Magpie 的配置。当前无新任务 schema、运行中的任务或数据库迁移；后续 WP-05/WP-25 上线后，回退必须按对应数据恢复合同执行。

验证及明确未验证内容见 [WP-02/summary.md](../work-items/WP-02/summary.md)。
