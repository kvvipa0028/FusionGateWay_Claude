# Gate A 报告 — 手动可用版验收

日期：2026-10-06
基线：`fusion/development` @ `749d69b`
状态：**合成验证全部通过；三路真实准入阻塞于用户官方设备授权登录**

## 验收范围

Gate A 验证"单阶段受控工作台"：用户通过原 Magpie 界面指定项目、角色、模型，启动一次受控执行，可取消、可查看记录。不要求自动工作流、Jev 或插件自动更新。

## 三路 Runtime Adapter 端到端状态

| 路线 | 写入方式 | 固定 Native | 合成端到端 | 真实准入 |
| --- | --- | --- | --- | --- |
| GLM / Claude Code | 进程内写工具 | Claude Code 2.1.287 | 42 子场景 PASS | 未准入 |
| Codex / ChatGPT | 委托沙箱 + exec_command | Codex 0.160.0 | 8 场景 PASS | 未准入 |
| Grok / xAI | 进程内写工具 | Grok 1.0.48 | 6 场景 PASS | 未准入 |

所有合成测试在隔离 runner（Go1.26.3、race、环境白名单）下通过。真实账号/计费/额度/续期均未验证。

## 组件证据索引

### 基础设施（WP-01–WP-11，全部 done）

基线导入、上游锁定、开发隔离、五角色绑定编译、独立任务存储、入口鉴权、原版兼容回归、发布者核验、Go 工具链、数据安全。证据包在 `docs/fusion/work-items/WP-01/` 至 `WP-11/`。

### 三路 Runtime Adapter（WP-12 Codex、WP-13 Grok、WP-14 GLM）

**Codex（WP-12）**：10 组件。协议子项（PARSE/CHANNEL/CALLS/GATEWAY-CLIENT/NATIVE-ISOLATION/ADAPTER）、私有凭据、订阅 Forwarder、生产执行 Factory、写入协议投影。全部组件有 RED→GREEN 证据、有效 mutation、固定 Native lifecycle 验证。

**Grok（WP-13）**：12 组件。协议子项（PROTOCOL/CHANNEL/CALLS/ADAPTER/READ/TOOLS/ARCHIVE/RESUME/RESUME-PROBE）、私有凭据、订阅 Forwarder、生产执行 Factory、写入端到端。

**GLM（WP-14）**：11 组件。协议子项（PROTOCOL/CHANNEL/CALLS/GRANT/TOOLS/ADAPTER）、私有凭据、CN Transport、生产执行 Factory、额度宿主、系统数据。

### 产品 API 与执行宿主（WP-15）

20 组件。ControlHost、Controller、Execution API、暂停/取消/恢复、事件流、OpenAPI、预算、检查点、预设。三路 authenticated loopback HTTP 验证通过。

### 阶段选择 UI（WP-16）

15 组件。原 Magpie 主界面接入、阶段编辑器、Providers 页面、任务工作台、提交回执、恢复页面、运行控制、配额观察区、计划修订。浏览器与 Native 桥验证通过。

### 真实登录准备（WP-17）

2 组件。发布者核验（Codex/Grok/Claude Apple Developer ID + 官方 release SHA256）和隔离官方设备登录入口。登录挑战已观察但用户未完成授权。

### 工作流闭环（WP-19–WP-22）

16 组件。工作流定义/冻结/批准、代码冻结与交接、持久产物索引、验证运行器、Go backend、审查证据消费、模型验收决策、人工决定 Store、人工控制 API、尝试限制、一次有限返工、项目独立性。

## 已验证的安全边界

- 外层 supervisor profile：readonly 路线 deny-default 隔离；Codex writer 委托内层沙箱
- 可执行文件 SHA256 pin（Codex/Grok/Claude 全部固定版本化路径）
- 私有执行根（home/config/cache/data/tmp 独立 0700 目录）
- 环境白名单（无 ambient 凭据泄漏）
- 模型通道 localhost-only（TCP4+TCP6 共享 lease）
- 凭据只存 Git 外 0600 私有文件，每次 Load 重查文件身份
- 入口鉴权：Management bearer token + 阶段 bearer + 模型 grant 三层
- 预算持久化：每次模型调用消耗持久共享预算

## 阻断项清单

| # | 阻断项 | 影响 | 解除条件 |
| --- | --- | --- | --- |
| B1 | 本人官方设备授权登录未完成 | 三路线无法执行真实模型调用；无法验证账号/tier/计费/额度/续期 | 用户运行 `login-codex.command` 和 `login-grok.command` 完成官方设备授权 |
| B2 | Codex implementation turn 终态报 interrupted | Codex 写入的完整五阶段闭环差最后一步 | 需调查 native 的 turn status 或 driver 的 observation（命令已成功 exit 0） |
| B3 | Grok effort 档位不支持 | Grok 路线只能 NoEffort 模式 | 需要真实模型 metadata（native 给未知模型剥离 reasoning_effort） |
| B4 | 产品 CLI 草稿不执行任务 | 只能通过 GUI 发起阶段执行 | CLI 集成待做 |
| B5 | 实际 Native UI 点击验证 | 部分 UI 组件只有浏览器/桥验证，无桌面像素验证 | 用户桌面解锁后执行 |
| B6 | 真实工程 Smoke 未执行 | 五阶段完整闭环用真实模型验证未做 | B1 解除后 |

## alpha 版本限制

1. 仅支持单阶段执行：设计→实施→测试→审查→验收，每阶段由用户明确推进，无自动串联。
2. 仅支持本机 macOS（darwin/arm64）；未承诺跨平台。
3. 三路真实准入需用户完成官方设备授权；未登录时所有模型调用被拒绝。
4. Codex writer 委托架构下，外层 supervisor profile 对 writer 启动不做 Seatbelt 隔离（由 native 内层沙箱接管命令隔离）。
5. Jev 旁路、自动工作流、插件自动更新全部 off。
6. 原版 Magpie 的聊天、Sessions、Gateway 功能不受影响但未与 Fusion 集成。

## 下一步

1. 用户完成官方设备授权登录 → 解除 B1
2. 三路真实 Smoke Test → WP-17 完成
3. B2/B3/B4/B5 逐项解除
4. Gate A 真实验证通过 → WP-18 done → 进入 M3 五阶段闭环
