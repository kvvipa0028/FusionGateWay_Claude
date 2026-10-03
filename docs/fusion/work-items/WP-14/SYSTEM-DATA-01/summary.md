# WP-14-SYSTEM-DATA-01：Native 启动的系统数据读取

本子工作项 `done`，父 WP-14 保持 `in_progress`。macOS Worker profile 仅增加 `/usr/share/icu` 与 `/private/var/db/timezone` 两处系统数据的只读权限；网络、fork、Mach/Keychain、IPC、环境白名单和项目写权限合同保持原有边界。

固定 Claude Code 2.1.287 在旧 profile 中、首个假上游请求前无输出超时。主线程采样与固定文件的有界反汇编指向系统 `ucal_openTimeZoneIDEnumeration` / `uenum_count`。这是启动依赖故障；JIT 权限试验没有消除故障，因此未增加 JIT、Mach、fork 或通用文件树权限。

真实系统 libicucore 的 C probe 在沙箱外有阳性对照，旧 Supervisor profile 内返回枚举错误（exit 98、状态 failed，RED）。补充两个数据目录后，同一 probe 在实际 Supervisor 内完成枚举并获得当前控制器可验证的 StopProof（GREEN）。probe 使用系统动态库和公开 ICU C ABI，不捆绑替代数据；[ICU Calendar API](https://github.com/unicode-org/icu/blob/main/icu4c/source/i18n/unicode/ucal.h) 与 [Enumeration API](https://github.com/unicode-org/icu/blob/main/icu4c/source/common/unicode/uenum.h) 提供接口声明。

独立 Native 无工具诊断只使用空私有目录、synthetic key 和本地 fake Anthropic SSE，四组消融结果保存在 `native-system-data.json`：

| 系统数据读取权限 | 实际 Native 结果 | 假上游请求 |
|---|---|---|
| 无 | 启动超时，父进程终止并 wait | 0 |
| 仅 ICU | 同上 | 0 |
| 仅时区 | 同上 | 0 |
| ICU 与时区 | exit 0、匹配 success 与固定 fixture 结果 | 1，glm-5.3、tools 0 |

诊断始终禁止 fork，只额外开放假服务的单个 localhost 端口。它手工构建同等启动 profile 并传入 synthetic Native env，**不经过生产 Supervisor**；parent wait 不能作为 Native 生产 StopProof，loopback 通信不证明真实上游准入、计费或额度。真实模型调用数为 0，未读取用户 key 或日常 Claude 配置，也未放开共享 `/tmp/claude-{uid}`。

复现命令（仓库根目录、已安装固定 hash 的 CLI）：

```sh
python3 docs/fusion/work-items/WP-14/SYSTEM-DATA-01/characterize-system-data.py
```

完整 Fusion/nogui tagged race：190 个 top-level PASS、2 个父进程 helper SKIP；Runtime 包单独回归 11 个 PASS、1 个 helper SKIP。既有真实 C 阳性对照和沙箱内 Mach/网络/fork 拒绝、兄弟目录读、越界写、只读项目、取消和终态证明均通过。Go 1.26.3 CLI/GUI 构建和全仓 Fusion/nogui vet exit 0。

仍需完成生产 GLM Adapter、受管 loopback 出口与全部 HTTP 请求预算、Native 凭据通道、工具/子进程合同及真实套餐/额度/计费验证。生产 profile 仍无网络和 fork；当前修复不将 WP-14、WP-17、Gate A 或最终 T 场景标为通过。
