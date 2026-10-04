# WP-15-SUBMISSION-RECEIPT-01

状态：组件 `done`；父 WP-15/WP-16 和完整实施目标继续 `in_progress`。基线 c2c58534735708b8223fd71a2da457b7f25f231e。本项只修改任务提交回执存储/API，UI 沿用原 Magpie，界面资源未改动。合同见 [task-submission-receipt.md](../../../contracts/task-submission-receipt.md)。

## 改动与验收

原 POST 的 preview_id/plan_hash/key 完整身份与 Task、原项目/创建 payload 在 schema 6 持久关联；写入和任务/计划/预算/预设/幂等/created event 原子提交。回执插入匹配原记录，禁止改写/删除；同 key 保持项目作用域。仅已提交身份可跨预览真实 expiry pruning、Server 重建、Store Close/Open 和 owned 宿主重启回读同一 Task 的当前状态。错误身份/未提交丢失预览不能创建第二个任务，未登记项目不返回内容。新任务仍受当前 stamp/配置/过期限制。

保留跨 Server 同项目/key/完整 payload 的已有幂等语义：另一 genuine 进程内预览可通过 existing-only 事务附加到已保存任务，不能创建新任务。回读不重新评估新建默认层，不执行 Runtime/额度查询，不改历史、预算及已用计数。提交处理后重查 Management context；等待期间撤销或取消请求，不公开 Task 或错误详情。此时任务可能已保存，仍应保留原请求等待有效认证后核对。

006 迁移保留 schema 1–5 fixture/历史数据与 001–005 原文件、checksum 和 payload hash；不为旧任务猜造 preview_id。006 metadata 注入失败整步回滚，可重试；当前 6 校验漂移及未来 7 拒绝。终态版本断言 5→6、未来拒绝 6→7，其余历史数据断言保留。回退旧二进制须恢复其一致性备份，不能改版本号降级。

## 实际验证

| 检查 | 最终结果 | 证据 |
| --- | --- | --- |
| Store 全包 race | 90 PASS、1 helper SKIP、26 子测试 PASS、0 FAIL | [日志](submission-receipt-store-host-final.log) |
| API 全包 race | 105 PASS、68 子测试 PASS、0 FAIL | [日志](submission-receipt-api-authority-final.log) |
| bootstrap 全包 race | 33 PASS、3 显式 SKIP、135 子测试 PASS、0 FAIL | [日志](submission-receipt-store-host-final.log) |
| 原浏览器流程 | 15 PASS、0 FAIL，当前 CLI/test-only 宿主 | [日志](submission-receipt-browser-final.log) |
| OpenAPI 离线校验 | 29 路径、33 操作、64 实际样本、9 输入/5 项目/7 任务页反例；production=false | [结果](submission-receipt-openapi-final.json)、[样本](submission-receipt-samples.json) |
| Go1.26.3 CLI/GUI/full vet | 三项 exit 0 | [结果](build-results.json) |
| graph 刷新 | 13986 nodes / 124279 edges | [结果](graph.json) |

合计受影响后端包 228 顶层 PASS、229 子测试 PASS、4 显式 SKIP、0 FAIL；没有声称完整 Fusion 套件重跑。新增 10 顶层测试覆盖持久恢复、原子失败、项目/key/payload 冲突、并发、后续计划/状态与已用计数、迁移保留/失败以及权限撤销。Store helper 和 bootstrap Native/helper/synthetic fixture 的跳过范围均见原日志，不作为实际 Native 模型验证。

owned 实际 loopback 宿主完成原提交、Close、重开同私有 Store/新管理认证，再经固定 Native 桥读回同一任务；HTTP 和 Native 均拒绝未登记项目，Source revision 撤销返回 503。Inspect/Resolve 0 次，只有 created 事件，预算已用为 0。该桥测试是实际 HTTP+函数通路；本项没有打开实际 Native 窗口或导出新截图。没有新增 SIGKILL/硬件断电测试。

## 失败与修正

1. [原恢复 RED](submission-receipt-red.log)：已提交预览真实到期清理后返回 409；Store 重开无原 receipt，未登记检查也只能返回 409。实现持久身份后初步恢复 [GREEN](submission-receipt-green.log)。
2. [首次回归](submission-receipt-regression.log)：migration fixture 将 PresetInput 传给 Layer 参数造成编译失败；修为 `.Layer`。[首次 vet](build-first-vet.log) 同原因失败，CLI/GUI 已编译。原 restart 测试要求已提交预览返回 409，与新持久回执需求冲突；改为精确 201/同 Task，同时新增原未提交预览仍 409，保留 fresh preview 同 key 不重复创建和事件断言。此处是有依据的合同更新，非按实现放宽断言。
3. 新增实际新 Server 合同样本时漏 policy import 的 [编译日志](submission-receipt-api-final.log) 保留；补 import 后最终 API/capture 通过。
4. [权限 RED](submission-receipt-auth-red.log)：认证通过后等待回读，Management 被撤销/请求取消仍返回 201 及 Task。新增响应前 authority fence 后两个场景返回 401、不含 ID/goal，最终 API 回归通过。
5. bundled Python 缺 PyYAML，未安装新依赖；现有系统 Python 已具备 yaml/jsonschema/referencing，实际执行离线 checker 并得到上述最终结果。日志只去掉每行尾空白，保留完整失败内容；原文件 hash 和保存变换见 [log-provenance.json](log-provenance.json)。

## 复现

当前实施工作树中执行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-receipt-recheck-store-host.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/store ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-receipt-recheck-api.log \
  test -race -v -count=1 -timeout=3m -mod=readonly -tags fusion,nogui ./internal/fusion/api \
  -args -fusion-api-contract-out="$PWD/.fusion-dev/implementation/submission-receipt-recheck-samples.json"
python3 scripts/fusion/check-openapi.py \
  --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json \
  --samples .fusion-dev/implementation/submission-receipt-recheck-samples.json
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-receipt-recheck-fixture.log \
  test -c -mod=readonly -tags fusion,nogui -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  PATH="$HOME/.local/bin:$PATH" node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
```

测试为私有临时 HOME/XDG、固定合成路线和独立 Chrome profile，真实模型/额度调用为 0；自己启动的宿主和浏览器在测试结束后关闭。来源、管理 token 和 GLM key 不进入 Git。源/证据 hash 与格式/链接/私有 key byte 排除检查见 artifacts.json；主仓库原两个 `.DS_Store` 保留。

## 未完成

窗口关闭/重载后的未确认 body/key 保存与恢复仍未实现，本项不将服务端持久回执冒充完整 UI 恢复。真实供应商 Factory/账号/Forwarder/quota、阶段运行控制、五阶段工程闭环及最终 T01–T60 Gate 仍待完成；Jev off。源码未改变原 Magpie UI，本项不涉及发布或合入主分支。
