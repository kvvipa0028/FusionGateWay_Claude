# 原 Magpie 界面的额度观察区

组件 WP-16-QUOTA-UI-01。按用户要求沿用 Magpie 的顶栏、列表、按钮、字体、配色及现有 Fusion 三个阶段配置区，只增加默认折叠的「路线额度」。原 app.css/app.js/index.html 和 Fusion editor.css 不改，不引入新框架或导航重设计。

页面载入和项目切换只 GET 当前登记项目的缓存，不发供应商请求。展开额度区后，「读取额度缓存」仍只读缓存；「刷新额度 route」才 POST 精确登记路线的 refresh，body 为 `{}`。未提供有效 Identity 或不支持查询的路线禁用刷新；来源来自服务端注册，不接受页面 key、URL、账号或 generation。配置保存、任务提交未确认时额度按钮禁用；额度读取本身不阻挡切换项目。切换/重新载入时立即清空额度区，旧项目与旧请求回复均不能覆盖新项目。

显示未知、过期、未核验、耗尽、需要授权和不支持查询。用量 null 显示「未知」，数值 0 显示 0% 已用；两者不等价。可用/耗尽缓存按源观察时间最多一分钟及重置时刻本地降级，不轮询。响应即使声称可用，缺少完整性、有效物理池、订阅窗口或有效百分比时也不显示可用。完整路线版本、账号/workspace、Identity、观察字段、时间及窗口范围需核对后才能替换显示；无效成功回复保留上次独立观察并标为历史。

来源观察时间、宿主接收时间分别显示，未知源时间不补成收到响应时间。credit/token/money 等独立资源保留单位和数值，不与订阅百分比相加；缺少物理池核验不合并，已核验共享池提示别名不累加。本界面不从额度推断生成准入、模型/effort 正确或实际计费路线。错误只显示固定提示，不渲染 raw、未知字段、HTTP 底层错误或供应商错误消息；所有显示使用 textContent。

查询回执丢失、超时或无效时不自动重试 POST；手动 GET 缓存核对实际观察。Native 原 5 秒响应头、8 秒总期限不变，Broker 查询期限仍为 10 秒；迟到观察可能已进入缓存，不能因窗口超时宣称供应商没有处理。来源/凭据撤销须重新登记；旧 cached value 标为历史，不能继续当作当前可用。关闭沿既有 Bridge/ControlHost 的取消、等待实际 Reader 和清理顺序。

## Native 产品操作

默认 fusion-ui 不加载 key。三个可选参数须同时指定，使用与 [GLM 额度宿主](glm-quota-host.md)相同的 private FileCredential/fixed CN QuotaReader；execution_enabled=false、Jev off。项目与 route 必须是实际已登记 ID，不是模型名：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/run-dev.py \
  --root /absolute/private/fusion-ui-state \
  --binary "$PWD/.fusion-dev/fusion-gateway-gui" -- \
  fusion-ui --projects /absolute/private/projects.json \
  --glm-quota-project REPLACE_PROJECT_ID --glm-quota-route REPLACE_ROUTE_ID \
  --glm-quota-key /absolute/private/glm-coding-plan.key
```

参数只含私有文件路径；不要将 key 内容放入聊天、projects.json、shell 参数或仓库。Native 只开放已登记项目 quota GET、精确已登记 route refresh POST 与 quota.mjs GET/HEAD；沿用本窗口、管理授权、冻结来源、限额与响应末端检查。未知项目/路线/方法/后缀拒绝，HTTP 输入不能改变供应商。

部分参数、无效路径、失效来源/凭据使入口拒绝启动，不 fallback。单次刷新失败可先手动读取缓存；需要更换来源或 key 时先关闭 owned 窗口/宿主，更新私有登记后重开。退出不撤销已有任务或退还执行预算。

## 证据边界

真实私有 CLI/API 浏览器流程覆盖自动缓存/手动刷新、真实合成 QuotaSource、零用量未核验、资源单位、原时间保留、丢失回执缓存核对、非法成功响应、跨项目迟到响应、授权与空值状态、本地过期及窄窗口。合成 Fetch 在测试二进制中，无供应商调用，不能作为真实账号/物理池准入。

Native Bridge 使用真正 owned HTTP/API 验证 scope/window/source fence、输入拒绝及资源服务；实际产品 GUI 使用现有私有 key 启动只查询宿主、读未知缓存并正常退出，未进行供应商刷新。CUA 返回 Mac is locked，实际窗口像素与点击尚未验证。此前一次真实 CN 查询的历史结果在 GLM-QUOTA-HOST-01；本轮没有新真实查询或模型调用。

父 WP-15/WP-16 继续 in_progress。实际 Native 操作、真实生成 Factory/账号/物理池/计费、运行事件/计划修订及工程闭环和 T01–T60 仍待完成。[组件证据](../work-items/WP-16/QUOTA-UI-01/summary.md)区分浏览器、Native Bridge 和实际窗口证据。
