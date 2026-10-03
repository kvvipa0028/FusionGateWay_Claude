# WP-11 阻断项

监督器安全单进程能力已验证。任意工具子进程、外部网络和原生 resume 均 unsupported。若 Native Adapter 依赖这些能力，必须保持未准入，先另行实现和验证，不能把 killpg 或子进程枚举当完整退出证明。

控制器 crash/启动记录 gap、原生身份漂移或写后失联保留占用，需 WP-24 对账；当前进程证据缓存不跨控制器重启。macOS sandbox-exec 在本机存在但本机 man 标注 DEPRECATED；不宣称跨系统/版本可用。未更改日常 Claude/Codex/Grok HOME 或导入认证。
