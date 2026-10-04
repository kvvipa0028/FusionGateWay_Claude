# 原阶段启动恢复页面

组件 WP-16-START-RECOVERY-UI-01，消费[持久记录 API](task-start-api.md)。只扩展原 Magpie 任务详情：复用 `.list`、`.row`、`.profiles`、`.text.action` 与现有文本样式，新增原启动记录和两个操作按钮。原 Magpie app.css/app.js/index.html、Fusion CSS、框架、整体布局、配色与导航均未改。

## 使用流程

1. 打开已保存任务，读完当前 Task、冻结 Plan 和调用预算；明确选择阶段并点击“启动所选阶段”。页面生成一次随机 key，保存原 ready Task、Plan、role、body 和完整 Task If-Match。
2. 先 POST start-request，核对服务端准备的完整原 Task/Plan/role/key/条件。准备未确认时不发送 start。准备成功才发送同一 key、role 与原条件的 start。
3. 成功回执仍须独立读取同 run 与当前 Task，核对 run ID、原角色、attempt、generation、计划版本及完整 target。随后确认精确原记录；确认未核对时保留原请求并锁定其他操作。
4. 窗口关闭或宿主重开时，先查询登记项目的待解决记录，发现所在项目，再显示原冻结模型、路线、账号、effort、条件、key 和原 run ID。启动时仅 GET，不自动运行、确认、封存或推进下一阶段；上方当前配置不会覆盖原记录。
5. “重试原运行请求”先按原 key/条件读记录。prepared 可继续同一原启动；committed 直接独立读取原 run/当前 Task，再确认元数据，不再 POST start；acknowledged 也须独立证明原 run 后才清除本地未知状态。unknown 运行仍按 unknown 展示，确认记录不证明运行成功或停止。
6. 没有 run 回执时可明确点击“封存未启动请求”，经过确认后封存同一原身份。丢失封存回执只允许同一封存重试或精确历史回读；服务端已写入 run 时不能封存。abandoned 历史解除本地锁定，迟到原 start 被服务端拒绝。

“重新核对原启动请求”读取同一原记录，不会采用当前配置重新编译或换 key。任务提交恢复与阶段启动恢复是两条独立流程，见[提交恢复页面](task-submission-ui.md)。

## 失败边界

断线、401/403/503、错误成功结构、替换 Task/Plan/role/run ID 或独立运行读取失败均保留原身份，禁用新启动、其他运行动作、项目和配置编辑。成功结构未通过校验不会成为已知记录；不显示服务端错误正文，不把请求失败解释成运行没发生。已准备请求的 Task 条件后来变化也不清除原记录；必须明确封存或证明原关联 run。

只有“准备尚未确认、从未发送 start、没有可信 journal/run”的本地请求有一个特殊解除流程：用户明确回读得到原记录不存在，独立读取同项目/同 ID/同目标的当前 Task，证明 generation 或计划版本单调前进，再次精确 GET 确认原记录仍不存在。原条件已失效，迟到原准备或 start 被拒绝，此时才解除本地锁定。仅一次 404、当前 Task 读取失败/被替换、条件仍相同或第二次读取找到迟到准备，都不能解除锁定。这不是伪造 abandoned 历史，不删除服务端记录。

页面不使用 localStorage/sessionStorage 保存 key 或管理凭据。Management 授权继续由固定 Native 桥注入；重开恢复依赖私有 Store 和当前来源/窗口权限，不开放通用代理。浏览器回读不能替代服务端准入、停止证明、reservation 释放或整项验收。

## 复现与证据

依赖 Go1.26.3、Python3、Node、已有离线依赖与已安装 Chrome；从隔离实施工作树运行：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
PATH="$HOME/.local/bin:$PATH" python3 docs/fusion/work-items/WP-13/CALLS-01/run-go.py \
  .fusion-dev/start-ui-fixture.log test -c -mod=readonly -tags fusion,nogui \
  -o .fusion-dev/task-ui-fixture ./internal/fusion/bootstrap
NODE_PATH=/Users/zhaojianzhi/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  node --test --test-concurrency=1 internal/gui/tests/fusion-stage-editor.test.cjs
node --test internal/gui/tests/fusion-editor-model.test.cjs
```

测试使用临时私有 HOME/XDG、管理凭据、合成路线/账号/额度与真实 loopback HTTP/Store/Controller；只关闭所属浏览器与宿主，不访问日常客户端凭据。hold/success、Runtime、StopProof 为合成。跨 browser/host 重开、原 key/条件保持、丢失准备/启动/确认/封存回执、独立运行证明失败、其他项目与当前默认模型变化均由实际浏览器测试验证。

证据见 [START-RECOVERY-UI-01](../work-items/WP-16/START-RECOVERY-UI-01/summary.md)。实际 Native 点击/像素仍未验证：本次 Native 工具报告 Mac 锁屏。真实供应商模型/额度调用为零，Jev off；产品真实 Factory、账号/计费/物理池准入、工程阶段闭环与最终 T01–T60 未完成，WP-15/WP-16 与整体目标仍 in_progress。
