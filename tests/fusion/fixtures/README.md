# 离线测试 fixtures

这些标识全部为 `fixture-*`，不能作为真实账号、套餐或模型准入证据。

`internal/fusion/testsupport` 提供进程内假 HTTP 上游与记录型 Transport，只允许调用显式登记的数值 loopback listener；外部 URL、DNS 主机和其他端口在调用底层 Transport 前拒绝。没有环境代理或真实凭据发现逻辑。

假 Runtime 可用 Go 1.26.3 构建：

```sh
go build -mod=readonly -o .fusion-dev/fake-runtime ./internal/fusion/testsupport/cmd/fake-runtime
```

仅限受信任的合成 fixture。启动必须在私有临时 HOME/XDG 下设置 `FUSION_FIXTURE_RUNTIME=1`，stdin 为单个 `RuntimeRequest` JSON，stdout 为按 seq 排序的 NDJSON `RuntimeEvent`。mode 列表见 fixture-index.json；`rpc` 指此合成请求/事件协议，不冒充厂商协议。CLI 默认不运行工具。孙进程场景只更新合成 workspace 的 heartbeat；写后失联退出 70，保留 write-marker 且不发 completed。cancel_race 发 waiting 后等 release-marker；取消测试使用独立进程组并在 finally 清理。

`CreateSyntheticRepo` 复制四个固定普通文件，在临时副本内设置只读文件、私有标记及指向合成兄弟文件的软链接。源模板保留一个确定性缺陷；初始 `go test` 应失败。已在临时副本修复一次验证其测试会变绿，未改模板。禁止把 fixture workspace guard 当成正式 Runtime sandbox。

Python 原生子进程验证命令：

```sh
python3 -m unittest discover -s tests/fusion -p test_offline_runtime.py -v
```

该测试自行生成环境白名单并禁止 Go 下载依赖；并不启动 Claude Code、Codex、Grok 或 Jev。Go 单测同样应在白名单临时 HOME/XDG、`GOENV=off`、`GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off` 下运行。依赖先按 WP-01 基线准备；离线缺依赖必须报错，不能允许供应商外发。
