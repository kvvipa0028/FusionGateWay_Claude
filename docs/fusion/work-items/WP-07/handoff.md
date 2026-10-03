# 交接

WP-08 绑定 Executor/transport 的版本化准入证据；Lookup 不得从客户端或可变模型组构造。WP-10 提供共享重试/额度预算，WP-15 接受严格解析的请求并使用服务端 WithStage Context 调用 Dispatcher。没有 Permit 时出口拒绝；不得以永远返回 nil 的产品 gate 开放真实调用。

HTTP 的普通错误不重试。Native Adapter 只有证明未交付输出/工具/写入效果时才可返回可重试 DispatchFailure；有未知效果的执行进入对账。旧 Magpie 组行为原样保留，Fusion 旧宽松入口继续拒绝。
