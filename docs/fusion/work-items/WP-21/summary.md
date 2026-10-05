# WP-21 · 真实验证和 EvidenceGate

状态：in_progress。[原工作包](../../planning/Fusion_Magpie_Fork_Agent工作包_v1.0.md#wp-21--真实验证和-evidencegate)的完整要求继续保留。

[VERIFICATION-RUNNER-01](VERIFICATION-RUNNER-01/summary.md) 完成实际 pinned executable、版本查询、只读私有产物副本、固定环境、kernel 隔离与真实停止，及 JUnit/硬失败/零测试/过期判定。Record schema 是导出合同，JSON 和模型意见不能创造 owned Result。

[VERIFIED-TESTING-01](VERIFIED-TESTING-01/summary.md) 完成实际 GLM testing 执行、持久可信 evidence receipt、硬失败 needs_review 和重启受控核验。

[只读 review 消费](../WP-22/REVIEW-ASSESSMENT-01/summary.md)已接入。未完成：后续 acceptance 消费；需要子进程的受控 toolchain backend；真实项目/供应商端到端和最终 T22/T38/T39/T57。没有 HIL/WCET/许可证或工具的条件保持 unverified。UI 按原 Magpie 做必要小范围扩展，本组件不改 UI。
