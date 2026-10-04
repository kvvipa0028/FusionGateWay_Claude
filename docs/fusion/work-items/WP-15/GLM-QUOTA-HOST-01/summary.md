# WP-15-GLM-QUOTA-HOST-01 · 产品 GLM 独立额度接线

状态：本组件完成；父 WP-15/WP-14/整体目标 in_progress，最终 T01–T60 not_run。真实生成 Factory、上游归属/计费与物理池准入、Native/UI 额度面板仍未完成。

## 修改和选择依据

原真实 FileCredential/固定 CN QuotaReader 仅能用于内部诊断，产品 OpenControl 没有可查询来源。本项新增 QuotaFactory/OpenQuotaControl 和内置 OpenGLMQuotaControl，以明确 project/route/key 文件参数独立授权查询目的；Factory 不获得 Scheduler/Store/Manager，也不能返回执行路线/Inspect/Resolve。共享现有 owned query 生命周期，Controller 始终不注册。原 OpenExecutionControl 的完整执行要求保持，nil Inspector/Resolver/未准入路线依旧拒绝。

产品 fusion-control 增加 --glm-quota-project/route/key 三参数，必须完整且准确对应私有来源，key 仍只从既有私有 FileCredential 加载；同 ID 多 revision 拒绝，key 不能位于任一登记项目。启动与 GET 缓存不访问上游；鉴权手动 refresh 才走单次 CN HTTPS，模型调用为零。来源/凭据变化阻止发布缓存或新查询，现有 Broker 合并/期限、8采集名额和 API DTO/OpenAPI 保持原语义。Magpie/Fusion UI、Native 桥、runtime Adapter/Reader、Store schema 均未改。详见[合同及本机流程](../../../contracts/glm-quota-host.md)。

Ruling：先推进真实额度用途接线，因为当前完整上游账号/计费/物理池证据不足；不把 fixture Inspect=true 或普通 API fallback 送入产品。代价是生成继续拒绝，等待实际证据，不能把本组件称为可执行版。

## 验证和问题处理

初始 RED 为缺少 QuotaFactory/OpenQuotaControl/glmQuotaFactory 的编译失败。实现后首次合成 Snapshot 没有 Status，原 quota.State 正确保留空状态，导致期望 unverified 的测试失败；按已有 Reader/AdaptGLM 合同补齐 fixture Status=unverified，生产状态算法未改。后续5顶层/19子场景覆盖真实本机 HTTP cache/手动刷新、不准入、无鉴权拒绝、冻结 key 漂移、源/宿主关闭的 detached query 取消、CloseContext 超时不提前 cleanup/关 Store、初始化失败 cleanup、作用域/路线/revision/provider/key权限/位置拒绝。

最终相关 Go race 48顶层/180子 PASS、3条件SKIP、0FAIL；产品CLI参数3 PASS。两个 Native SKIP 随后单独用冻结 Codex0.160.0/Grok1.0.48 运行，2顶层/12子 PASS、0SKIP/FAIL：10次实际 Native、15次假上游 HTTP，成功/取消/来源撤销/Close/parent cancel/启动前拒绝、真实 wait/StopProof/release 均通过。旧 Reader live诊断仍未再次运行，本项改为实际产品探针，不能把该 SKIP 描述为通过。

Go1.26.3 CLI/GUI/full fusion,nogui vet exit0；实际产品探针使用该最终CLI hash。graph14068nodes/125483edges已刷新。probe脚本真实查询后仅增加“报告已存在时在查询前拒绝/exclusive创建”，独立 no-overwrite preflight 测试与 Python语法通过；未重复真实 GET。UI/API DTO/schema未改，不重跑浏览器或OpenAPI capture。validation.json/artifacts.json 保存具体检查与文件hash。

## 实际产品只读查询

scripts/fusion/probe-glm-quota-host.py 用现有私有 key 启动当前产品 CLI，在临时合成只读项目、独立 HOME/XDG 中读取空 cache→明确 refresh一次→回读同快照→读取仍未准入配置→SIGTERM wait/reap exit0→删除临时目录。两个 coding_plan 模型窗口 0% 已用；观察时间见 glm-quota-product-live.json，这不是当前额度保证。账号/workspace是本机未核验标签，Complete=false/Pool.Verified=false/status=unverified，不从0%推导生成可用。零真实模型/Claude请求、Jev off，没有修改日常认证/项目/配置，key/token及其指纹均未输出或入库。

## 复现

当前工作树已预装 Go1.26.3/Python3/Xcode SDK/public module/cache；runner 临时 HOME/XDG 和环境白名单，不读取日常认证。基本回归：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/glm-quota-host-recheck.log \
  test -race -v -count=1 -timeout=4m -mod=readonly -tags fusion,nogui \
  ./internal/fusion/bootstrap ./internal/fusion/api ./internal/fusion/runtime/glm \
  -run '^Test(GLMQuotaHost|QuotaHost|ControlHost|ExecutionHost|ProjectSource|GLMQuotaReader|GLMFileCredential|Quota)'
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/implementation/glm-quota-cli-recheck.log \
  test -race -v -count=1 -timeout=2m -mod=readonly -tags fusion,nogui . -run '^TestFusionControl'
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

固定 Native 检查与真实只读探针分别需要明确版本/私有 key 参数；命令见父 CODEX-HOST-01 和 glm-quota-host 合同，不自动读取凭据/登录或重试上游。只清理测试自己拥有的进程/临时目录；永久控制 Store 不因退出删除。下一项继续接 Native/UI 额度路径与真实生成证据/Factory，工程阶段闭环、其余路线及60类最终 Gate 保持未完成。
