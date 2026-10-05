# 未开始阶段计划修订 Native 通路

组件 WP-16-PLAN-REVISION-BRIDGE-01，是既有[计划修订API](../work-items/WP-15/REVISION-01/summary.md)的窗口消费者通路。只在已绑定的 Native 窗口内接入，不开放旧 `/api/*`、通用代理、额外 Worker 操作或真实供应商准入。页面和 CSS 未改；界面控件将继续沿用原 Magpie 的任务详情和模型选择器。

## 精确通路与请求

| 方法/路径 | 合同 |
| --- | --- |
| GET `/control/v1/tasks/{task_id}/plan` | 既有读取当前快照，ETag 为原计划 revision |
| POST `/control/v1/tasks/{task_id}/plan/preview` | If-Match 为读取到的带双引号正整数 revision，body 只有 `task` Layer；返回服务端冻结的新计划、receipt、配置版本及原预算上限，无持久效果 |
| PUT `/control/v1/tasks/{task_id}/plan` | 同一原 revision If-Match，仅发送原 `preview_id` / `plan_hash`；成功返回该次提交的不可变 Snapshot 和新 ETag |

计划 tag 如 `"1"`，不是运行控制的 `"p1-g0-ready"`。缺失428、非法/重复/弱tag400、版本或已开始角色冲突409。格式正确但空 Layer 无修改，语义编译失败422。未登记/未知Task404。Native 对上述通路拒绝任何 Idempotency-Key，包括空值；只通过原receipt实现准确重读，不丢弃调用方不受支持的请求意图。

请求先经过 SDK Native 来源检查、冻结窗口身份、私有宿主 current 核验，然后确认 Task 属于已登记项目，最后使用 Go 内部管理凭据调用 owned loopback HTTP。未知方法、额外路径段和项目路径不透传。原 If-Match 保留重复值给服务端严格判定；不归一化。既有API增加等待后与回执前的当前Management重查；新增可信Store guard在取得锁后、提交前检查同一请求的权限，失效会回滚快照、当前版本和事件。旧进程内可信Store方法保持签名/行为。来源/管理身份在回复前失效时，拒绝已缓冲响应；真实提交可能已完成，不能把503解释为回滚。

## 计划与执行边界

只对没有任何 stage_run 的角色生成新绑定；任何已开始/已结束/失败/取消/未知attempt的角色均不可改写。原角色集合、已冻结未修改绑定、旧快照、活动run的target/lease/session/attempt/generation、当前state与累计预算保持不变。不调用 Runtime、Inspector、Resolver，不暂停或启动新阶段，不退还预算。预览后阶段启动的竞态在提交事务再次拒绝。

旧提交receipt准确重读仍返回当次冻结计划，即使任务已有更新版本；不把当前计划倒回旧版。当前状态应重新GET读取。receipt仍为原进程内、有界、五分钟预览；已持久计划可在宿主重启后读取，原receipt重启后不能重放，需读取当前状态并重新明确预览。组件未新增Store schema或恢复授权。

## 验证与未完成

[证据](../work-items/WP-16/PLAN-REVISION-BRIDGE-01/summary.md)覆盖真实 NativeStageBridge→loopback Management HTTP→可信Factory/Store，SDK窗口header、外国/未知Task、强tag、非法DTO、并发只允许一个提交、跨Taskreceipt、预览后启动、来源撤销、历史/预算保持及宿主关闭重开。活动run是明确标注的合成Store生命周期，不冒充真实Native供应商执行或StopProof。

实际Wails点击/像素、页面计划修订控件、三路线真实准入、工程试点与最终T01–T60仍未完成。WP-16保持in_progress，Jev off。本组件未变更UI模板、配色、CSS或原Magpie资产。
