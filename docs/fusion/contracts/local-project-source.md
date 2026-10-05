# 本机项目登记来源

`internal/fusion/bootstrap` 提供只读的本机启动配置加载组件，供后续产品 bootstrap 使用。已由后续 [control-host.md](control-host.md) 接入独立 fusion-control 草稿 CLI/listener，GUI及真实执行尚未接线；不会启动 Worker、读取 API key、查询额度或调用模型。Jev off。配置声明与真实路线准入分别验证；只有声明不能创建可执行计划。

## 文件与权限

入口 `Load(path)` 只接受绝对、clean、无 symlink 的 `projects.json`。文件必须由当前用户拥有、无 group/other 权限、单一 hardlink、普通文件，大小 2–262144 bytes。直接父目录及上一级目录必须由当前用户拥有且无 group/other 权限；所有路径祖先不得存在 `.git`，因此私有登记文件必须放在 Git 仓库之外。文件不能位于登记的项目目录内，项目也不能位于配置目录内。

`openat`/`O_NOFOLLOW` 沿目录 FD 遍历，文件以 nonblocking 打开并先核对类型；FIFO 不会阻塞加载。读取前后核对身份、大小与修改时间。JSON 必须为 UTF-8、唯一对象字段（包括大小写及 Unicode fold 别名）、无尾随值、无未知字段，深度至多 32，每对象至多 256 字段。错误统一为 `ErrRegistration`，不返回私有路径、账号或底层错误。Loaded 的 String/GoString 脱敏；调用方也不得日志输出返回的内部 Project。

项目 `path` 必须是已经存在、当前用户拥有且无 group/other 写权限的绝对 canonical 文件夹；项目本身可以是 Git 仓库。加载不创建或修改文件夹，不扫描项目内容。不同项目不能共享文件夹身份或互相嵌套。本机文件夹 path 与 provider 的 workspace 标识是不同字段，不能替换使用。

macOS/Linux 使用上述 FD 检查；本次仅验证 macOS/arm64。其他平台返回固定失败，不宣称跨平台运行已验证。

## 配置合同

| 字段 | 约束与行为 |
|---|---|
| schema_version / revision | 1 / 正整数；只作本机来源版本，不表示路线通过验证 |
| projects | 1–32 项，唯一 id、非空 name、已有 path；read 必须 true，write 默认 false 且必须明确声明 |
| routes | 最多 64 个具体 ID/revision；每项目只引用自身获登记的 RouteRef |
| native_route | 仅 codex-chatgpt、grok-subscription、glm-cn-claude；与内置目录精确匹配 |
| model / account / workspace / credential_identity / runtime_version | 有界、无空白/控制符/路径分隔符的明确声明；拒绝 unknown、undisclosed、default；credential_identity 是非密钥标识，不能填 API key |
| efforts / default_effort / no_effort | 至多16个不重复声明；默认档位必须在列表中；no_effort=true 不得同时声明 effort/default |
| global / project.layer | 沿用阶段 Layer 合同，展开三个分组为五角色；绑定只能引用本项目登记的精确模型与路线版本 |
| independence | 可选角色对列表，每对 first/second 为已知且不同的角色；至多10对，拒绝重复（含反向）；canonical 顺序输出。只约束具体 ResolvedModel，默认无模型独立性硬约束；规则随任务冻结，不能由任务 body 覆盖，详见 [项目独立性](project-independence.md) |
| max_calls / max_reworks | 省略默认 50 / 1；明确填写分别为 1–1000 / 0–1，calls=0 拒绝 |

所有 route 均导出为 `Admitted=false`、`BillingKnown=false`、`LockEnforcement=unverified`，不携带 capabilities/plugin 准入。BillingPath 仅由内置路线目录派生为候选计费路径，不证明账号订阅有效。配置不接受 admitted、billing_known、证据、StopProof、API key、URL 或 argv。具体声明也不能证明上游 account/workspace/模型/推理档位可用。

未选择任何模型可以保存为 routes=[]、空 Layer；五角色均 inherit，不编造默认模型。真实执行仍需独立的身份、权限、Runtime、锁定、计费及额度准入。管理 HTTP 可以读取这些未准入配置，但已登记的 locked route 在 preview 时仍返回 422 / route_revision_not_admitted。项目 path 和本机 read/write 不进入 HTTP ProjectConfiguration，不能通过 task body 改写授权。

