# WP-12-PARSE-01 · Native JSON 字段歧义修复

状态：done（仅此子工作项）；WP-12 的完整 Native 接入仍 in_progress。

GLM 协议边界验证发现 Unicode case-fold 字段别名可绕过仅 lower-case 的重复键检测；Codex 的解析器存在同一问题。`ſtatus` 和 `status` 会被 Go struct decoding 视为同名字段，之前的检测未拒绝它们。

修复 Native duplicate-key 检测，使其按 Unicode SimpleFold 的等价关系拒绝别名碰撞，并拒绝 invalid UTF-8；普通有效 Unicode 内容不受影响。没有放宽任何 route、权限或 Runtime 隔离策略。

新增回归先 RED（歧义帧被接受），再 GREEN：24 个 Codex top-level race tests 通过。日志与本次源文件 hash 随包保存。验证覆盖协议解析与已有 Codex 状态/传输回归，仍不证明真实 Native 登录、沙箱启动或全部模型调用控制。
