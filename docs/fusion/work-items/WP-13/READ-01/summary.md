# WP-13-READ-01：受控文本读取与 Native 关联

基础 HEAD：`9769a72d2b235d9bc18c0a9e8412f2957a4bbf41`。完成可信 opt-in ReadTools：响应进入 Native 前的项目路径/文件快照批准、Native tool trace 验证、下一轮 HTTP 关联和准确模型历史字段检查。未注册生产 Native Worker/Forwarder、未提升真实准入，parent WP-13 in_progress。合同：[grok-read-tools.md](../../../contracts/grok-read-tools.md)。旧 [TOOLS-01](../TOOLS-01/summary.md) 及 [CALLS-01](../CALLS-01/summary.md) 证据保留，按各自拥有的提交树验证。

先执行尚无 ReadTools/NewReadSession 的 [RED](wp13-read-red.log)。初版发现 orphan empty id 使结果查找 panic，新增实际失败用例 [RED](wp13-read-orphan-red.log) 后在使用 record 前拒绝孤立结果。JSON 转义的 stage grant 在 request/response 能穿过原来的 raw bytes 检查：[RED](wp13-read-secret-red.log)；改为同时检查已解码字符串/嵌套 JSON，保护保留并加严。Missing-type 的首次测试没有真正删除 json.Marshal 排在末尾的 type 字段，[harness RED](wp13-read-type-harness-red.log) 记录；修正 mutation 输入，未改测试预期或放宽 parser。

实际 Native 的首次受控读取在后续 HTTP 返回400：[Native RED](wp13-read-native-red.log)。[metadata-only debug](wp13-read-native-debug.log) 确认文件文本字节完全匹配，但 assistant 带 model_id 字段。新增只允许等于冻结模型的 metadata 校验，不接受未知字段或将其当路由覆盖。Native GREEN 及相应正/负回归覆盖了这一精确字段。

十项新增 Host tests 覆盖完整 grant→trace→continuation，14种非法/私密/过大路径与文件，10种结果/身份/文件变化，scope精确绑定/关闭，orphan空id，11种Native trace异常，model_id正/负，解码授权反射，7种完整/分片/非法function call和root replacement。原有默认文本模式测试保持拒绝工具。未删除/弱化已有断言，没有源文本或凭据进入错误/Outcome。

实际 [13行格式调查](format/characterization.json) 运行固定 Native；首行与每10th行编号、保留末尾newline、FileContent各字段确认。原始 stdout/stderr 在format目录，没有完整保存厂商system prompt。它是无scope的格式诊断，安全性不从它推出。其 helper 是此前脚本的13行 owned fixture 变体。

正式 [Native全回归](wp13-read-native-final.log) 在当前最终源码执行3个top-level/8个场景：原有正常2HTTP、标题模型不一致、第二次预算不足、隐式工具、429 retry，以及默认文本scope私有config攻击、新controlled_read与controlled_private_config。两个新场景分别证明：

| 场景 | 真实固定 Native/真实 Store-Scheduler，假上游与合成准入 | 结论 |
| --- | --- | --- |
| controlled_read | 读取自己创建的13行项目文件；tool trace/后续HTTP/终态一致，Native exit0 | 3 HTTP/Permit/usedCalls（title1+main2），end.modelCalls=2，scope/Gate ready；独立准入/停止flags false |
| controlled_private_config | 同样read_file指向GROK_HOME config | 2 HTTP/usedCalls后响应拒绝，Native exit1；没有tool_call/update，没有stage grant回显 |

[Targeted](wp13-read-green.log)：34 PASS/3 SKIP/0 FAIL；[Fusion race](wp13-read-fusion-tagged-race.log)：399 PASS/13 SKIP/0 FAIL；[CLI/GUI/full tagged vet](build-results.json) exit0。SKIP 属于显式 Native 环境诊断，本项上述 Grok 8个场景另行执行；其它 Native 诊断未重跑，不能记为本项新证明。Go1.26.3，私有HOME/XDG/env白名单；schema/Store/API、GLM/Codex gate和准入状态保持原状，仅新增可选工具链并强化Grok JSON credential边界。

复现：macOS/arm64、Go1.26.3、clang/SDK、公开 module/build cache、sandbox-exec、固定 binary/hash；无需登录/API key。使用前项记录的临时环境 helper：

```sh
set -e
mkdir -p .fusion-dev/implementation
helper=docs/fusion/work-items/WP-13/CALLS-01/run-go.py
python3 "$helper" .fusion-dev/implementation/read-native-recheck.log \
  test -race -v -count=1 -timeout=150s -mod=readonly -tags fusion,nogui \
  ./internal/fusion/runtime/grok -run '^TestGrok(CallGatePinnedNative|ControlledReadPinnedNative)' \
  -fusion-native-grok /Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64
python3 "$helper" .fusion-dev/implementation/read-fusion-recheck.log \
  test -race -v -count=1 -timeout=60s -mod=readonly -tags fusion,nogui ./internal/fusion/...
python3 scripts/fusion/build-dev.py
```

未提供 Native flag 时相应测试 SKIP；提供却 pin/协议不符应失败。目录/Store/监听器按用例清理；Native实际wait，但不等于生产Supervisor StopProof。原始 logs whitespace保留，diff排除*.log；JSON/NDJSON/Python/gofmt/local links/hash分别检查，源文件与packet hash见 [artifacts.json](artifacts.json)，正式 counts/flags 见 [test-results.json](test-results.json)。

范围限制：当前只批准 target_file 的 clean text Read，大小/行数/总缓存/调用数有合同中的显式界限；offset/limit、二进制/转换、其他工具/多工具、写权限、Resume和生产启动/取消仍未交付。Unix下controller path/file核验不是Worker隔离，也不能独自防止主机并发换文件后Native物理读取；不同/敏感结果不能转发或作为可信产物。生产Adapter须正确接入Source/权限/保密stdout处理/Worker并验证取消后停止。真实模型/额度调用0，没有真实凭据使用，Jev off。
