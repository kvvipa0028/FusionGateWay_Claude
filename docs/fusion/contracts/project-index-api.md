# 可信项目列表 API

Management 保护的 `GET /control/v1/projects` 返回当前服务由可信 SetProject 登记的项目：

```json
{"projects":[{"id":"local-pilot","configuration_revision":1}]}
```

按 ID 排序，最多128项；没有登记时返回空数组，不返回null。不包含名称、项目目录、路由、账号、凭据引用、权限或准入标记。名称与目录的可信GUI展示接线尚待实施，前端不能从此响应猜测或提供任意workspace path。

每个配置版本与读取对应 `GET /control/v1/projects/{project_id}/configuration` 使用的服务内版本相同；保存的global/project默认层变化会使该投影版本更新。列表是有界发现读，不承诺跨不同项目/不同Store默认层写入的数据库级原子快照。没有列表ETag或If-Match写操作；配置revision只是发现提示。界面选择项目后重新读取具体配置/defaults/presets，写操作继续用对应资源ETag，任务创建仍以服务端preview/hash和持久原子stamp核验为准。列表不会修改已提交任务或提供运行准入。

只支持GET；其他方法405并返回Allow:GET。query400；Management缺失/错误/撤销401，stage/query凭据与cross-site403；读取后的请求取消或issuer撤销也拒绝。Store失败500，无部分项目列表。bootstrap来源/私有目录/token撤销沿宿主原503。响应no-store/nosniff，不提供CORS或匿名访问，不采集额度或调用Native。它不新增HTTP项目注册接口，已有草稿execution off保持。

当前OpenAPI与实际Handler样本已同步；验证证据见 [PROJECT-INDEX-01](../work-items/WP-15/PROJECT-INDEX-01/summary.md)。可信登记来自现有私有项目来源，真实Factory与GUI鉴权仍为后续工作。
