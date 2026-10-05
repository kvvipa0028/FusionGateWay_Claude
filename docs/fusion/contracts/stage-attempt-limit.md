# 阶段执行次数上限

WP-22-ATTEMPT-LIMIT-01 落实首版“同阶段最多首次加一次补救”的硬上限。每个 task/role 最多创建两个持久启动 intent；修改 plan revision、重启宿主、换请求 key 或恢复 checkpoint 均不能重置次数。计数来自原 stage_runs.attempt，不新增数据库 schema，也不改写已有执行记录。

## 执行行为

- 已提交的相同 key/请求继续读取原 receipt，Created=false，不占用新次数、不再准备或启动 Runtime。
- 新请求必须通过当前管理权限、完整 Task 条件、冻结角色及原 workflow 顺序检查。Controller 在选择目标和调用 Resolver 前检查次数；实际 intent 事务再次检查，防止并发绕过。
- 已使用两次且当前允许请求该阶段时，拒绝第三次，原子保存 needs_review、generation 加一和 stage_attempt_limit 事件。事件引用原阶段最近的 run；没有第三个 run、reservation 或启动 key 映射。API 沿用 HTTP 409 / workflow_requires_review。
- 同一 task 的其他角色使用各自次数，调用和返工预算仍按整个 task 累计。本限制不退还或消耗额外调用，不自动 ReserveRework，不自动启动后续阶段。
- 权限在等待锁期间或提交前撤销、旧版本/状态、非法角色/输入及事件写失败均回滚；重复第三次请求不会再次改变 generation。未释放 reservation 继续要求停止对账，本限制不能释放进程、writer 或容量。超限状态写入遇到 generation 整数上界时拒绝，避免溢出。
- 恢复请求也服从上限：次数已耗尽时先停止派单；checkpoint 引用不能授权第三次启动。未耗尽时继续由原可信 checkpoint reader 检查来源、seal、实际停止和恢复能力。

## 验证与回退

[组件证据](../work-items/WP-22/ATTEMPT-LIMIT-01/summary.md)包含三个 Store 入口的失败到通过、实际管理 Handler、Controller 启动/等待/释放、事务回滚/权限撤销/并发/计划修订/重启/恢复与溢出边界。Store/Controller 测试使用合成 Runtime 与停止记录；固定 Claude Code Native 回归另列，不宣称真实供应商准入已完成。

复现：

```sh
mkdir -p .fusion-dev/attempt-limit
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/attempt-limit/targeted.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=120s -p=2 \
 -run 'Test(StageAttemptLimit|ControllerStageAttemptLimit)' -v \
 ./internal/fusion/store ./internal/fusion/control ./internal/fusion/api
```

需要 Go 1.26.3、macOS 工具链及已缓存依赖；测试使用临时私有 HOME/XDG，不读取账号。回退代码无需 schema 迁移；已保存的 needs_review 和历史 intent 必须保留，不能删记录、改次数或恢复 ready 来绕过限制。

## 当前边界

这里交付次数上限，不等于自动返工闭环已完成。实际 review 问题交回 implementation、复测/复查/验收的有限调度及项目独立性仍须完成。原 workflow 顺序不放宽，达到上限也不绕过硬测试或人工接受。Magpie 主界面、CSS、组件和导航未修改；原主导航接入仍按最小扩展要求另行完成。Jev 保持 off。
