# WP-15-CONTROL-HOST-01 独立本机草稿服务

状态：`done`，父WP-15：`in_progress`。BASE：`526cbf22363383a7c2e5a8b5cb2759dc7108c074`。

新增fusion tagged fusion-control命令，在legacy登录/网关/代理/更新逻辑之前分派。独立private HOME/XDG，数值127.0.0.1 listener，精确Host/peer和自身Origin，持久随机0600管理能力；任务库controller lock复用schema5。私有控制状态不与项目/Git重叠。所有来源/项目/控制根/tasks/管理文件当前身份或权限不符均拒绝，并撤销Management；250ms watcher撤销已有SSE。Close等待Handler后关闭Store，可重复且支持重启。

服务只登记未准入路线和空选继承的draft configuration，不登记Controller/Native/额度采集，不开放旧model出口；执行主开关false、Jev off。不会将本机JSON当真实账号/计费/额度证明，管理能力不放argv/environment/stdout或模型请求。

新增5项Host race tests与2项CLI测试。缺实现RED后补代码；第一轮fixture控制根父目录非private，改为已经private的config目录下，不改保护规则。进一步验证现有SSE撤销、重启/双实例、目录与凭据轮换拒绝。实际CLI烟测发现run-dev未创建data/fusion-gateway子目录；扩展既有private目录准备并获GREEN。随后真实launcher SIGTERM测试发现Python退出但子服务仍持有stdout，TimeoutExpired RED保留；受管Popen转发SIGINT/SIGTERM并wait后实际退出0，测试失败仅清理自有合成进程组后再删除临时目录。

最终全量Fusion race **365 PASS / 10 SKIP / 0 FAIL**，CLI2 PASS；CLI/GUI build和全仓tagged vet0，无tag/nogui原版回归编译0。实际launcher→CLI→loopback HTTP smoke核对正确/错误管理、无私有路径/凭据输出、未选模型preview422、旧model404、来源改变503、SIGTERM实际退出0。全部临时合成项目，不读取真实provider key或调用模型/额度。十项skip仍为七项Native opt-in、一项真实额度opt-in、两项helper；本项未修改Native执行代码，未重跑Native或提升fixture为真实准入。

OpenAPI只修正当前24路径/28操作描述并标记draft_control_registered=true；生产执行仍false，DTO/路径/Handler/Store/schema5/迁移不变。沿用上一项49个实际Handler样本与官方3.1文档schema进行离线核对通过，未宣称重新采集或完整产品运行。Python语法、runbook zsh、gofmt和源码/文档diff通过，source/packet hashes及私有key排除核对通过。

合同与启动/复核步骤见 [control-host.md](../../../contracts/control-host.md)。当前是真实可启动的草稿服务，真实用户项目配置/试点未安装，Controller/Native/真实路线身份与计费额度/GUI及工程闭环尚未接线；组件完成不等于WP-15、30工作包或60类最终Gate完成。
