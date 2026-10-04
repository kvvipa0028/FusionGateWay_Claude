# GLM Coding Plan 独立额度宿主

组件 WP-15-GLM-QUOTA-HOST-01。产品 fusion-control 增加明确的额度用途参数，把已实现 FileCredential/QuotaReader 接入受管理鉴权的 quota API；不注册执行 Controller，不改模型准入、调度条件或 Magpie UI。Jev off。

## 登记及当前身份

OpenQuotaControl 的 trusted QuotaFactory 只获得 Loaded 和 Current；不能返回执行路线、Inspect/Resolve/SelectAuto，也没有 Scheduler/Store/Manager。它返回额度源与可选 cleanup，共享原 owned 查询取消/关闭顺序。OpenExecutionControl 仍要求已准入路线、Inspect、Resolve；不会因为新增模式接受缺少这些条件的执行服务。

内置 OpenGLMQuotaControl 要求明确 project ID、route ID、私有 glm-coding-plan.key 路径。项目和路线必须在已冻结私有 projects.json 中；同 ID 的多 revision 拒绝而非选第一个。仅接受内置 CN GLM 声明派生的 coding_plan、无 plugin 路线，不尝试团队/海外/普通 API fallback。key 必须满足已有仓库外、无链接、当前 UID、祖先私有权限、单 hardlink 和冻结 inode/content 的 FileCredential 合同，不能位于任一已登记项目内。

账户/workspace/credential identity 来自准确路线声明；quota identity 为 bigmodel/CN，generation 为当前私有来源 revision。它们是本机登记标签，不证明上游账号归属。每次 Current/实际查询前后重核宿主来源与冻结凭据；替换、内容或权限变更使旧读取器失效，旧缓存不继续展示为可用，拒绝新刷新，不自动采用新 key。停宿主、更新明确来源/身份后重新登记，已有任务绑定不被改写。

## 产品操作

默认 fusion-control 不读 GLM key、不查询额度。只有三个参数同时明确提供才启用 quota-only 接线；部分参数、未知项目/路线、非法 key 位置拒绝启动。启动和 GET 缓存均不查询上游；鉴权后的 POST refresh 使用固定 CN 个人套餐端点，至多一次 HTTPS，沿已有单次传输/响应 bounds/脱敏规则。整个服务八个采集名额及 Broker 合并/期限保持不变。

先按[私有项目来源](local-project-source.md)登记本人选定 GLM 路线；在当前工作树构建，使用独立开发状态。下面只含文件路径，不包含 key 内容：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
fusion_control_root="$HOME/.config/fusion-control-dev"
fusion_project_source="$HOME/.config/fusion-local-registration/config/projects.json"
fusion_glm_key_file="$HOME/.config/fusion-gateway/credentials/glm-coding-plan.key"
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/run-dev.py \
  --root "$fusion_control_root" -- \
  fusion-control --projects "$fusion_project_source" \
  --glm-quota-project local-pilot --glm-quota-route REPLACE_EXACT_DECLARED_GLM_ROUTE \
  --glm-quota-key "$fusion_glm_key_file"
```

将 project/route 替换成实际登记 ID，不能用模型名代替 route。启动 JSON 的 quota_query_enabled=true/execution_enabled=false 只表示查询配置已加载；没有后台额度轮询。沿[ControlHost 的内存凭据读取流程](control-host.md)访问 GET `/control/v1/projects/{project_id}/quota`；明确查询使用 POST `/control/v1/projects/{project_id}/quota/{route_id}/refresh`，JSON body `{}`。HTTP 不接受 key、URL、账号、workspace 或 generation 参数。Native 桥与 UI 额度面板尚未接入；不把管理凭据放进浏览器 URL、聊天或 shell 参数。

Ctrl+C/SIGTERM 停止 owned 宿主。关闭撤销管理、取消正在读取的查询、等待实际 Reader 结束，再执行 cleanup/关闭 Store。CloseContext 超时不等于 Reader 已停，不能提前清理其依赖。首个 HTTP 断线不取消 Broker 的共享读取，来源撤销/宿主关闭则会取消。

## 当前结果和真实探针

实际产品 CLI 使用现有私有 key 完成一次刷新，两个 coding_plan 模型窗口均 0% 已用；GET 缓存没有改写 observation，宿主 SIGTERM exit0、临时状态/合成项目删除。模型调用数 0，未读取日常 Claude 认证，未修改用户日常配置。观察时间和二进制 hash 见 [原记录](../work-items/WP-15/GLM-QUOTA-HOST-01/glm-quota-product-live.json)，它是当次快照，不保证现在的额度。

Reader 仍 Complete=false、Pool.Verified=false、status=unverified；0% 不证明物理池、账号归属、所有窗口完整、模型/effort/生成计费正确，更不能启用 Scheduler。真实生成 Factory、Native/UI 查询、工程流程和最终 Gate 仍未完成。

可选择执行一次真实只读复核；不自动重试，不发送模型请求。报告路径必须不存在，以免覆盖已有证据。探针使用全新私有合成项目与临时 HOME/XDG，在 finally 中停止自己启动的产品进程并清理临时目录；不会修改日常工程。

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/probe-glm-quota-host.py \
  --binary .fusion-dev/fusion-gateway-cli --go "$HOME/.local/bin/go" \
  --key-file "$HOME/.config/fusion-gateway/credentials/glm-coding-plan.key" \
  --report .fusion-dev/implementation/glm-quota-product-recheck.json
```

失败只输出固定 unconfirmed，不输出底层错误、HTTP body、私有路径、key/token；核对配置、权限和网络后再明确选择复核。此探针不能升级工程路线准入。

官方 [Claude Code 接入说明](https://docs.bigmodel.cn/cn/coding-plan/tool/claude)确认 CN API key/Base URL 的工具配置方式；本组件的只读传输以已冻结 Reader 和实际 GET 证据验证。查询授权与套餐生成用途继续分别核对，不从 Native firstParty/costUSD 标签推断计费。完整回归和边界见 [GLM-QUOTA-HOST-01](../work-items/WP-15/GLM-QUOTA-HOST-01/summary.md)。
