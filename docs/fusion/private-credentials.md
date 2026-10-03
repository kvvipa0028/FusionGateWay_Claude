# 本机私有凭据录入

GLM CN Coding Plan 通过 Claude Code 接入。真实 key 保存在仓库外，不放进聊天、Git、命令行参数或 `~/.claude/settings.json`。

在仓库根目录执行：

```sh
python3 scripts/fusion/private-glm-key.py init --dialog
python3 scripts/fusion/private-glm-key.py status
```

macOS 会弹出隐藏输入窗口。终端录入可省略 `--dialog`，使用 `getpass`。默认文件是 `~/.config/fusion-gateway/credentials/glm-coding-plan.key`；设置了 `XDG_CONFIG_HOME` 时，以该目录替代 `~/.config`。私有目录权限为 `0700`，文件为 `0600`。工具拒绝覆盖已有 key、使用软链接或写入 Git checkout。`status` 只返回配置状态与路径，不显示 key，也不据此判定认证成功。

2026-10-03，用户已通过本机隐藏窗口完成录入。该结果只证明凭据已按上述边界保存。套餐档位、剩余额度、计费权益、Claude Code 真实调用和 WP-14 Adapter 仍需分别验证。

不要用 `cat`、环境变量打印或调试日志检查 key。如果录入失败，请检查目录与文件权限，保留已有文件；不要将 key 粘贴到聊天中。更换 key 属于显式凭据轮换，应在停止使用该凭据的任务后处理。
