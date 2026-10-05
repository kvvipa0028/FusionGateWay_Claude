# Grok 生产执行 Factory

本组件 `WP-13-FACTORY-01` 提供进程内可信 Factory，把 [受管只读 Adapter](grok-adapter.md)、[私有文件凭据](grok-private-credential.md)、[固定订阅 Forwarder](grok-subscription-forwarder.md)、Registry 准入和私有执行根组装为真实生产注册。它不证明本人已登录或账号 tier/地区/计费/额度池准入；父 WP-13 与最终 T01–T60 不升级。Jev off。

## 可信构造与登记

`GrokRuntimeConfig` 是可信 server wiring。`NewGrokRuntimeFactory` 构造零启动/零查询/零准入；登记逐项核验环境、执行根未替换、项目可读且 native route 精确为 `grok-subscription`（防止与 Codex/GLM 订阅路线互换）、Registry candidate 为 xai/grok/oauth/local_user/subscription 且地区已核验、resolved route 绑定固定 CLI 版本、非插件、subscription 计费、controlled_calls、项目声明与 admitted route 逐字段一致、私有缓存按 [FileCredential](grok-private-credential.md) 官方 issuer 作用域打开、执行根与认证根及项目互不重叠。公开入口无上游覆写缝（buildForwarder 仅包内测试）。

Grok 无独立账号 epoch 服务：身份绑定是 route 声明的账号/workspace/credential identity 加文件身份钉住，凭据轮换即 fail-closed 重新登记。共享阶段执行合同（resolver、owned handoff、硬验证、review/acceptance/rework、撤销 watcher、锁内有界 `registrationCurrent`）与 [Codex 工厂](codex-execution-factory.md)完全一致。

## 能力边界

受管 Grok Adapter 当前只支持 no-effort 只读面（read_file）：工厂要求 admitted route 声明 NoEffort，`grokFactoryTarget` 拒绝任何 effort 档位并要求编译器逐字段重建一致；effort 档位支持落地后按 route 声明放开。新增 `Adapter.ValidateLaunch` 使 writable 启动在 intent 之前拒绝（ErrUnsupported），不允许静默只读降级；resume 仍需密封 checkpoint。

## 验证、剩余和回退

[组件报告](../work-items/WP-13/FACTORY-01/summary.md)记录合成准入下的登记拒绝矩阵（13 模式含 native_route 互换）、精确副本与角色、复制前漂移拒绝（13 模式）、固定 Grok 1.0.48 完整生命周期（成功发布 durable artifact 与 Store 重开/消费端复制、inflight 取消、Registry/缓存/source 撤销实际终止 owned 进程、写入 intent 前拒绝）、preintent 拒除 mutation、受影响三包 race 与 CLI/GUI/vet。identity/model/quota 全合成，真实供应商调用 0。

```sh
go test -mod=readonly -tags fusion,nogui -race -count=1 -timeout=180s \
  ./internal/fusion/bootstrap -run 'TestGrokFactory' \
  -fusion-host-native-grok <固定 1.0.48 可执行文件>
python3 scripts/fusion/build-dev.py
```

真实登录、tier/费用/额度、effort/写入/恢复支持、产品 CLI/GUI 注册、Gate A 与最终验收仍待完成。回退可撤销该可信登记；不影响认证缓存、原 Magpie UI 或 Store schema。
