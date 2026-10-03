# 当前 Handler 的 OpenAPI 合同验证

`openapi-fusion.yaml` 描述当前内部 Handler 已实现的 24 个路径、28 个操作。无调用预览、提交/读取、阶段计划预览/修订、预算、事件流、启动/取消/run 读取、配置只读、五角色预设版本、任务预设来源、额度读取/刷新与全局/项目默认层读写/历史及任务 pause/continue/cancel 均纳入合同。独立 [fusion-control 草稿 listener](control-host.md) 已注册，生产执行 Controller/GUI 仍未接线；内部 Handler 校验不能视为真实路线或完整产品部署完成。

OPENAPI-01 只补合同；后续 DEFAULTS-01 新增了运行期默认层，并同步更新合同。OpenAPI 是人工维护的接口合同；Go DTO、现有行为合同与实际 Handler 返回是字段核对来源。以后新增接口或改变 DTO，要同时更新 YAML、样本采集与 checker 的明确操作清单，禁止只为通过检查删掉已有操作。尚未实现的操作不占用实际合同路径。

## 验证内容与边界

`scripts/fusion/check-openapi.py` 在离线条件下执行：

- 官方 OpenAPI 3.1 文档 schema 校验，组件 JSON Schema 的 Draft 2020-12 语法校验，所有引用解析，path 参数及唯一 operationId 检查。
- 已实现路径/方法清单比对，以及每一个成功操作的实际 Handler 样本覆盖。
- 49 份 Handler 返回、成功请求和所需 header 与对应 schema 的核对。包含实际本机 SSE 连接；事件帧的 `data` 用 Event schema 检查，完整 SSE 格式/重连行为仍由原事件测试验证。
- 6 个越权字段输入 schema 的拒绝检查，以及临时删除 submit path、破坏 Snapshot hash 类型、错误禁止 nil capabilities 的三项反例。反例修改只在临时合同副本中，不修改正式文件。

样本来自 `TestImplementedAPIContractSamples`。模型、账号、额度 reader 和执行 backend 都是 fixture，本项实际上游模型调用和额度查询为 0。capture 正常参与 API 测试，仅显式传入绝对输出路径时导出 JSON；不读取真实 key。

这验证接口文档的标准结构与已采集数据的一致性；不替代运行期鉴权、route 准入、所有错误/值组合、真实账号 smoke 或最终 Gate。HTTP 状态和请求限制仍以 Handler 的独立测试为依据。`x-fusion-max-utf8-bytes` 描述 Go 的 UTF-8 byte 上限，标准 `maxLength` 计字符，不能据此宣称两种计数的全部边界已覆盖。

## 核对中的具体修正

原 draft 仅 12 个路径，遗漏了已经实现的 submit、budget、plan、plan preview 和 events；Snapshot、ExecutionTarget、registry route、quota pool 等仍是没有字段的占位 object。现已补齐对应结构与方法。

实际 Management wrapper 和 `http.Error` 可以返回 `text/plain`；401/403/404 在相应位置也可能由 JSON control error 返回。405 使用纯文本。合同按实际 media type 声明，避免客户端无条件解码所有错误为 JSON。

Go DTO 中未设置 `omitempty` 的 nil slices/指针会序列化为 `null`。ExecutionTarget/registry capabilities、registry efforts/routes 和 Handler quota Snapshot 的 windows/resources 如实描述这一形式。输入层与受校验的 normalized quota reader 有更强的独立合同，不能混用：原 `quota-snapshot.schema.json` 保持不变；Handler 的可信来源 Snapshot wire schema 单独列出，保留原始 JSON，不因 wire 合法就允许调度。

## 离线复核

需要 Go 1.26.3、PyYAML、jsonschema 和 referencing。本次使用本机已有 Python 依赖，没有安装包或改变 Go module。官方 schema 固定副本与 Apache-2.0 许可保存在 [OPENAPI-01](../work-items/WP-15/OPENAPI-01/third-party-notice.md)，checker 禁止自动取得远端引用。

在仓库根目录执行以下命令；测试使用独立 HOME/XDG 与环境白名单，不继承日常认证。`GOMODCACHE`/`GOCACHE` 只复用本机公开构建缓存，依赖需要预先准备。输出为 fixture JSON，无真实凭据。

```sh
mkdir -p .fusion-dev/openapi-check
fusion_test_root=$(mktemp -d)
fusion_sdk=$(xcrun --show-sdk-path)
env -i PATH="$HOME/.local/bin:/usr/bin:/bin" \
  HOME="$fusion_test_root" XDG_CONFIG_HOME="$fusion_test_root/config" \
  XDG_CACHE_HOME="$fusion_test_root/cache" XDG_DATA_HOME="$fusion_test_root/data" \
  TMPDIR="$fusion_test_root" GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  GOMODCACHE="$HOME/go/pkg/mod" GOCACHE="$HOME/Library/Caches/go-build" \
  CGO_ENABLED=1 CC=/usr/bin/clang CXX=/usr/bin/clang++ SDKROOT="$fusion_sdk" \
  MAGPIE_NO_STATS=1 DO_NOT_TRACK=1 \
  go test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
    ./internal/fusion/api -run '^TestImplementedAPIContractSamples$' -args \
    -fusion-api-contract-out="$PWD/.fusion-dev/openapi-check/samples.json"
python3 scripts/fusion/check-openapi.py \
  --standard-schema docs/fusion/work-items/WP-15/OPENAPI-01/openapi-3.1-2022-10-07.json \
  --samples .fusion-dev/openapi-check/samples.json
```

预期输出：24 paths、28 operations、49 handler samples、28 covered operations、6 negative schema cases；生产注册为 false。测试失败时先保留日志和临时目录，依据具体字段/行为修正合同或实现，不能删除不匹配样本来获得成功。成功后可用 `rmdir` 删除上述空临时目录；若 Go 测试产生缓存子目录，保留或按本人清理策略处理，不宽泛删除其他临时目录。

原 17 路径/19 操作的历史证据见 [OPENAPI-01/summary.md](../work-items/WP-15/OPENAPI-01/summary.md)。当前默认层扩展及全量 Fusion race、CLI/GUI build、full vet 和固定 Native 合成回归证据见 [DEFAULTS-01/summary.md](../work-items/WP-15/DEFAULTS-01/summary.md)。标准校验与 fixture 样本不能证明产品 listener 或真实账号准入。

最新任务控制与 Native 停止验证见 [PAUSE-API-01](../work-items/WP-15/PAUSE-API-01/summary.md)，TaskControlReply 的可选安全 Run/固定 error 及 409 不确定暂停意图样本已纳入 checker。普通 Continue 的 409 仍为 ErrorResponse，未授予 Native resume。

整项任务取消扩展见 [CANCEL-API-01](../work-items/WP-15/CANCEL-API-01/summary.md)：新增 task cancel 的200/202/安全409样本及重复只读；实际原 stage cancel 仍由独立路径覆盖。当前49样本/28操作不是产品注册或真实账号准入证明。
