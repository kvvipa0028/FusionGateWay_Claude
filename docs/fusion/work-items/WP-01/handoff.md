# Handoff

WP-02 使用固定上游与现有 Go module，不自动更新 main。原 Magpie 默认 appdir、portable marker、端口、服务、update/plugin/DAV/agent takeover 必须重新隔离；基线 build-only 不能证明并行运行不污染。保留 CPATH 白名单修正和 Google OAuth 外部配置身份合同。
