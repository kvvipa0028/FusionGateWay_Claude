# 按项目读取任务列表

此接口为单阶段工作台提供任务发现，不启动阶段或改变任务、预算、快照、事件及路线准入。

Management 保护的只读路径：

```text
GET /control/v1/projects/{project_id}/tasks
GET /control/v1/projects/{project_id}/tasks/before/{task_id}
```

项目必须已在可信服务端登记。固定每页最多 32 项，按当前 SQLite 任务入库顺序倒序；排序使用内部 rowid，响应和游标不暴露它，也不伪造创建时间。后续页以同项目既有任务 ID 为边界，严格读取它之前的任务。外项目或不存在的边界均返回 404，无信息区分；不接受 offset、任意页大小或 query。

```json
{
  "tasks": [{
    "id": "task-example",
    "project_id": "local-pilot",
    "goal": "检查本地工程",
    "state": "ready",
    "plan_revision": 1,
    "generation": 0,
    "goal_truncated": false,
    "etag": "\"p1-g0-ready\""
  }],
  "next_before": ""
}
```

有后续页时，`next_before` 等于本页最后一项 ID；没有后续页时为空字符串。空列表为 `tasks:[]`，不是 null。列表是当前视图，不是冻结的多页快照：新任务在刷新首屏后出现，不挤动已读取的旧页；旧任务状态可以变化。没有任务删除接口；以后若增加删除或重建 tasks 表，须重新评估游标合同。

`goal` 最多 512 Unicode code points，超出时 `goal_truncated=true`；摘要不能替代执行输入或完整目标。现有 `GET /agent/v1/tasks/{task_id}` 仍返回原完整 Task 和资源 ETag。列表 `etag` 从同一行的 plan_revision/generation/state 生成，详情或控制前应重新读取 Task 并沿原 If-Match 核验；列表本身没有 ETag 或写操作。

仅返回上述八个字段，不包含模型、目录、账号、凭据身份、预算、owner、Native session、原始输出或 Snapshot。目标是用户已有任务数据，仍只对管理授权开放；前端按文本渲染，不把目标当 HTML 或指令。摘要和数组上限约束响应，不宣称 SQL 扫描成本与全项目历史数量无关。

非 GET 为 405/Allow:GET；非法路径/query 为 400，未知项目/边界为 404。Management 缺失、错误或撤销为 401，stage/cross-site 等沿原 middleware 拒绝；等待登记/Store 读取后重新检查 ManagementCurrent，请求取消或撤销时不返回任务数据。Store 错误为 500，返回固定错误，不返回部分页。所有响应沿 Handler 的 no-store/nosniff；ControlHost 来源或 token 失效沿原 503。

NativeStageBridge 只向已登记项目开放这两个精确 GET 路径，另按[任务工作台](task-workbench.md)开放精确冻结提交及详情读取，不开放启动/取消。实际私有本机 HTTP、Native 桥函数及 OpenAPI 样本已验证，见 [TASK-INDEX-01](../work-items/WP-15/TASK-INDEX-01/summary.md)。后续 TASK-UI-01 已加入页面列表消费者，未增加实际 CUA 窗口证据，未使用真实 key、模型或额度；生产 Factory/账号/额度准入、阶段执行控制工作台和最终 T01–T60 仍待完成。Jev off。
