# WP-15-EVENTS-01：持久事件 SSE 与撤销

状态：本子工作项 `done`，WP-15 继续 `in_progress`。新增任务组件接口 `GET /agent/v1/tasks/{id}/events`；尚未注册到生产 listener/GUI，没有开放旧入口。

## 行为

客户端通过 Authorization Bearer 提供管理身份；重连使用 `Last-Event-ID` 的十进制 sequence。禁止 cookie/query token、stage secret 和权限 header 退路。未提供 cursor 时从 0 补读；拒绝重复/负数/溢出/非规范 cursor，以及超出数据库持久 tail 的值。cursor 只影响读取，不成为执行、继续或取消指令。

Store.EventsPage 每次最多 256 个事件，核对 task 存在、sequence 连续性与 durable tail，保持数据库顺序。缺失尾部或中间事件不会作为完整页发出。SSE 的 id 就是持久 sequence，event 为受限制的 Kind，data 是 TaskID/Seq/Kind/RunID/Generation 元数据；不转发 prompt、凭据或原始 Native 输出。heartbeat 没有 event ID，不能被当成任务完成。

连接关闭、重连和补读均不调用 StartIntent、CancelIntent、模型出口或预算扣减。已有运行 attempt、generation、事件数和共享预算继续保持。SSE EOF 也不是 Native 成功、任务验收或进程停止证明。

Manager 向通过管理鉴权的 Context 放入本 issuer 的私有标识，不包含原始管理秘密。ManagementCurrent 拒绝其他 issuer、普通/取消/超时 Context 和已撤销的管理身份。SSE 在每次发送与轮询时再次检查；撤销后不发送新数据，连接关闭。已在撤销前写入 socket 的字节不构成新的授权发送，不承诺从客户端缓存追回这些字节。撤销管理身份不等于已停止 Worker 写入。

组件默认每秒轮询、最多 8 条活动 stream；超过容量返回 429，断开后释放 slot。单连接最长 5 分钟，write deadline 为 5 秒；不支持 write deadline 的 transport 返回 503/stream_transport_unsupported，不默开无界 stream。到期后客户端凭当前授权和 sequence 重新连接。数据库读取和 socket 错误关闭当前 stream，不自动重跑任务。UI 的 fetch 解析、重连展示和生产 server 超时配置仍待接线。

## 验证

组件缺失先 RED。额外 RED 发现非空事件页掩盖了缺失 durable tail、取消的管理 Context 仍被视为有效，以及不支持 deadline 的 transport 仍开始 stream；修复后 GREEN。

8 个 SSE tests 包括实际本机 loopback HTTP/client：从 seq=1 补读 seq=2，重连只收 seq=3；断开保持合成 running attempt；撤销管理身份后插入新事件但旧 stream 不发送；8 条活动连接与第 9 条拒绝、关闭后容量恢复；cursor/凭据/控制行注入均拒绝。另用已鉴权、能 flush 但不支持 deadline 的 Recorder 验证拒绝，不发送 task 数据。测试中的 Native session 是合成标识，不代表真实供应商 Worker。

2 个 Store 页测试、1 个 issuer/context 撤销测试与完整 Fusion tagged race 173 个 top-level tests 通过；2 个父进程 helper skip，其子进程在既有 OS 测试中执行。Fusion CLI/GUI 编译和全仓 Fusion/nogui vet exit 0。测试轮询为 10 ms、生产默认为 1000 ms；没有外网 provider 或真实套餐请求。

阶段计划 If-Match、暂停/取消/继续、预设/额度与调度/GUI 接线尚未完成。此组件的 HTTP 成功不能作为最终真实工程闭环通过，WP-15 与相关最终 T 场景仍未完成。
