# WP-15-SUBMISSION-JOURNAL-01

状态：Store 基础组件 `done`，恢复 API/Native/UI 消费仍未接入；完整 WP-15/WP-16/实施目标继续 `in_progress`。基线 998f9a27bb04f34dc9d8fdc57febbe2259cbdd0d。合同见 [task-submission-journal.md](../../../contracts/task-submission-journal.md)，没有改变原 Magpie 界面或既有公开 HTTP DTO/路径。

## 行为

可信预览 owner 可在 task POST 前把原 preview/project/key/CreateRequest/expiry/配置版本保存在私有 Store；不创建 Task、不调用 Runtime。每项目一条未解决记录，身份与完整草稿不可改写，终态历史保留。prepared→committed 与原 Task/计划/预算/预设/幂等/事件/提交回执同事务；prepared→abandoned 原子封存，永久拒绝迟到原提交；committed→acknowledged 只确认已证明的任务保存，不是工程验收。

创建和封存并发只能有一个结果：先封存则原创建冲突，先创建则放弃冲突。完整原身份的重复准备/解决只读旧状态，不换 key 或复活历史，不重置计数。已提交完整 peer payload 可绑定同一 Task 并同步记录；已有 HTTP 回执也可由可信 owner 准备为 committed。损坏原冻结记录拒绝读回、解决及继续提交。

schema 7 新增 007 表/索引/约束；001–006、旧 payload/hash/Task 状态及已有 HTTP 回执不变，旧记录不猜造 journal。007/checksum 写入失败整步回滚到 6，校验漂移及未来版本 8 拒绝；原 schema 1–6 fixture 和数据检查保留，终态断言更新到 7，未来拒绝测试用 8。回退旧 binary 须恢复相应一致性备份，不能改 user_version 降级。

## 验证

| 检查 | 最终结果 | 证据 |
| --- | --- | --- |
| Store 全包 race | 103 PASS、1 helper SKIP、32 子测试 PASS、0 FAIL | [日志](submission-journal-verified-final.log) |
| API 全包 race | 105 PASS、68 子测试 PASS、0 FAIL；原 64 样本捕获仍通过 | [日志](submission-journal-verified-final.log) |
| bootstrap 全包 race | 33 PASS、3 显式 SKIP、135 子测试 PASS、0 FAIL | [日志](submission-journal-verified-final.log) |
| Go1.26.3 CLI/GUI/full vet | 各 exit 0 | [结果](build-results.json) |
| graph 刷新 | 14016 nodes / 124673 edges | [结果](graph.json) |

受影响后端合计 241 顶层 PASS、235 子测试 PASS、4 显式 SKIP、0 FAIL，新增 13 顶层 Store 测试。包括准备无 Task、Close/Open、项目/key/字段冲突、终态历史、默认 stamp、注入准备/解决失败、提交全部回滚、确定顺序及 8 轮并发、peer/旧回执、已用调用/返工计数、计划修订、完整记录损坏及 schema 6→7 迁移/回滚/漂移/未来拒绝。已有 Task API/owned HTTP/桥接在 7 上回归，不等于新 journal API 或 UI 已交付。

本项没有新增公开 HTTP/Native 路由，OpenAPI DTO 未变，因此未重新执行 schema checker；API 既有实际样本测试已回归。UI 资源和浏览器交互均未变，本项没有重复浏览器或实际 Native 窗口操作。没有声称完整 Fusion suite、真实账号/模型/额度或硬件断电验收。

## RED 与修正

1. [接口 RED](submission-journal-interface-red.log)：新增 Store 类型和方法尚不存在，编译失败；该结果只证明接口缺失。
2. CRUD/迁移建立后，针对已有 CreateSubmission 的 [行为 RED](submission-journal-behavior-red.log) 证明原请求放弃后仍创建 Task、创建未同步 journal、创建/放弃两方均报告成功。加入事务内原记录检查、同步 committed/task_id 和 SQL 放弃封存保护后，四项初始 [GREEN](submission-journal-green.log) 通过。
3. 首次损坏注入 [日志](submission-journal-corruption-red.log) 与 [首次最终回归](submission-journal-final.log) 被状态转移约束阻止，尚未触及目标创建断言。这是测试 fixture 失败，不能当作生产缺陷证据。改为插入特意损坏 hash 的临时数据库行，不禁用任何生产 trigger。待所有进程终止后，仅临时还原本项自己的上一版“只读索引身份列”检查，观察 [真实 RED](submission-journal-corruption-verified-red.log)：损坏草稿仍成功提交。然后恢复完整草稿/JSON/hash 校验；最终回归通过。原失败内容完整保留，没有放宽断言或移除生产保护。

日志只去每行尾空白，原日志 hash 及保存变换见 [log-provenance.json](log-provenance.json)。源码/证据 manifest 在 artifacts.json，必要的 JSON/本地链接/gofmt/diff/构建 hash 和真实私有 key byte 排除检查记录在 validation.json；不输出 key 或其 hash。主仓库原两个 `.DS_Store` 保留。

## 复现

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-journal-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/store ./internal/fusion/api ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

需要 Go1.26.3、macOS Xcode SDK 与 Python3；run-go/build-dev 使用私有临时 HOME/XDG/环境白名单，不继承日常账号。测试为合成数据，实际外部模型/额度调用 0。

## 未完成

下一项须提供限定管理/来源/项目的恢复 API、精确 Native 通路，再让原 Magpie 工作台在 task POST 前保存原记录、重开读取、核对相同提交和显式解决未知回执。当前页面仍只有进程内原请求，窗口关闭后的恢复尚未完成；不以本组件替代端到端操作验证。真实 Factory/账号/Forwarder/quota、阶段控制、工程闭环、最终 T01–T60 及整分支最终审查继续未完成，Jev off。
