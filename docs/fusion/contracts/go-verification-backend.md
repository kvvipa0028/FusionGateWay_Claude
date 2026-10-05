# 固定 Go 工具链真实验证

`evidence.Spec.Go` 只能由可信宿主在 Factory 构造时登记。HTTP、模型和导出 JSON 均不能注册工具链、添加 flags 或授权子进程。旧 Spec 的 nil 字段与旧 Record 的命令字段保持 omitempty，不改变旧 canonical bytes；SQL001–012 未修改。

## 冻结输入

本机首版固定 macOS arm64 Go1.26.3。登记 canonical SDK 根、完整文件内容/模式/路径 hash、精确 bin/go hash 和实际 `go version`；SDK 禁止 symlink 与 group/world writable 文件/目录。Package 必须是 `.` 或一个明确 `./relative`，不接受 `./...`、逃逸路径、调用者环境或 `-toolexec`。

逻辑命令固定为 `test -count=1 -json -vet=off -p=2 -buildvcs=false -pgo=off -mod=readonly|vendor PACKAGE`。实际执行分段记录，不把这个逻辑 argv 冒充直接执行过的命令。测试子树来自 frozen manifest，Acceptance 与已批准设计仍逐项绑定。

标准库项目直接离线编译；外部依赖必须随代码冻结进 vendor，并显式登记 Vendor=true。网络与 GOMODCACHE 外部读取不开放；缺少依赖是实际失败，不下载、不修改 go.mod、不继承用户 go env。CGO、外部链接器、许可证工具、shell、项目测试自行创建子进程及任意多 package 暂不支持，不提供对应能力准入；若尝试构建且编译实际非零，记录该次失败，不宣称其他编译条件已验证，也不放宽沙箱。

## 实际命令与隔离

同一总超时内顺序执行，成功时共五段：

1. 精确 `bin/go version`，实际完整版本必须匹配。
2. `go test -c` 将明确 package 编译到私有 build/package.test。
3. `go build cmd/test2json` 从同一固定 SDK 源码生成官方 converter。
4. 直接执行生成的测试二进制，`-test.v=test2json -test.count=1`。
5. 直接执行 converter，`-p PACKAGE`，stdin 是第四段真实捕获输出。

编译器 sandbox 只允许固定 bin/go 和该 SDK 的 compile/asm/link/vet 精确 executable；允许可信工具 fork，但不能执行生成的项目二进制。只读冻结代码与 SDK，写入独占 build scratch；HOME/XDG/TMPDIR/cache/modules 全部私有，GOENV=off、GOTOOLCHAIN=local、GOPROXY/GOSUMDB/GOVCS/GOWORK=off、CGO_ENABLED=0、GOTELEMETRY=off。

项目测试另用新 test scratch。不能读编译器 cache/其他生成文件、供应商凭据、父进程私有环境或项目外文件，不能写代码、联网、创建子进程。converter 同样无 fork；它只转换实际 stdout。官方事件与 `-test.v=test2json` 的语义见[Go1.26.3 test2json](https://pkg.go.dev/cmd/test2json@go1.26.3)。

运行中检查原冻结来源、工作副本、私有根、SDK 元数据与精确工具 hash；结束后重新核验完整 SDK 内容和输入 TreeHash。代码/工具/规则变化使旧证据 superseded。

可信编译器使用专用 process group。取消/超时/来源撤销杀掉该 owned group；真实 wait/reap、PID starttime 消失以及 kern.proc.pgrp 查询确认 group 为空共同证明停止。查询错误不算停止。此证明依赖固定可信 SDK 工具不自行脱离 process group；不扩展为任意不可信子进程安全声明。项目测试仍由内核禁止 fork，使用原 owned PID 停止证明。

## 判定和持久消费者

Record 额外保存每段精确 executable/image hash、argv、实际白名单环境、起止时间、退出码、执行/停止/中断/截断状态以及 stdout/stderr hash。getter 深复制；Store 仍先验证私有 released-run 来源，导出的 shape/hash 不构成授权。

五段成功和完整报告才能 passed。真实测试非零 exit 优先于 converter 的零 exit。报告严格要求一个 package start、成对案例事件和唯一最终 package 结果；拒绝重复 JSON 字段、未知字段/动作、外部 package、不完整案例、重复完成、负计数等。零执行/跳过继续服从被冻结的项目规则；报告或 stderr 截断不能通过。

编译失败产生实际 failed receipt，但 `tests_executed=false`、tests=0；Native 协议 succeeded 仍只表示阶段协议终态。handoff 和 Store 消费真实命令位置，不能用编译器启动或模型文字替代测试执行。该失败进入 needs_review 并阻断 review/acceptance。测试通过后仍须受控审查、模型建议和明确人工接受；不自动验收 Task。

当前消费者能读取旧直接命令 receipt。新增 Go receipt 需要此版本严格消费者；旧程序/旧 schema 将安全拒绝。回退时先停派单、确认所有 owned group/PID 停止，对账数据库和产物/执行根同一检查点备份；不能删去 go_toolchain/go_commands 伪装兼容。执行根由可信宿主保留用于重启核验，清理前须确认全部真实进程已停止。

[组件验证与重现](../work-items/WP-21/GO-TOOLCHAIN-01/summary.md)。本组件没有改 UI、HTTP API、SQL、供应商准入或 Jev 设置；WP-21 及全部最终验收仍 in_progress。
