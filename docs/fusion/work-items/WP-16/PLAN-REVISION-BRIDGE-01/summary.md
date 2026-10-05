# WP-16 · PLAN-REVISION-BRIDGE-01

完成既有计划修订API的精确Native通路：plan/preview POST与plan PUT。当前窗口、私有宿主与登记Task核验后才调用owned loopback Management HTTP；GET plan保持原路径，额外段/未知方法/未登记Task拒绝，不开放通用代理。现有8秒/128KiB/2MiB与回复前来源检查保持不变。页面、原Magpie模板/脚本/全部CSS未改，界面修订按钮尚未接入。

基线`06e0dac7228211942b073877e693e363a669dd3d`。遵从原[计划修订合同](../../WP-15/REVISION-01/summary.md)：强计划revision If-Match、冻结receipt、原预算上限、不可变历史与已启动角色约束。不用运行控制的Task ETag；Native拒绝任何不受支持的Idempotency-Key，不默默丢弃该意图。

## 发现并修复的权限缺口

为了接入新窗口操作，增加了当前权限延迟测试。实际旧API在已通过Management middleware后等待时，取消或撤销权限仍返回200；PUT还提交了revision2。[实际RED](revision-authority-red.log)。新实现增加入口、Server等待后、预览保存前与回执前的ManagementCurrent检查，且使用`RevisePlanCurrentGuarded`在Store锁取得后和事务提交前重查原请求权限；失效一起回滚快照、Task版本与plan_revised事件。旧可信进程内Store方法的签名、行为及SQL/schema保持不变。

该修复属于新开放计划修订通路的必要鉴权边界，未扩展至其他接口或重构原鉴权系统。正常已授权请求与原receipt重读保持原语义；失效请求现在401/management_authority_unavailable，不返回冻结目标或写入计划。

## 验证证据

- 真正路径RED：[native-path-red.log](native-path-red.log)，原Native桥在真实HTTP到达前返回404；接入后[首轮通过](native-first-green.log)。此前一次未使用import编译失败、一次合成Factory遗漏text capability初始化失败保留：[native-red.log](native-red.log)、[native-behavior-red.log](native-behavior-red.log)，没有把fixture失败冒充产品行为RED。
- NativeStageBridge→真实loopback HTTP→可信合成Factory/真实Store：五角色任务，明确review未来绑定修订；预览无事件，应用精准冻结快照；活动design run/session/attempt/generation/target与预算保留，未修改角色及历史不变；后续revision3存在时旧receipt仍只读返回原revision2。没有Inspector/Resolver/Runtime调用。
- 窗口、方法/路径、重复或非法强tag、未知DTO/预算/凭据/独立性/角色导入、跨Taskreceipt、外国/未知Task、空Idempotency-Key、两次并发仅一次成功、预览后角色启动拒绝、来源在响应前撤销与重开回读均有实际断言。活动run是明确的合成Store生命周期，不宣称真实Native供应商进程或StopProof。
- 首次扩展反例对空Layer误期望400，原JSON/DTO合法但语义编译失败应422；按`stageplan.ErrInvalidPlan`到422的既有映射修正，仍断言无事件/快照/执行。[失败](native-expanded.log)、[修正](native-expanded-corrected.log)。没有修改运行行为以迁就测试。
- API管理等待撤销/取消、原receipt准确重读权限、当前plan读取权限已验证；Store当前权限缺失/开始失效/提交前失效/合法提交验证全事务回滚或精准提交。[初轮GREEN](authority-green.log)、[Store缺失方法RED](store-authority-red.log)。
- 两项有效mutation：移除登记项目保护时实际外国Task GET plan返回200，新测试失败；强制事务authority恒true时失效授权仍能写入，新测试失败。两次生产源码均按原bytes恢复：[项目边界日志](project-boundary-mutation.log)、[事务权限日志](commit-authority-mutation.log)、[恢复记录](mutation-results.json)、[权限恢复记录](authority-mutation-results.json)。

初次API/Store全模块运行通过，但事务mutation曾与该运行有时间重叠，保留[初次日志](api-store-final.log)；最终证据使用生产源码固定后单独重跑的API/Store/Bootstrap回归，不把重叠检查作为最终闭合证明。早期鉴权修复前的构建与桥/API结果是历史记录，最终构建另记。

## 重现与范围

在implementation工作树执行；Go1.26.3与本机SDK可用，日志目录先创建：

```sh
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
 .fusion-dev/plan-revision-bridge/final.log test -mod=readonly -tags fusion,nogui \
 -race -count=1 -timeout=240s -p=2 -v ./internal/fusion/api ./internal/fusion/store ./internal/fusion/bootstrap
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
```

操作与错误/重试边界见[合同](../../../contracts/native-plan-revision.md)。修订receipt仍是原进程内记录，重启后需重新读取当前计划并明确预览；持久快照可读取但不能作为原receipt重放。没有变更Store schema、Controller/Scheduler、供应商准入、OpenAPI DTO或Jev off。

实际Wails修订按钮/像素、原Magpie最小页面消费者、三路线真实准入、工程试点及最终T01–T60仍待完成；父WP-16及整体目标继续in_progress。日志归档只删除行尾空白，原始日志哈希保留；未纳入真实供应商key。

## 最终固定源码结果

最终 API/Store/Bootstrap 回归共 375 项顶层与 485 项子测试通过，6 项顶层与 0 项子测试跳过，0 失败。[逐模块计数](test-results.json)、[完整最终日志](api-store-bootstrap-final.log)。跳过项是需显式启用的 fixture/真实连接入口，未作为完成证据。最终 Go1.26.3 CLI、GUI 编译及全仓 vet exit0，[构建结果](build-results.json)。
