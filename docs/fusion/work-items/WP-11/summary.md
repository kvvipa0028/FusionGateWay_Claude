# WP-11

后续子项 [SOURCE-SEAL-01](SOURCE-SEAL-01/summary.md) 完成项目副本发布前的整个源树复核与私有 SourceCurrent。原生命周期/沙箱证据保持，下文计数为初始交付；新增组件不等于产品整个 Source 准入或最终 Gate 完成。

实现受管 Worker 生命周期接口、独立 HOME/XDG/临时目录、项目副本与 macOS kernel sandbox。当前安全能力仅支持单进程、禁止网络、禁止子进程；需要这些能力的 Native Adapter 保持未准入，不自动放宽沙箱。

启动在既有持久化预留之后；launch intent/fsync 在 spawn 前，出生时间+PID+nonce/hash 标识进程，取消 TERM→200ms KILL 并等待真正 reap。超限输出、timeout、取消竞争、旧 generation 与写后失联都有明确终态；写入后失败进入 interrupted/needs_review，禁止重放。StopProof 仅由当前监督器实际退出记录核验，重启不能凭 JSON 伪造或恢复。

104 个 Fusion 顶层 race 测试通过，2 个 helper 仅在父测试跳过；CLI/GUI/full vet 通过。原生 C fixture 在沙箱外成功连接合成 loopback、查找 SecurityServer 端口、fork，在沙箱内三项拒绝；未读取 Keychain item 或认证内容。另验证管理环境不继承、合成凭据不可读、越界写被拒绝、只读阶段不能写、TERM 忽略时 KILL、取消/完成只有一个持久化终态。

RED→GREEN 修正 dyld 根目录读取需求及控制器 flock FD 缺少 O_CLOEXEC 的真实继承问题。PID reuse 用实际当前进程 + 错误出生时间证明拒绝发信号，不宣称在 OS 强制造成 PID reuse。禁止派生保证孙进程无法创建；并未证明任意多进程树的停止。原版 WP-03 孙进程诊断不升级成 Native 准入。最终验收仍 not_run。

后续 [WP-15-SOURCE-GUARD-01](../WP-15/SOURCE-GUARD-01/summary.md) 完成私有来源记录接入执行/归档边界，固定Grok/Claude Native源漂移拒绝成功并实际停止/释放通过。完整Fusion460PASS22SKIP0FAIL，Native7顶层/17子测试通过；整体产品登记/真实路线/最终Gate仍未完成。