## 快照与失效

`Loaded.Project(id)` / `Projects()` 返回深复制，修改返回值不会改变内部登记；这两个 getter 只读快照，不能授予执行权限。`Current(id)` 重新核对来源 inode/device、原始内容摘要和项目文件夹 inode/device/当前权限，任何当前不符均返回 false。配置替换（即便内容相同）、内容变化、文件夹替换、symlink 或公开写权限需要停止新派单并显式重新加载验证，不能自动采用新内容。摘要仅在内存中，不发布私有配置 fingerprint。

未来 Controller Resolver/工具授权必须在实际目的与派单时调用 Current，并独立落实文件访问边界；本组件的布尔声明和一次目录检查不能替代运行中的 FD/路径检查。已冻结任务不能因重新加载而被静默换模型、账号或执行路径。

## 模板准备

仓库模板 [projects.example.json](../examples/projects.example.json) 无模型或凭据，文件名故意不满足 Load。准备时复制到私有目录并命名 projects.json，填写本人明确选定的已有项目绝对路径；保持 read=true / write=false，只有明确允许实施写入时才改 write。此准备不启动应用，也不代表完成产品接线。

例如先创建 Git 外的两个私有目录；不要覆盖已有配置：

```sh
umask 077
fusion_config_root="$HOME/.config/fusion-local-registration"
mkdir -p "$fusion_config_root/config"
chmod 700 "$fusion_config_root" "$fusion_config_root/config"
if [ ! -e "$fusion_config_root/config/projects.json" ]; then
  cp docs/fusion/examples/projects.example.json "$fusion_config_root/config/projects.json"
  chmod 600 "$fusion_config_root/config/projects.json"
fi
```

以上只是操作者准备示例，本轮未在本机实际创建该配置或试点项目。填写前检查父路径无 symlink/Git；模板占位 path 必须替换。不要将 key、完整私有配置或账号来源提交到 Git。凭据继续放在独立私有文件，由对应 Adapter 使用。

## 独立复核

在仓库根目录执行；需预先准备 Go 1.26.3、公开 module/cache 和 Xcode SDK。环境清空且 HOME/XDG/TMP 独立；不读取日常认证或真实 key。

```sh
fusion_test_root=$(mktemp -d)
fusion_sdk=$(xcrun --show-sdk-path)
env -i PATH="$HOME/.local/bin:/usr/bin:/bin" \
  HOME="$fusion_test_root" XDG_CONFIG_HOME="$fusion_test_root/config" \
  XDG_CACHE_HOME="$fusion_test_root/cache" XDG_DATA_HOME="$fusion_test_root/data" \
  TMPDIR="$fusion_test_root" GOENV=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  GOMODCACHE="$HOME/go/pkg/mod" GOCACHE="$HOME/Library/Caches/go-build" \
  CGO_ENABLED=1 CC=/usr/bin/clang CXX=/usr/bin/clang++ SDKROOT="$fusion_sdk" \
  CGO_CFLAGS="-O2 -g -isysroot $fusion_sdk" \
  CGO_CXXFLAGS="-O2 -g -isysroot $fusion_sdk" CGO_LDFLAGS="-isysroot $fusion_sdk" \
  MAGPIE_NO_STATS=1 DO_NOT_TRACK=1 \
  go test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/bootstrap -run '^TestProjectSource'
```

预期8项顶层测试 PASS；包含非阻塞 FIFO、symlink/hardlink/Git、字段别名、预算边界、来源/目录替换、深复制及实际受鉴权 Handler 的未准入拒绝。失败保留日志，不调整权限保护或准入断言掩盖失败。仅产生本人临时测试数据，可按本人策略清理该临时目录。

证据见 [PROJECT-SOURCE-01](../work-items/WP-15/PROJECT-SOURCE-01/summary.md)。产品执行 Controller/GUI、真实项目选取、路线验证、额度和工程闭环仍待接线，此组件完成不等于 WP-15 或整体验收完成。
