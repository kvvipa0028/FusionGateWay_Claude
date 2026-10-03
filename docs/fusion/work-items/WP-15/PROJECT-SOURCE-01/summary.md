# WP-15-PROJECT-SOURCE-01 本机私有项目登记

状态：`done`，父 WP-15：`in_progress`。BASE：`9af4e0568ad39c7a9e8a79f72c5a019d7ff4fb78`。

新增 bootstrap.Load，只读 Git 外私有 projects.json：已有本机文件夹、明确 read/write、精确内置路线元数据、五角色 Layer 与预算边界。默认禁止写入；空模型选择保持 inherit。声明永远不授予 Admitted/BillingKnown/锁定/capabilities，不能承载 API key/URL/argv/证据。Provider workspace 与项目文件夹分别保存。

目录 FD/openat/NOFOLLOW、私有 owner/权限、单 hardlink/普通有界文件、JSON唯一字段/fold别名/深度及未知字段验证。FIFO非阻塞拒绝，来源不能在Git或项目内；项目间嵌套/共享身份拒绝。Project/Projects深复制，Loaded打印脱敏；Current检查来源身份/内容及文件夹身份/权限。产品执行仍须独立Current、文件访问边界和真实准入。

新增8项顶层 race tests，含实际受鉴权 Handler SetProject→GET配置→preview 422 / route_revision_not_admitted，无项目绝对路径公开。RED为缺实现；增加API兼容测试时临时Store根目录非私有而失败，修正fixture为0700，不削弱Store检查。加强preview原因断言时先误用扁平error格式；实际API合同为error.code，纠正测试字段而未修改生产API。所有失败日志保留。

最终全量 Fusion race **360 PASS / 10 SKIP / 0 FAIL**。CLI/GUI build与全仓tagged vet exit0；模板JSON、复核命令zsh语法、gofmt、diff检查通过。运行环境临时HOME/XDG、Go1.26.3、macOS/arm64。十项skip仍为七项Native显式opt-in、一项真实额度opt-in、两项helper；本项未改Native/controller，不重跑或宣称新的Native验证。

本轮真实模型调用0、额度查询0、凭据读取0（发布排除检查仅以内存比对私有key与交付内容，不调用上游）。schema5/001–005、Store/API/controller/Native/OpenAPI源码未改；现有24路径/28操作合同沿用。本机实际配置与试点文件夹未创建，未注册产品listener/CLI/GUI或开放旧入口，Jev off。下一项接线可信产品宿主，实际路线/计费/额度、工程闭环和最终Gate仍待实施。

合同与复制模板见 [local-project-source.md](../../../contracts/local-project-source.md)。组件完成不表示WP-15或整体目标完成。
