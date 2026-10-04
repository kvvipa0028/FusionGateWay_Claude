# WP-16-SUBMISSION-UI-01 · 原请求恢复工作台

状态：页面实现、浏览器/targeted 回归与构建完成；实际 Native 恢复窗口操作未验证。父 WP-16 和完整实施目标继续 in_progress，T01–T60 继续 not_run。

## 改动

- 原 Magpie app.css/app.js/index.html 与锁定上游内容一致，Fusion CSS 未改，只增现有列表/行/按钮组成的原请求区。
- 冻结提交前持久准备原目标、key、完整预览/预算；证明成功才 Task POST。原身份重试，匹配 Task 201 后确认 journal；未知确认只重试确认，不重复提交。
- 开窗发现登记项目的未解决记录，显示原冻结选择、保留编辑锁定，不自动提交/解决。prepared 宿主重启后不复活旧预览；明确封存成功后才允许新预览。
- 异常 metadata、另一个窗口、未知保存/封存、替换 Task ID 均保留原请求，独立原 journal/Task 证明后才释放。Task 201 的待核对 ID 不冒充已证明原任务。
- 沿用原只读分页/Task/Plan/Budget fences；无 browser storage、Runtime/quota 调用或准入扩展。合同见[原请求恢复 UI](../../../contracts/task-submission-ui.md)。

## 真实失败与修复

新增持久化/重开测试先 RED：Task POST 前没有 prepared 记录，新窗口未恢复 prepared/committed 请求。接入准备/恢复/确认/封存后 GREEN。后续替换 Task ID 测试证明页面曾将未验证 201 ID 写成原任务：改为单独缓存待核对回执、保留 journal 独立身份。手动成功核对空记录曾留下失败提示：补成功提示，启动读取仍保留配置载入提示。原断言与权限处理未削弱。

## 验证

最终浏览器 27/27 PASS、0 FAIL/0 SKIP。之后仅测试增加恢复区截图和 390px 无横向溢出断言，该 targeted 场景 1/1 PASS，产品源未变；两张图片已实际查看。[桌面](desktop.png)、[窄窗口](mobile.png)均为 synthetic 浏览器证据，不是 Native 截图。

Go targeted 40 顶层/86 子测试 PASS、0 FAIL/0 SKIP，覆盖 SubmissionJournal/SubmissionReceipt/NativeStage/ControlHostStageUI。Go1.26.3 CLI、GUI 构建及 nogui 全包 vet exit 0。API DTO/schema/Go 源未修改，不重复完整 Fusion/OpenAPI 样本套件。资产测试核对宿主提供的 bundle 与当前源一致；原 Magpie 三文件字节对比及证据 hash 另见 validation.json、artifacts.json。

实际 Native 恢复窗口工具返回 “The Mac is locked”。私有 HTTP 读回证明 committed 原请求未变，owned launcher 正常退出；未验证窗口展示、按钮、真实窗口关闭重开，见 native-observation.json。没有读取日常认证、Keychain 或实际用户 key 内容到输出，也没有真实 provider/额度调用；Jev off。

## 复现

在当前实施工作树，需要已安装 Go1.26.3、Xcode SDK、Python3、Node、Chrome 和 Playwright；不安装或读取日常账号。

```sh
mkdir -p .fusion-dev/implementation
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-ui-fixture-recheck.log \
  test -c -mod=readonly -tags fusion,nogui -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  PATH="$HOME/.local/bin:$PATH" node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/submission-ui-go-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/store ./internal/fusion/api ./internal/fusion/bootstrap \
  -run '^Test(NativeStage|ControlHostStageUI|SubmissionJournal|SubmissionReceipt)'
```

测试私有根/临时 HOME/XDG/profile 由对应 owned 测试清理；Management token 不写日志。准备未持久成功的草稿关闭后不保证恢复，也不发出 Task POST。下一项仍须实际 Native 恢复验证、阶段运行控制、真实 Factory/Forwarder/quota、工程交接及最终验收，不能将当前分项称为整个项目完成。
