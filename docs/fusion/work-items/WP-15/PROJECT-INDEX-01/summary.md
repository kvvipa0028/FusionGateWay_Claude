# WP-15-PROJECT-INDEX-01：界面所需的可信项目列表

基线 `a137bd57756029eb41883086d959119d9cfff072`。本子项完成；WP-15 继续 in_progress，WP-16 界面仍待实施，最终 T01–T60 仍 not_run。合同见 [project-index-api.md](../../../contracts/project-index-api.md)。

## 实现

新增 Management 保护的 GET `/control/v1/projects`，返回最多128个可信服务端登记的项目 ID 与当前 configuration_revision，按 ID 排序；空登记为 `projects:[]`。不包含项目目录、模型、账号、credential identity、预算或准入标志，不导入旧客户端配置，不新增登记或执行任务。

沿已有 currentProjectLocked 重读保存的默认层 stamp，使列表版本与单项目配置读取一致。读操作可能更新服务内已变化的配置投影；不写 Store、创建事件或改动已提交任务 Snapshot。相同默认层无变化时连续 GET 不增加 revision。配置版本不是运行授权或提交条件，界面选择项目后仍须 GET configuration/defaults，并使用各自资源的 If-Match 与服务端 preview。

读取受同一 Server.mu 与已有128项注册容量约束；store错误不返回部分列表，读取后重新检查 ManagementCurrent。GET 不采集额度、调用 Native 或刷新准入。非 GET 返回405/Allow:GET，query沿既有入口返回400，无 Management/错误为401，stage/cross-site为403，issuer撤销或请求取消保持拒绝。草稿宿主来源变化仍为503。

## 测试与证据

新测试先实际 RED：接口未实现时404，4个顶层/7个子场景失败。实现后 GREEN；随后增加等待登记锁时撤销 issuer 或 request context 的两个场景，最终均拒绝且不泄露项目。共新增5个 API顶层测试和15个子场景，另新增1个真实本机HTTP宿主测试。

覆盖稳定排序、精确两字段与私有元数据排除、草稿不升级准入、空数组、128容量、当前默认层与已冻结任务保持、关闭Store失败无部分结果、缺失/错误/stage/cross-site/撤销鉴权、方法/query拒绝、并发登记读取，以及middleware鉴权后被阻塞再撤销。真实loopback进一步验证默认层写后版本递增、POST不开放注册、私有来源撤销拒绝；没有模型或真实额度调用。

最终 API race：97顶层 PASS、47子测试 PASS、0 FAIL/SKIP。bootstrap race：19顶层 PASS、2顶层 SKIP、75子测试 PASS、0 FAIL。SKIP为显式Native fixtures；本项没有改动Runtime或执行路径，不重复此前已通过的Native/完整Fusion套件。

OpenAPI同步为27 paths/31 operations；56份实际Handler样本覆盖全部31操作，原9项越权DTO schema拒绝保留，另5项列表响应反例拒绝null、0版本、私有路径、准入附加字段和129项数组。标准schema/全部ref/组件和样本离线校验通过。

Go1.26.3 CLI/GUI构建及全仓tagged vet均exit0；graph刷新到13900 nodes/123120 edges。最终运行代码检查完成后只有文档/packet变更；源文件与packet hash绑定本提交，历史证据不改写。

## 复现

从工作树根目录执行，helper使用临时HOME/XDG和环境白名单：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-project-index-api.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/api -fusion-api-contract-out "$PWD/.fusion-dev/implementation/project-index-samples.json"
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py /tmp/fusion-project-index-host.log test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui ./internal/fusion/bootstrap
python3 scripts/fusion/check-openapi.py --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json --samples .fusion-dev/implementation/project-index-samples.json
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

本项是阶段界面所需的真实后端发现接口；不代表UI页面、浏览器/Native GUI鉴权、真实三路线Factory、GLM账号/物理pool/生成准入已完成。Jev off，真实key未使用或导入。本轮GLM准入合同核对确认QuotaReader仍Complete=false/Pool.Verified=false，保留未准入；不为了推进界面填入测试成功标志。
