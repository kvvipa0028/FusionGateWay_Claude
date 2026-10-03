# WP-03：离线假上游与假 Runtime

完成本地合成 HTTP、CLI/NDJSON RPC 与小型缺陷项目。全部模型/账号为 fixture 标识。Transport 只允许登记的数值 loopback listener，拒绝路径外发计数为 0；Go 下载关闭。Runtime 子进程使用白名单私有 HOME/XDG，没有真实认证发现。

覆盖模型/账号标记、分片/tool call、延迟、429/额度未知、配置失效、异常退出、断流、写后失联、孙进程写入停止和取消/完成 barrier。12 个 Go 顶层测试通过（另有子场景），6 个原生进程测试通过；Go race 通过。合成仓库初始测试按预期失败，在临时副本修复后通过；源模板保留缺陷。

本包仅提供故障 fixtures，不证明产品锁定、取消状态机或三路线真实接入。T05/T14/T15/T24/T25/T48/T59 最终产品 E2E 仍为 not_run。
