# WP-01 基线验收

2026-10-03 完成：固定 commit/tree、1,229 个原版 Go 文件对照、go.mod/go.sum、原始日志 hash、GUI/CLI 构建入口和内置/插件/官方 Runtime 路径说明。当前 Go 仅含获准 OAuth A 的六处修改和两个新增文件，hash 与既有完整 nogui suite、race、vet 和双构建报告匹配。

原版六项检查继续使用 baseline/verification-results.json 与原始日志；没有重复运行未变化的检查。原 CLI 的 CPATH/SDK 失败及隔离白名单修正保留原文与原日志，不把失败覆盖成成功。源码差异核对结果见 source-comparison.json。

T51：核对 movers/Moved/KeepRetiringMoved/plugin fetch 等实际路径，明确内置测试不能替代插件测试。未执行真实插件准入或版本变更回归。T60：固定可重建源码与依赖，保留 upstream 只读与开发分支边界；未执行数据迁移、回滚或发布。这两类最终测试保留 not_run。
