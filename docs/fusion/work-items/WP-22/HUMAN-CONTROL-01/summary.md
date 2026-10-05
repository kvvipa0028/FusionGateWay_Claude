# WP-22-HUMAN-CONTROL-01 · 人工验收管理接口与现有面板

BASE `b5085e2cdfb095fae541c8ee75eac5e40b7b0df0`。本组件已接入可信当前证据读者、Management GET/POST、限定 Native bridge 和现有 Fusion 工作流面板的人工接受/退回控件。原 Magpie 主界面三个资产与基线逐字节相同，全部 CSS 零修改；只修改补充页面的 HTML/既有 workbench，并扩展既有浏览器测试。WP-22 仍 in_progress，真实供应商调用0，Jev off。

## 行为和兼容性

新增 `/control/v1/tasks/{task_id}/workflow/decision`。GET 分别返回当前真实硬证据、逐项模型意见与历史人工决定；无最终阶段/独立读者时不虚构 report。POST 要求完整 Task If-Match、精确六项产物身份和明确 action/reason，不收客户端验证标准、source/root、passed 或角色。当前配置从可信 Factory 读取；JSON/hash 不能创建 Store owned 权限。旧资源和 SQL001–011 不变，新增 endpoint 不允许替代人工决定来源。

管理权限在等待锁后、事务前/提交前及响应前核对；当前 Runtime、路线/账号/私有凭据身份、三份实际产物也重复核对。标准失效禁止 accept；return 保留原因且不启动返工。明确接受后 Task completed，模型 accepted 本身仍只是 advisory_only。重复原决定不增加事件/调用/预算，不同原因或动作冲突。响应丢失或提交后外部变化不能自动新建决定；必须手动回读当前状态。

Native bridge 仅扩展当前绑定窗口和登记项目的精确路径，保留同源、项目范围、私有管理凭据和响应后二次校验。页面与模型拿不到管理秘密。

原有“工作流与设计检查点”折叠面板增加“读取验收证据”、原因和明确确认的“接受当前交付 / 退回当前交付”。沿用 Magpie list/row/profiles/text/action/primary 类名、样式变量和交互；模型文本采用 textContent。旧证据、任务/条件/标准不匹配或响应丢失停用操作；切换任务清空旧证据，迟到响应不会覆盖新任务。

合同：[human-acceptance.md](../../../contracts/human-acceptance.md)。真实路径：[openapi-fusion.yaml](../../../openapi-fusion.yaml)，41 paths / 49 operations，仍 production_registered=false。

## 验证

- Go1.26.3 完整 Fusion race：703顶层、1289子测试 PASS；33顶层、11子测试 SKIP，19 packages，0 FAIL。未指定 Native executable 的 skip 不算 Native 验证。
- 固定实际 Claude Code 2.1.287 Factory：10顶层、57子测试 PASS，0 SKIP/FAIL，含25种链场景。实际设计→人工批准→实施→真实测试→只读审查→模型验收后，经真正 Native bridge 请求接受/退回、重启回读、精确重试、旧条件/错hash/非法窗口/未知字段拒绝。标准改变可退回但不能接受；读者响应前失效与提交中管理撤销不能持久接受。上游、key 和准入均为合成 fixture，不是供应商账号测试或真实用户点击。
- 管理 API targeted：3顶层、24子测试 PASS。draft/null reader、精确字段、null/大小写/重复字段/额外权限字段、Worker credential、Idempotency-Key、无/过期条件和等待后的授权撤销/取消均不产生决定或事件。
- 实际 Chromium 77/77 浏览器回归 PASS。新增6项覆盖无证据、手动取消确认、完整身份/原因、纯文本展示、过期证据退回、坏/跨任务响应、丢失/旧条件/授权失败响应、切换任务后的迟到证据。正向展示与决定使用明确的 presentation fixture；真实 owned 后端由上一项 Native 链独立验证。桌面与390px截图已检查，无横向溢出；不宣称 Wails 像素或真实人点击已验证。
- CLI/GUI build 与全仓 go vet exit0，私有 HOME/XDG/环境白名单。CLI 原内嵌页面导致初次浏览器67PASS/10FAIL，重新构建后77PASS；保留失败记录，没有删断言或改预期。
- 实际 Handler/Native bridge 124 samples 与离线官方 OpenAPI3.1 schema 检查：49 operations 全覆盖，新增8类响应/4类请求反例拒绝。时间校验显式支持 Go 纳秒、UTC receipt 和 Python3.9，不依赖缺失的可选 RFC3339 包；JSON schema 不证明执行来源或权限。

缺少 endpoint 的实际 Native400 RED、缺少控件的浏览器 RED 均保留。有效 mutation 删除事务管理 current 检查后，最终撤销仍持久写人工决定，测试实际 FAIL；源文件精确恢复后完整回归 PASS。初次 fixture/helper 编译错误及旧内嵌页面、时间校验兼容问题均按真实接口/源码修复，没有削弱门或断言。

证据：[test-results.json](test-results.json)、[human-openapi-final.json](human-openapi-final.json)、[build-results.json](build-results.json)、[human-scope-check.json](human-scope-check.json)、[artifacts.json](artifacts.json)。截图：[desktop.png](desktop.png)、[mobile.png](mobile.png)。

## 复现

在 implementation 仓库根执行，Go1.26.3 / Python PyYAML+jsonschema / Node playwright 已安装；不传真实 key：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/implementation/human-fusion-final.log test -mod=readonly \
 -tags fusion,nogui -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/...
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/implementation/human-native-final.log test -mod=readonly \
 -tags fusion,nogui -race -count=1 -timeout=240s -p=1 -run '^TestGLM(Factory|StageWritePaths)' \
 -v ./internal/fusion/bootstrap -fusion-host-native-claude /Users/zhaojianzhi/.local/share/claude/versions/2.1.287
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/implementation/human-ui-fixture-build.log test -mod=readonly -tags fusion,nogui \
 -c -o "$PWD/.fusion-dev/task-ui-fixture" ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" \
 NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
 node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
python3 scripts/fusion/check-openapi.py --contract docs/fusion/openapi-fusion.yaml \
 --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json \
 --samples docs/fusion/work-items/WP-22/HUMAN-CONTROL-01/human-handler-samples.json
```

运行日志保存到本任务私有 scratch；不覆盖历史已提交证据。CLI/fixture 必须先重新构建，否则测试会拒绝旧内嵌页面。测试临时宿主/目录自行停止和清理，不改变真实注册或日常客户端。

## 仍须完成

原 Magpie 完整主导航与其他页面的受控接入、有限返工/次数约束/独立性策略、需要子进程的工具链、真实 Gate A/账号/个人项目及最终 T01–T60。当前补充页面不等于原主界面已全面整合；实际 Wails 窗口保存/退出仍须独立验证。不将本组件通过描述为整个工作包、完整工程闭环或最终目标完成。
