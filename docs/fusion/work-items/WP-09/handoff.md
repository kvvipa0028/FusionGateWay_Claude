# 交接

WP-10 只消费 exact identity/generation、完整、来源足够新且 pool 已核验的 model quota，执行前重算 State。unknown、stale、unverified、auth_required、unsupported、zero 均不自动转入付费路径。

Native Reader 单独核验查询准入，并显式传 Meta；不得以收到响应的时间填观察时间。账号/workspace/region 切换先 Retire，再 SetIdentity 新 generation。Broker 超时不证明进程已停止；WP-11 负责 Native Reader 的外部进程终止。Quota.Raw 只含已知额度字段，不保存完整认证/错误响应。
