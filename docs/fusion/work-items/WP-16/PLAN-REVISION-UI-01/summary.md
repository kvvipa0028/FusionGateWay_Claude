# WP-16 · PLAN-REVISION-UI-01

在原 Magpie 导航和现有任务详情旁增加默认折叠修订区，复用原三个选择区、五角色模型/effort控件、所有CSS和布局组件。只增加载入/预览/明确应用/同请求重试/结束操作，没有新增导航、配色或框架。基线`4559e56433432fce7f402319822590c80942e249`，操作与恢复边界见[合同](../../../contracts/plan-revision-ui.md)。

冻结计划转换为本地草稿只保留原请求模式、路线、模型和effort选择及caps/锁定例外，不复制credential_identity。载入脏task草稿需明确确认，项目/全局草稿不改。仅实际改变的必需角色进入POST，未改历史绑定不被重新编译；服务器预览核验原角色集合/independence/预算与未改完整绑定，改变目标核验明确选择及当前登记的模型/账号/路线/effort/计费/Runtime/插件/capabilities。继承仍由原服务端解析较低层完整绑定。

原计划强tag、preview_id/hash冻结至应用请求；每次明确应用需审阅确认。未知回执保留原body/tag，锁住选择、项目/范围和新运行；读取Task不能解除未知结果，成功原receipt重读可能返回revision2而当前已为3，必须再读当前版本。明确原服务端拒绝才重新读取与解除。预览/应用不会派单、重置预算或改写已开始角色。

## 证据

- [原始RED](red.log)：缺少转换helper和折叠控件失败；之后[首次GREEN](first-green.log)通过。
- [改变目标RED](changed-binding-red.log)：初版未严格匹配改变目标，伪造路线仍显示预览已核对；增加与明确选择/登记元数据的核验后拒绝。没有通过弱化断言掩盖失败。
- [扩展初轮](expanded-green.log)的一项失败来自把合成UI Runtime的started与已消费供应商call混为一谈：fixture只登记实际受管run但不消费外部call。按fixture明确语义改为断言真实generation1；保持已开始角色拒绝及原预算完整相等，不声称发生真实供应商调用。[修正后的目标测试](target-final.log)。
- [有效mutation](only-changes-mutation.log)只在隔离浏览器上下文把changed-role gate改为所有必需角色，实际请求包含五角色，精确断言失败；生产文件从未改动。最终检查使用无mutation环境。
- HTTP/Store实际断言预览不写事件、明确应用未来review、原design绑定和累计预算保持，原receipt精确重试无新事件，后来revision3不被旧receipt倒退；脏草稿确认/拒绝、修改失效、异常成功回执冻结、明确冲突、已开始design不可改均覆盖。全部是隔离synthetic fixture，真实供应商调用0。

## 复现

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/plan-revision-ui/fixture-build.log test -mod=readonly -tags fusion,nogui -c \
 -o "$PWD/.fusion-dev/task-ui-fixture" ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
 node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs internal/gui/tests/fusion-editor-model.test.cjs
```

截图使用当前生产资产和实际HTTP合成数据，原 Magpie 首页/CSS不变；桌面浅/深色与390宽度检查，不代表实际Wails像素。修订receipt只在当前窗口/服务器进程内，未新增browser持久journal或自动恢复；重开必须读当前计划并明确预览，不能把当前读取当作未提交的证明。

[单阶段角色RED](absent-role-red.log)复现未包含角色仍可编辑，补充仅本任务角色可编辑/选择和跨角色共用禁用；[目标回归](target-absent-final.log)8项通过。非本任务角色不被导入修订。早期脚本stdin遗漏UTF8声明失败，添加文件encoding声明后执行；没有修改产品行为迁就脚本失败。

原底层API/Native/Store/schema/Controller/Scheduler/OpenAPI、供应商准入及Jev off不改。实际Wails修订点击、原其他页面操作、三路线真实准入、工程试点与最终T01–T60继续待完成，父WP-16及整体目标保持in_progress。

## 最终验证

[完整浏览器/模型回归](browser-model-absent-final.log)104/104通过、0跳过/失败；随后只新增[自动候选/明确继承专项](auto-inherit-final.log)1/1通过，没有再改生产代码。合计105个不同测试通过，不把专项日志冒充全105项重跑。最终[Host/原页面资产回归](host-assets-absent-final.log)72项顶层、228项子测试通过，5项显式fixture/真实连接入口跳过，0失败。Go1.26.3 CLI/GUI编译和全仓vet exit0，[构建结果](build-results.json)。[范围检查](scope-check.json)、[测试计数](test-results.json)。

[桌面浅色](desktop-light.png)、[桌面深色](desktop-dark.png)、[390宽深色](mobile-dark.png)来自通过的隔离浏览器，人工视觉核对无裁切或横向溢出；原导航窄屏横向滚动习惯保持原值。源码/文件哈希见[清单](artifacts.json)，日志仅去除行尾空白，原始哈希单独保留。
