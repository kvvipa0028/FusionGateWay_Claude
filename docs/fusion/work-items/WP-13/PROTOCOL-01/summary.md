# WP-13-PROTOCOL-01 证据

基础 HEAD：`f91a82084ee2f87bb506a17ce78598e842307d7a`。本工作项只交付 Native characterization 与纯 headless 协议观察器，不交付实际 Runtime Adapter 或准入。代码位于 `internal/fusion/runtime/grok/`，合同见 [grok-headless-protocol.md](../../../contracts/grok-headless-protocol.md)。

先运行不存在 Session/Binding 的测试，保留 [RED](wp13-protocol-red.log)，随后实现并运行 [GREEN](wp13-protocol-green.log)。测试覆盖实际终态/EOF、旧 run/generation、当前身份失效、工具声明、未知/工具事件、会话/模型/usage、Cancel/error、JSON 歧义/大小/总量以及 Native 辅助 HTTP 与 end.modelCalls 不一致。

实际固定 Native 四个独立诊断：[baseline](baseline/characterization.json)、[Read](read/characterization.json)、[restricted](restricted/characterization.json)、[invalid model](invalid-model/characterization.json)。原生 stdout/stderr 在各自目录；只把随机临时根替换为 `<fixture-root>`，不伪造终态。每次都使用新的私有目录/空项目，不继承父认证。全部使用合成模型回复，未真正执行工具；本机 publisher [verify](wp13-codesign-verify.log) 与 [display](wp13-codesign-display.log) 单独记录。

复现要求：本机 macOS/arm64，固定 binary 位于脚本中的已登记路径，Python3，sandbox-exec。在开发 worktree 执行：

```sh
set -e
for mode in baseline read restricted invalid-model; do
  python3 docs/fusion/work-items/WP-13/PROTOCOL-01/characterize-native.py \
    --mode "$mode" --output ".fusion-dev/implementation/wp13-$mode"
done
```

脚本先验证 binary hash；只向 127.0.0.1 临时端口提供 SSE；记录模型/tool/purpose，不记录 Authorization 或真实凭据；每次限时 20 秒，退出/异常时先清理自己创建的进程组、wait，再删除临时目录及关闭假服务。未知模型预期 Native exit1/HTTP0，其余预期 exit0；restricted 断言两个 fixture-model HTTP、主请求只有 read_file。日志中的 git spawn denied 保留。该辅助脚本的诊断 profile 与子进程清理不等于受管 Supervisor 的生产停止证明。

各次正式检查与数量见 [test-results.json](test-results.json)，源文件与原始证据 hash 见 [artifacts.json](artifacts.json)。hash 在提交树中核验；原始 logs 保留，包括 RED 和 stderr 的原有 whitespace，源码/文档 diff 检查排除 `*.log`。Go1.26.3 临时 HOME/XDG 环境执行 targeted/full Fusion race；生产构建使用 fusion tag。

未验证：真实 X 登录、订阅/Heavy、账号/计费/额度池、effort、完整受管调用与进程取消、tool execution、准确 Resume、安装/notarization/更新、最终 Gate。Jev off；真实模型/额度调用 0，没有 fixture 变更真实准入。
