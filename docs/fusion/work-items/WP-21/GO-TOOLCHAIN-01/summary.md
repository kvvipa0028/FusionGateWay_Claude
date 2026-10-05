# WP-21 · GO-TOOLCHAIN-01

固定 Go1.26.3 的离线单 package 真实编译/测试 backend 已接入 evidence、handoff、Store 和 GLM testing。可信编译器可以调用固定 SDK 工具，编译出的项目测试仍禁止 fork、网络和代码写入；沿用原只读来源和 owned receipt，不改 UI。基线为 `4d36622b69793b699ddab2e368c85fc79bbec8bc`。

新增可选 GoToolchain 及实际 GoCommands：成功记录版本、编译测试、编译官方 test2json、运行测试、转换真实 stdout 五段。SDK 完整内容冻结、私有环境、每段 argv/image/log hash 与停止证明均保留。编译失败记录 failed、tests_executed=false、tests=0，不能继续 review/acceptance；不把模型“通过”或 converter 零退出替代实际测试退出。

首次仅支持明确 package、标准库或随产物冻结的 vendor。CGO、在线模块、项目测试的子进程、任意多 package 和其他工具链仍未准入。可信 compiler group 的停止证明依赖固定 SDK 不脱离 group，不能用于任意不可信子进程。详细输入、隔离、兼容性和回退见[合同](../../../contracts/go-verification-backend.md)。旧 nil Go Spec/Record 保持 omitempty，SQL001–012、HTTP、模型权限、Magpie 原 UI 与 Jev off 均未改变。

## 验证

| 检查 | 结果与边界 |
| --- | --- |
| 原直接命令 backend 的真实 RED | [go-red.log](go-red.log)；Go 编译需要子进程，原路径不能通过。首次重复使用原 Spec 的失败保留为 [go-compile.log](go-compile.log)，不冒充新 backend 成功 |
| 新 backend 初次真实标准库构建 | [go-native.log](go-native.log)通过；后续失败/零执行/跳过/编译/缺依赖、内核边界与 owned 持久回读分别通过，见 [go-target.log](go-target.log)、[go-final-target.log](go-final-target.log) |
| 实际冻结 vendor / 取消 / 原来源修改 / 输出截断 | [go-extra.log](go-extra.log)；真实 vendor 代码被运行，两个实际测试 PID 已停止，截断不能 passed，SDK symlink/可写/内容漂移被拒绝 |
| 可信编译器真实子进程停止 | [child-stop-green.log](child-stop-green.log)；取消/撤销后 wait/reap 和 group 为空，真实 child 身份消失。初次 fixture 参数错误未启动 child，保留 [child-stop.log](child-stop.log)；修正 fixture，不放宽读取权限 |
| 有效保护 mutation | 临时只为生成的项目测试允许 fork，真实边界测试检测到 `untrusted fork` 并失败；[fork-mutation.log](fork-mutation.log)。随后精确恢复生产文件原字节，完整回归通过 |
| 完整 Fusion race 回归 | [fusion-final.log](fusion-final.log)：19 个包、741 顶层通过 / 33 明确跳过、1320 子用例通过，0 失败。跳过项仍需独立 Native 参数/真实条件，不算最终 Gate |
| 固定 Claude Code 2.1.287 实际 Native 回归 | [native-final.log](native-final.log)：10 顶层、67 子用例通过，0 跳过/失败；其中交接流水线35场景，包括 Go 成功/编译失败、重启、返工、审查、模型建议和明确人工决定。上游是合成服务，不是供应商准入 |
| Go 1.26.3 CLI/GUI/vet | 三项 exit0；[build-results.json](build-results.json)及三个构建日志 |
| 导出 schema | [schema-check.json](schema-check.json)：当前真实 Go Record、先前实际直接命令 Record 通过，12 个负向形状被拒绝。时间格式显式校验，不依赖可选 RFC3339 library |
| 精确变更与凭据排除 | 源/包 hash、UTF-8/JSON、gofmt、local links、diff 与主工作树保护已核对。私有真实 key 仅用于静默字节排除检查，不进入测试命令、日志、源码或提交 |

全量首轮误将 evidence 包的导出 flag 传给其他包，导致19包参数错误；[fusion-regression.log](fusion-regression.log)保留。去掉该参数后真正全量重跑通过，没有改测试或产品保护以掩盖失败。schema 脚本初次发现当前环境不会自动检查 date-time，补了显式 UTC/calendar 校验，非法时间负向断言已通过。

当前实际 Go 测试的[完整 Record](actual-go-record.json)含私有临时执行路径的元数据，没有认证信息。它仅供复现 schema 与精确阶段记录；任何 JSON、hash 或模型意见均不能创建 owned Result、私有 Store origin 或执行 grant。[原 scratch 日志 SHA256](raw-log-hashes.json)保留；归档只去除行尾空白，并将失败命令帮助文本的缩进 tab 展开为空格；不删除失败或改写结果。

## 重现

在 implementation 工作树运行。需要本机 SDK `/Users/zhaojianzhi/.local/share/go/1.26.3`、macOS sandbox-exec 与编译工具；Native 检查另需固定 Claude Code 2.1.287，合成配置不传真实 key。先创建日志目录：

```sh
mkdir -p .fusion-dev/go-toolchain
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/go-toolchain/fusion-final.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=360s -p=2 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/go-toolchain/native-final.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=360s -p=1 -run '^TestGLM(Factory|StageWritePaths)' \
 -v ./internal/fusion/bootstrap \
 -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/go-toolchain/capture.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=90s -p=1 -run '^TestGoRunnerActuallyBuildsAndRunsCurrentArtifact$' \
 -v ./internal/fusion/evidence \
 -fusion-go-evidence-record "$PWD/.fusion-dev/go-toolchain/actual-go-record.json"
python3 docs/fusion/work-items/WP-21/GO-TOOLCHAIN-01/check-schema.py \
 .fusion-dev/go-toolchain/actual-go-record.json
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

默认全部 Fusion suite 有条件跳过，所以固定 Native 回归必须单独执行。记录导出参数仅能传给 evidence 包。归档数据不用于真实任务；执行根的清理/回退遵循合同中的真实停止和同检查点恢复规则。

codebase graph 已刷新（14694 nodes / 132746 edges）；不提交生成索引。WP-21 及整个目标继续 in_progress，原各页数据与操作、真实供应商/工程、实际 Wails 窗口及最终60类验收仍待完成。当前 GUI 构建通过不代表新增窗口操作通过。
