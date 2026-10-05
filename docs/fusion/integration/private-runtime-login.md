# 私有官方设备登录

2026-10-05：Codex 0.160.0、Grok 1.0.48 已使用最终登录 profile 和环境实际取得官方设备登录挑战，随后由检查进程取消并 wait。此项准备登录入口，没有完成本人账号登录，没有模型调用，也不代表生成路线已准入。记录见 [PRIVATE-LOGIN-01](../work-items/WP-17/PRIVATE-LOGIN-01/summary.md)。Jev 保持 off。

## 本机操作

在当前实施工作区中双击 `scripts/fusion/login-codex.command`、`scripts/fusion/login-grok.command`，分别在 Terminal 中完成官方设备登录。也可以在该仓库目录执行：

```sh
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py prepare --runtime codex
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py check --runtime codex
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py login --runtime codex

/usr/bin/python3 -I scripts/fusion/private-runtime-login.py prepare --runtime grok
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py check --runtime grok
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py login --runtime grok
```

`login` 的 stdin/stdout/stderr 必须全部连接本机交互终端；重定向任一输出会拒绝启动。一次性代码只在该终端读取并输入官方浏览器页面，不要发到聊天、工单或截图，不要开启终端录制或复制整个认证目录。Codex 的官方验证页为 `auth.openai.com`，Grok 的当前验证页为 `accounts.x.ai`；本次真实观察只保存 hostname 和布尔结果，不保存代码、完整验证 URL 或 Native 输出。

操作完成后在同一目录执行：

```sh
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py status --runtime codex
/usr/bin/python3 -I scripts/fusion/private-runtime-login.py status --runtime grok
```

`credential_file_present=true` 仅说明本人独立目录出现符合权限要求的缓存。工具不解析 token/JWT，不从缓存推断账号、workspace、权益、费用、额度或模型能力；验证标志保持 false。`official_login_process_succeeded=true` 需要官方进程 exit 0、文件/配置身份未变且私有缓存存在。下一步还需受控身份核验与生产服务接线，不能用此输出设置 Registry/Inspection true。

Codex 后续读取已有[私有凭据服务](../contracts/codex-private-credential.md)：要求明确缓存账号与独立登记一致，不从 JWT 补账号或推定权益；缓存变更拒绝静默跟随。该服务未接生产 Forwarder/Factory，不能将本登录工具的文件存在结果作为生成授权。

## 隔离与执行合同

- 使用 UID 数据库中的本机 HOME 定位固定 Runtime 和 `.local/share/fusion-gateway/auth/{codex,grok}`，不信任环境 HOME。认证位于 Git 外部，每路线独立；新建目录 0700、配置 0600，不复制日常认证。
- 独立 HOME、CODEX_HOME、GROK_HOME、XDG、TMPDIR；配置字节和目录 inode 重查。已有配置漂移、symlink/hardlink、宽松权限或已有 auth.json 均不自动覆盖。Grok 的配置种子并非企业 MDM 权限证明，固定设备登录 argv 和环境白名单才是本入口的执行边界。
- Python 使用系统解释器的 `-I`；Native 只执行已核验固定文件，启动前核对固定 hash、Apple identifier/Team，退出后核对文件身份。不能指定其他版本、模型、API key、endpoint、OIDC、代理、插件、hooks 或外部认证命令。
- Seatbelt deny default；只允许私有 HOME/缓存/临时目录写入，空 cwd 只读，fork 和 Keychain 服务不开放。Codex 仅读其自身 preferences；Grok 不增加此权限。
- 联网登录允许 TCP 443、DNS 53 和精确系统 DNS socket；离线 help 没有网络。此为端口限制，不是按 HTTP hostname 的防火墙。固定可信官方 login 子命令及冻结配置用于本次认证；它不能充当模型执行 Worker 的出口。
- 固定 `NO_PROXY=*`、`no_proxy=*`，避免复用机器代理路径。Codex 显式选择 root 拥有、不可由普通用户修改的系统 PEM CA，并重查身份；不接受环境自带 CA，不跳过 TLS 校验。网络要求代理时本入口可能失败，不能擅自换成付费 API 或放开整个沙箱。
- `login` 最多 900 秒，离线 help 最多 15 秒；SIGINT/SIGTERM 转发给所持有进程，超时 KILL 后 wait。macOS 拒绝 group signal 时，仅向自己的 Popen handle 发信号；profile 已拒绝 fork。成功返回、缓存出现或错误文本都不代替 wait。

## 失败处理与后续验证

用 Ctrl+C 取消本次终端登录。失败时保留已有认证，不自动重试、删除缓存、执行 logout、复制其他账号或修改日常客户端。系统 `/etc/grok`、`/etc/codex` 或 `/Library/Application Support/Codex` policy 存在时拒绝操作，需先核对现有管理要求。

不要把 Native 私有 `codex-login.log` 或整个 HOME 加入仓库。本入口不提供删除/替换缓存操作；如需重新登录，先核实具体私有文件归属和失效原因。回退可停止使用两个 `.command`，保留私有认证与历史证据；本项没有数据库迁移或 UI 改动。

实际本人登录、登录后刷新/保存、账号/workspace/地区、订阅权益、真实物理额度池、费用、生产 Forwarder、工程读写/测试/取消和实际 Wails 操作仍待完成。已有设备挑战不能证明这些要求。

相关官方说明：[OpenAI Authentication](https://learn.chatgpt.com/docs/auth)、[Grok CLI Reference](https://docs.x.ai/build/cli/reference)、[Grok Enterprise Authentication](https://docs.x.ai/build/enterprise)。DNS 精确 socket 规则另与 [WebKit 固定源码](https://github.com/WebKit/WebKit/blob/1f1ec0ac4034ce2e63772f557827153f1d46bf01/Source/WebKit/Resources/SandboxProfiles/ios/com.apple.WebKit.Networking.sb)核对，最终以本机 kernel 正反测试为证据。
