# 交接

前置 WP-01/WP-02 已完成；base commit 为 `2d5fdd2`。下一包 WP-04 定义五角色的完整绑定合并和冻结，使用 fixture 标识做离线验证。

`NewProvider(Scenario)` / `NewTransport(URL)` 支持明确模式；Transport 的 Outbound 与 Denied 区分请求实际送出和提前拒绝。

`ServeRuntime(stdin,stdout,spawn)` 处理单个有界请求和 seq 事件；CLI `cmd/fake-runtime` 必须带 fixture 环境标记。`write_loss` 非零退出但留下写入，不能自动盲重跑。`cancel_race` 用 release-marker 控制完成时机；孙进程在独立进程组中清理。

`CreateSyntheticRepo(template,root)` 只导入固定四个普通文件、不覆盖已有目标；外部链接只指向合成兄弟文件。错误模板与只读/越界用例见 fixture-index。
