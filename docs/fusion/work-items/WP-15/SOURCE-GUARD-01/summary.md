# WP-15-SOURCE-GUARD-01：来源复核接入受管执行

基线 `5b2d2a8bf52735dae307d2596bc9768199c25b02`。完成可信来源 Guard 的 Controller、Grok/GLM Adapter、Supervisor 和归档接线；合同见 [workspace-source-guard.md](../../../contracts/workspace-source-guard.md)。产品真实项目/账号/数据/Route/quota/pool 准入、产品执行 bootstrap/GUI、Codex Native、工程闭环与60类最终 Gate仍未完成。Jev off，真实模型/Quota调用0。

Guard 由成功 Copy 的私有记录产生，以值传入 Spec，不从 HTTP/公共 manifest/外部 Snapshot 重建，String/GoString脱敏、不序列化授权。私有绑定实际Cwd目录身份、owner/group/权限；源检查前后重查副本，生成文件变化不当作原来源变化。Controller.RequireSource=true要求新执行和明确恢复存在Guard，缺失/漂移/错误Cwd在intent前拒绝。保持旧诊断零值兼容，不能作为Source verified；实际产品 bootstrap 必须显式启用，并独立绑定当前项目登记和完整准入。

慢Probe、Inspection前后、prepared检查后再次核验。prepared之后失效沿既有无Handle合同：Store interrupted/needs_review，Completion execution_uncertain，保留reservation、没有模型调用或虚构stop proof。Native Adapter Current在请求/事件/工具/结果边界核验，Supervisor在intent/spawn前、heartbeat和validator前后核验。检测到闲置进程失效按原取消/wait/reap链停止。归档保留原Guard/Cwd，producer前后复核；不宣称已经外发/已私有发布内容可自动回滚。

证据：

- 新Host6项顶层PASS、7个PASS子测试：缺失/错误Cwd/已漂移、Probe/Inspection中变化、prepared后变化保留不确定状态、归档前/中变化拒绝ref、Guard不受公开清单伪造/副本身份替换、Supervisor闲置进程实际停止。补充原模块回归不重新定义其完成条件。
- Controller实际RED证实三类来源无效与慢检查/已提交变化仍会启动；修复后GREEN。最初Completion预期写为interrupted，按既有源码修正为execution_uncertain并另断言Store interrupted，未更改保留容量规则。
- 固定Grok1.0.48实际Native三场景：复制源文本成功(2HTTP)、复制源Read成功(3HTTP)、第一HTTP中显式改变原来源后拒绝成功(1HTTP)。原生RED原先仍succeeded，修复后failed，无成功文本，实际wait/proof/release通过。
- 固定Claude2.1.287实际Native两个场景：复制源成功、第一HTTP中显式漂移后拒绝成功；各1HTTP、持久预算均1。负向验证暂时移除本项自有Current中的来源检查后实际succeeded RED，恢复最终检查后failed GREEN。没有使用真实key。
- Supervisor闲置来源漂移在旧检查下6秒等待超时RED；heartbeat检查后约2秒实际取消/wait/可信proof GREEN。归档前/中变化负向关闭本项自有guard后实际披露ref RED，恢复后拒绝。测试时所有临时变化已恢复，最终检查覆盖最终代码。
- 完整Fusion race **460 PASS/22 SKIP/0 FAIL，663个PASS子测试**。Native另跑，不把SKIP当PASS。Go1.26.3 CLI/GUI build、完整vet exit0。

最后新增带实际Guard的Grok归档→准确UUID恢复，与既有Grok/Claude生命周期、Read、暂停/取消、断线/Close和恢复场景合并验证，最终7顶层PASS/17子测试PASS，20场景/25次实际Native启动；新Source共6场景/7次Native，包括Source归档恢复的两次进程与3次HTTP。最终Native新增场景只改变显式pin fixture（完整套件中此parent按设计跳过），未改运行代码；不重复已有通过的完整套件。graph最终13638 nodes/119790 edges。

原Grok fixture日志沿用sourceUnchanged字段，但其断言比较的是实际Cwd中的fixture.txt。新漂移场景的原来源由fixture显式修改，受管Cwd仍未被Native改写；不把该字段解释成原来源没有外部变化。Native经内部可信Controller验证，未将其与独立的Management HTTP测试拼成真实产品端到端结论。

复现（helper第一个参数是日志路径，隔离HOME/XDG）：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-source-guard-host.log test -race -v -count=1 -timeout=90s -mod=readonly -tags fusion,nogui ./internal/fusion/control ./internal/fusion/runtime ./internal/fusion/workspace -run '^Test(Controller(RequiresSource|RechecksSource|PreparedSource|CheckpointRechecksOriginalSourceGuard)|SourceGuardIsImmutableAndBoundToCopiedDirectory|SupervisorSourceGuardRevokesIdleProcess)'
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-source-guard-native.log test -race -v -count=1 -timeout=180s -mod=readonly -tags fusion,nogui ./internal/fusion/control -run '^TestControllerPinned(Grok|Native)' -fusion-control-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64 -fusion-control-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

Sources是有界观测，每次最多100MiB；不是树的原子快照、外部写锁或即时撤销。进程内Guard不重建持久权限，调用后外发/写入不会因失效自动撤销；整体验收仍须原计划的当前项目/真实权限与源数据要求。私有目的目录发布的并发管理仍需可信宿主完成。
