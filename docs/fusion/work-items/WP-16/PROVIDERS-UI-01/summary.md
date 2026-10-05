# WP-16 · PROVIDERS-UI-01

复用 Magpie 原 Providers 页面及 modal，接入当前项目登记的模型与账号详情，并支持明确选择单个阶段草稿。原页头、鸟形标识、八个导航、布局类、深浅色和六份 CSS 全部保留；没有重做界面或引入 UI 框架。

基线 `eacf29227715c8ef10c103f8c7086bb729e5d0a1`；固定上游 `1a50db1a8afd0849df2853f92a47da9d5e2f2cc9`。从原 index.html 提取整个 Providers main 与 modal，原 Add provider 只增加 disabled。模板锚点缺失或重复即拒绝生成，未开放原旧 API。详情框通过固定字段和 textContent 显示模型、账号、路线、Runtime、计费/准入/锁定核验和 effort，不显示 credential_identity。

“应用到阶段草稿”只修改当前范围所选角色，保留其他角色，清除旧 effort 并要求重新确认。保存状态未确认、执行操作进行中或来源/授权无效时不允许应用；不自动保存、调用供应商或启动任务。重新载入沿用原未保存草稿确认。原账号新增和登录仍未开放，本组件不等于原 Providers 全功能或整个 WP-16 完成。

## 兼容性

现有配置 GET 响应新增 `project_id`，用于核对当前项目，同时严格验证 revision/ETag/路线字段。服务器、OpenAPI 和页面同步更新；旧严格 DTO/缓存样本需更新，新页面拒绝旧宿主缺少此字段的响应。宽松旧消费者可忽略新增字段。HTTP 路径/方法、Native 白名单、窗口身份、Store schema、准入与凭据保护不变。

## 验证

- 实际页面 RED：[providers-red.log](providers-red.log)；缺失原 Providers view，真实浏览器失败。接口 RED：[config-identity-red.log](config-identity-red.log)，实际200响应缺少 project_id。
- 当前 API race：132 顶层 / 154 子测试通过，0 失败或跳过；[api-regression.log](api-regression.log)。
- Management/Native 资源及模板 targeted race：14 顶层 / 66 子测试通过，0 失败或跳过；[ui-host-regression.log](ui-host-regression.log)。
- 当前固定 Claude Native 五阶段 success + restart + 人工决定读取/写入通过；[human-native-capture.log](human-native-capture.log)。供应商为合成上游，没有真实模型或 Jev 调用。
- API 124 + 当前 Native bridge 2 项实际响应样本，OpenAPI 41 paths / 49 operations 完整覆盖，新增5类配置响应反例拒绝；[openapi-check.json](openapi-check.json)。初次只读普通 API 样本未覆盖两项人工决定操作，保留[普通 API 样本覆盖拒绝复核](openapi-api-only-rejection.log)；补采本轮 Native 样本后通过，没有采用历史响应替代当前证据。
- 原 UI 的6项新增交互覆盖实际数据/安全文本/角色草稿选择/独立 Store 保存回读、跨项目和错误版本、keyboard/focus/backdrop、保存未确认原请求、真实来源撤销、未保存取消确认和旧项目迟到响应。
- 有效保护 mutation：在独立浏览器上下文中只移除当前 model.mjs 的项目身份判断，跨项目拒绝测试实际失败；[identity-mutation.log](identity-mutation.log)。生产文件与二进制未修改；其他浏览器上下文保持原模块。
- 首次完整回归95通过/1失败：[browser-final.log](browser-final.log)。迟到响应测试没有处理原切换项目的丢弃草稿确认，浏览器默认拒绝切换；补齐显式确认后 targeted 通过：[late-confirmed.log](late-confirmed.log)。产品未绕过确认或降低测试断言。
- 修正确认操作后，全部现有/新增浏览器和模型测试96/96通过，0失败或跳过；[browser-confirmed-final.log](browser-confirmed-final.log)。
- Go1.26.3 CLI/GUI/full vet exit0；[build-results.json](build-results.json)。
- 当前 Native 工具仍返回 Mac locked，未取得此版本实际 Wails 点击/像素；[native-ui-state.json](native-ui-state.json)。

浏览器截图来自当前真实私有 ControlHost 和合成模型、账号；[桌面深色列表](desktop-dark.png)、[桌面浅色详情](desktop-light-detail.png)、[390px 浅色详情](mobile-light-detail.png)、[390px 深色列表](mobile-dark.png)。已查看桌面列表和手机详情，保留原 CSS；不代表真实供应商准入或 Native 窗口验收。

## 重现

在 implementation 仓库根执行，使用 Go1.26.3 / Python PyYAML+jsonschema / Node Playwright，日志先创建私有 scratch，不能覆盖历史证据：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/providers-ui/fixture-build.log test -mod=readonly -tags fusion,nogui \
 -c -o "$PWD/.fusion-dev/task-ui-fixture" ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
 node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs internal/gui/tests/fusion-editor-model.test.cjs
```

配置采样仅对 API package 传 `-fusion-api-contract-out`；人工决定实际样本对 bootstrap success 子测试传 `-fusion-human-control-capture` 并指定已安装的固定 Native CLI。两份本轮响应数组相加后执行 `scripts/fusion/check-openapi.py`，不能给整个 Go suite 传仅单 package 支持的测试 flag。Mutation 使用显式 `FUSION_PROVIDERS_ID_MUTATION=1` 和 `--test-name-pattern='Magpie Providers rejects foreign'`，预期 exit1；正常运行不设置该变量。

更多操作见[合同](../../../contracts/magpie-providers-ui.md)。日志归档只移除行尾空白，[原始日志哈希](raw-log-hashes.json)保留。真实供应商准入/登录、其他原页面、当前 Wails 操作、本机真实工程及最终60类验收仍未完成；WP-16 保持 in_progress，最终验收 not_run，Jev off。
