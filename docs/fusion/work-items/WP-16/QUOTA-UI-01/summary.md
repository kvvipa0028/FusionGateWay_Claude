# WP-16-QUOTA-UI-01 · 原 Magpie 额度观察区

组件完成；父 WP-15/WP-16 和整体工程目标仍为 in_progress。基线 f9cfad4。沿用原 Magpie UI，只加默认折叠的额度列表、缓存读取/手动刷新；原 Magpie 三个资源及 Fusion CSS 未改。Native GUI 可用明确三参数登记已有 GLM quota-only 产品宿主，execution_enabled=false，Jev off。

改动集中在 quota.mjs、阶段页小范围挂接、固定 embedded 资源、Native 精确允许路径及 GUI 参数；API/Store/调度/Runtime/OpenAPI 不改。来源/窗口/管理授权末端复核、请求/响应 bounds、8 秒 Native 期限保持不变。GUI partial flags 拒绝；默认无 key/后台查询。详见[操作与合同](../../../contracts/quota-ui.md)。

有效 RED→GREEN：页面额度区缺失；Native 缓存路径 404；「可用」响应但订阅用量 null 仍显示可用；390px 窄窗口状态在 sub 被省略。分别完成固定通路、保守额度判断和现有 p.details 独立状态行。中间完整回归的一处同名文本定位歧义改为原 design.details 精确范围，保留失效路线断言，没有降低测试。后续完整回归暴露原暂停测试先等待控制回执、却立即断言异步详情完成；按实际合同改为等待 paused/ready/cancelled 详情后执行原状态断言，未修改产品控制逻辑。

最终浏览器 44 PASS，含原 36 项及新增 8 项额度场景。真实临时产品 CLI/API 和 test-only 合成 QuotaSource 验证：加载只读缓存、手动刷新一次、0% 未核验、独立 credit 单位、源/收到时间保持、丢失回执 GET 核对无重复 POST、非法成功回复保留历史、旧项目回复丢弃、unknown/stale/zero/auth/unsupported、可用但空百分比降未知、源时间经过后本地过期无 HTTP 轮询、窄窗口状态完整与无横向溢出。Desktop/mobile 及完整状态行截图单独归档；浏览器截图不替代 Native 视觉证据。

Go targeted bootstrap race 15 top/41 sub PASS，覆盖新 Native 两项及既有 quota/桥接边界；GUI CLI 参数与 Cocoa navigation 2 PASS。全部无 FAIL/SKIP。Go1.26.3 最终 CLI/GUI/full fusion,nogui vet exit0。GUI 测试日志包含现有 SDK macOS 链接版本警告，测试与构建退出均 0；不将警告说成新增修复。

实际 owned Native 产品使用私有 key 启动 quota-only、读取初始 unknown 缓存，SIGTERM 等待 GUI/launcher exit0，临时私有项目/HOME/XDG/Store 清理。CUA 返回 Mac is locked，实际窗口像素、点击与供应商刷新未验证。该探针使用最终状态行修正前的构建，仅证明产品启动/登记/退出；本轮零真实 quota refresh 和零模型调用。此前 GLM-QUOTA-HOST-01 的真实 CN 观察保留为历史，不声称当前用量。

保留原 Native 5 秒响应头/8 秒整体期限：慢查询回执未确认时只提示手动 GET 核对，Broker 的 10 秒共享查询和 owned 关闭合同不变，不自动 POST 重试。物理池/账号归属/计费/模型/effort 与生成准入分别验证；查询成功和 0% 不升级 Controller 或 route。

尚待：实际 Native 点击、关闭窗口后的原启动身份恢复、运行事件/计划修订、真实供应商 Factory/Forwarder/物理池与计费证明、工程各阶段和最终 T01–T60。最终验收矩阵保持 not_run，未将单组件验证当作全工程验收。

复现：先按合同构建 CLI/GUI，并用隔离 Go runner `test -c -tags fusion,nogui -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap` 生成 synthetic fixture；当前 browser test 检查服务资源逐字等于源码，禁止拿旧 bundle 运行。`NODE_PATH` 指向已装 playwright 后执行 `node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs`。合成模式只存在测试二进制，产品入口不能选择这些虚假准入值。每个 fixture 退出等待 owned 宿主 exit0 并清理临时目录。
