# WP-11-SOURCE-SEAL-01：项目副本发布前复核来源

基线 `935007f20366f6defe8eb956728cae59530744eb`。完成 Copy 的来源复核组件；合同见 [workspace-source-seal.md](../../../contracts/workspace-source-seal.md)。完整项目工作流、真实账号/计费/Quota、产品执行注册/GUI、实际派单的整个 Source 接线和 60 类最终 Gate 仍未完成。Jev off，Go1.26.3，真实模型/额度调用0。

保留 Copy 公开签名，改为在已打开 os.Root 上遍历。私有 seal 记录每个纳入条目的身份、权限、owner、文件内容和时间；返回前重新核对整个源树。新增 Snapshot.SourceCurrent，公开 Path/Files 不参与其判断。保持排除认证、链接/特殊文件和原文件/bytes 上限，增加20000条目上限。来源校验是有界观察，不宣称跨文件原子快照或外部写锁；未注册为产品真实权限。

编译 RED 保留缺失方法/复制内部入口。补充确定性发布前回调（仅同包内部测试，公开 Copy 无 callback）：原文件已复制后改变内容、添加文件、替换根目录，三个场景均实际 RED，增加整体 SourceCurrent 复核后 GREEN，拒绝并清理临时副本。另对新增目录上限做负向检查：仅在本项自有预提交代码中将阈值从20000移到200000，实际20000空目录被错误发布而 RED；恢复最终阈值后 GREEN。此检查单独记录，不冒充旧基线的文件描述符泄漏：原 callback 内 defer 已会逐文件关闭。

新增5项顶层测试：私有 seal 不受公共清单篡改、10类整个树漂移、3类发布前变化、认证排除与副本改写不动源、目录上限。加上原2项测试验证合计7顶层/15子测试。最终回归与构建结果由 test-results.json 和原始日志记录；SKIP单独保留，不算通过。完整Fusion race最终454 PASS/20 SKIP/0 FAIL，656个PASS子测试；Go1.26.3 CLI/GUI build和vet均exit0。graph13618 nodes/119589 edges，此后仅文档与证据。Runtime/HTTP接口及实际Native路径未改变，没有重复真实模型调用。

复现（helper首个参数为日志文件路径，独立HOME/XDG）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-source-host.log test -race -v -count=1 -timeout=90s -mod=readonly -tags fusion,nogui ./internal/fusion/workspace
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-source-all.log test -race -v -count=1 -timeout=240s -mod=readonly -tags fusion,nogui ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

SourceCurrent 只读原来源，生成副本修改不改变源。进程内私有 seal 不序列化为重新启动授权。产品 Resolver/Adapter 仍需把当前项目登记、外发许可、来源观察、准入和实际执行关联；本项不完成这部分。目的目录必须由可信宿主管理，复核后任意外部文件变更与目的目录并发发布仍有后续边界。
