# WP-14-PRIVATE-CREDENTIAL-01：冻结私有 GLM 凭据文件

子工作项 `done`，父 WP-14 保持 `in_progress`。新增 `glm.FileCredential` 可信控制器服务，可将 `Load` 直接接到 Adapter/QuotaReader。scope 固定 account/workspace/credential identity 与 coding_plan，不使用环境变量、旧 provider 配置、Keychain、浏览器认证或另一 provider 的 key。

仅接受绝对 clean 路径、固定 `glm-coding-plan.key` 文件名；构造与每次 Load 从 `/` 开始逐层目录 FD + `openat(O_NOFOLLOW)`，拒绝任意 symlink 和任意 Git 祖先。末两级私有目录及普通文件要求当前 UID、无 group/other 权限；文件仅一个 hardlink、8–4097 bytes（key 最多 4096、可有一个尾随换行）。拒绝 FIFO、占位 key、控制字符与空白。文件打开/读取后再 fstat，不回显路径、底层错误或 key。

构造仅在服务的私有内存保留 content digest 与 device/inode，不写出 key hash。每次加载重读并核对 inode/content/权限，换文件（即使内容相同）、换 key 或权限漂移均拒绝；运行中的任务不会静默跟随密钥轮换，需新 credential identity、服务注册和对应配置版本。JSON/String/GoString 不输出 secret。darwin/linux 使用此路径；其他系统明确 unsupported，不模拟所有权检查。

该服务只把本地文件冻结到声明的 identity，不证明上游真实账号、Coding Plan 计费或物理共享额度池。它不能替代 Registry/Inspector 的完整证据，不自行注册产品入口。

## 验证

缺少 API 先 RED，三个 targeted race tests GREEN；包括五种 scope/context 变化、十五种不安全初始配置与八种运行后漂移。原始日志保留同目录。

现有实际 pinned Native Adapter 测试改接该服务的 synthetic 私有文件；design、implementation/Edit 创建、SDK retry、wrong identity、late quota、current identity、cancel in flight 与 rotated_file 共八个场景 PASS。rotation 在启动前拒绝，不写 launch journal、不发送上游请求；四个实际执行场景继续逐 HTTP 预算和真实 StopProof/Release，真实模型调用 0。

显式真实 CN quota 诊断也改用同一服务，去掉测试中的重复 loader：本轮一个真实 GET 成功，两个模型窗口 used_percent=0/93；账号/pool/billing/model/effort 与生成准入仍 unverified。真实 key 没有进入 Worker、日志、文档或 Git。

完整 Fusion tagged race：220 个 top-level PASS、7 个 SKIP、0 FAIL；Go 1.26.3 CLI/GUI 编译、Fusion/nogui 全仓 vet exit 0。产品注册、固定真实 generation Transport、完整路线/额度证据、恢复与 Gate A 仍未完成。Jev off。
