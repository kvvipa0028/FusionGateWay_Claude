# WP-22-ATTEMPT-LIMIT-01

已落实每个 task/role 最多两个持久启动 intent。第三次符合当前条件的启动在 Runtime resolver 前被拒绝，原子保存 needs_review 和 generation fence，保留原 run、停止预留、产物与预算。修改计划、重启、恢复 checkpoint 或换 key 不重置次数；原 key 仍只读取原 receipt。实际 intent 事务重复检查。详见[合同](../../../contracts/stage-attempt-limit.md)。

## 验证

| 验证 | 结果 | 证据 |
| --- | --- | --- |
| 修改前三个 Store 入口 | 真实第三次 intent 均被创建，测试失败 | [RED](store-red.log) |
| 新次数/权限/CAS/重启/恢复/溢出/回滚/并发/Handler/Controller | 9 个顶层与 5 个子用例 PASS，3 packages；无 FAIL/SKIP | [最终定向](targeted-final.log) |
| 阈值临时改为 3 | 1 个顶层与三个入口子用例 FAIL，恢复原源码后通过 | [有效 mutation](mutation.log) |
| Fusion 回归 | 711 个顶层、1294 个子用例 PASS；33 个顶层和 11 个子用例 SKIP，19 packages，无 FAIL | [全量日志](full-fusion.log) |
| 固定 Native 回归 | 10 个顶层、57 个子用例 PASS，25 种父产物流水线场景，无 FAIL/SKIP | [Native 日志](native.log) |
| 追加实际管理 Handler 用例 | 两个用例 PASS；第三次 409、任务 needs_review、原 key 重读 200，Runtime 总启动仍为两次 | [API 日志](api.log) |
| Go 1.26.3 CLI / GUI / 全量 vet | 三项 exit 0 | [构建结果](build-results.json) |

Fusion 全量运行后仅追加一个 API 测试，源实现没有再改变；追加测试包含在 API 与最终定向运行中。不同运行的重复用例不相加声称新的完整套件总数。Store/Controller/Handler 的 Runtime/StopProof 是合成 fixture；Native 回归实际执行固定 Claude Code 2.1.287、owned supervisor 和合成供应商上游，不等于真实账号、模型计费或准入已通过。

权限失效在写入前和提交前均重查，撤销回滚状态和事件。三个入口保留完整历史，第三次没有 run/reservation/key；同 key/旧条件读取已有 intent，不重新启动。事件故障回滚后重试，十二并发请求只有一个 needs_review fence；未释放进程继续要求对账，不由次数限制释放。

## 范围与回退

本轮 UI 文件零修改；原 Magpie index.html/app.js/i18n.js 和六个原 CSS 与固定上游源码一致，[边界检查](boundaries.json)另记录原 schema11/SQL001–011 不变、graph 刷新和 Jev off。未重新运行浏览器或声称 Wails 窗口已验证。保留主工作区两个已有 .DS_Store。没有读取/导入真实认证用于测试，也没有调用真实供应商或 Jev。

回退不需要 schema 迁移；保留 needs_review 与原历史记录，不能删除次数或恢复 ready 绕过限制。实际自动返工链、项目独立性、完整原主导航接入、子进程工具链、真实 Gate A/Wails 操作及最终 T01–T60 仍须完成，WP-22 与整体目标继续 in_progress。

## 复现

定向命令见[合同](../../../contracts/stage-attempt-limit.md)。回归命令：

```sh
mkdir -p .fusion-dev/attempt-limit
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/attempt-limit/full-fusion.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/attempt-limit/native.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=240s -p=1 -run '^TestGLM(Factory|StageWritePaths)' \
 -v ./internal/fusion/bootstrap -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

先确认本机固定 Native 可执行文件仍匹配原准入版本；不要换路径或版本后仍宣称原证据有效。公开日志仅清理行末空白，原始私有日志与其 hash 保存于本任务 scratch/[结果](test-results.json)。
