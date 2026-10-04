# WP-15-TASK-INDEX-01：工作台所需的只读任务分页

基线 `92ee311db61982e0c5747bd52faa93ff33868470`。本组件完成；父 WP-15/WP-16 继续 in_progress，任务工作台消费者、生产 Factory/真实账号与额度准入、工程闭环和最终 T01–T60 仍待完成。合同见 [task-index-api.md](../../../contracts/task-index-api.md)。Jev off。

## 实现及限制

Store 新增 ProjectTasks，按既有 SQLite 入库顺序倒序，固定32项与额外一行判断后续页。后续游标为本页末尾的同项目既有 taskID，不暴露 rowid、不接受 offset/query/任意页大小；新的首屏任务不挤动旧页。排序和边界在现有 Store.mu/单连接下读取，没有迁移或改写 tasks 表。

Management Handler 新增项目 tasks GET 与 tasks/before/{task_id} GET，要求可信项目登记，保持 path/query/方法/auth 保护，等待读取后再次核验 ManagementCurrent。无部分错误页、无模型/额度读取和执行。DTO 只含原Task六字段、goal_truncated 和 etag；goal最多512 Unicode code points，完整目标仍从现有 Task GET读取。ETag来自同一行的计划/generation/state。列表是当前发现视图，后续状态改变仍需详情/控制 If-Match，不承诺多页冻结快照或SQL扫描成本恒定。

NativeStageBridge 只增加已登记项目的两个精确 GET 通路，未开放提交/执行/取消或任意网络代理。阶段页面没有改动，本项没有新的实际 CUA 窗口测试或截图；桥函数通过实际私有HTTP读取，不能将它称为界面工作台已完成。

## 验证与实际失败

- API 缺失路径先实际返回404，Store方法缺失先编译失败，Native桥未开放路径先实际返回404；原始失败日志保留。第一次实现引用了不存在的错误名，编译失败后改用现有 control.ErrForbidden，未改变鉴权语义。
- Store验证67项与交错外项目、32/32/3页、期间新建任务、关闭重开后继续游标、最末空页、精确32项无伪后续、当前计划版本、外项目/不存在游标拒绝、非法输入、取消与关闭Store。
- API验证有限Unicode摘要、完整Task保持、精确八字段/Task ETag、项目隔离、当前暂停状态、资源名同名项目、空数组、非法路径/query/方法、Management/stage/cross-site、关闭Store，以及阻塞登记读期间撤销/取消。
- 真实本机 ControlHost 经33个独立 preview/submit创建任务，读取32项首屏与Native桥旧页，检查所有任务仅有created事件、Inspect/Resolve计数0，来源撤销后HTTP与桥均拒绝。路线/账号/准入都是独立fixture，未导入真实凭据。

API包最终102 PASS/63子PASS/0FAIL；Store包84 PASS/22子PASS/0FAIL/1SKIP；bootstrap最终31 PASS/123子PASS/0FAIL/2SKIP。Store SKIP是仅子进程调用的TestStoreCrashHelper；bootstrap两项固定Native参数测试未重复执行，本项未改变执行协议或Adapter。全量Fusion套件不重复，最终Gate保持not_run。

回归第一次把API专用导出flag传入多个包，API完整运行且exit0，Store/bootstrap因未知flag未运行，命令整体exit1；保留日志，不计其他包通过。随后Store包通过，bootstrap新增HTTP测试因误将同一已提交preview换key重用而收到409；现有合同要求一个preview对应一个receipt，因此只修正fixture为每个任务独立preview，保留拒绝保护。修正后的单项和完整bootstrap包均通过。API与Store已经通过且源码未变，不重复其检查；test-results逐包标明日志与命令边界。

OpenAPI为29路径/33操作，63份实际Handler样本覆盖全部操作；保留9个输入和5个项目响应反例，新增7个任务响应反例。标准schema/refs/组件及样本离线校验通过。Go1.26.3 CLI/GUI build、全仓tagged vet均exit0；graph更新至13955节点/123836边。实际模型调用、真实额度查询均0。

归档日志仅在需要时去除行末空白/补末尾换行，并将 flag usage 的行首混合缩进转换为等价空格，原始文件保留在.fusion-dev/implementation或build-dev，原始hash与转换记录于test-results。源文件与packet hash绑定本工作项提交，历史组件证据不改写。

## 复现

在工作树根目录执行；helper使用临时HOME/XDG、环境白名单和已有公共缓存，不继承日常认证：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-task-index-api.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/api -args -fusion-api-contract-out="$PWD/.fusion-dev/implementation/task-index-recheck-samples.json"
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-task-index-store-host.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/store ./internal/fusion/bootstrap
python3 scripts/fusion/check-openapi.py --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json --samples .fusion-dev/implementation/task-index-recheck-samples.json
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

API导出flag只能传给API包，不能附在多个包命令后。失败保留具体日志，不删除断言、不将未知额度或连接成功填成真实准入。
