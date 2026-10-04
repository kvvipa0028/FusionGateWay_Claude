# 本机草稿控制服务

`fusion` tagged 二进制新增 `fusion-control --projects /private/path/projects.json [--addr 127.0.0.1:0]`。仅沿独立 HOME/XDG 的 run-dev.py 启动；在 legacy 登录、代理、网关和自动更新启动逻辑之前分派。无 tag 构建保持原有命令行为。工作流主开关仍 off，Jev off。

此服务把本机可信项目登记接入已有任务 API，供配置、默认层、预设与已有持久记录管理。**运行模式为 draft-control，execution_enabled=false**：路线始终未准入，未选择模型不能生成执行计划，默认没有注册 Controller/Native/额度采集器；后续[独立 GLM 额度宿主](glm-quota-host.md)可通过明确的三个 CLI 参数仅登记额度查询，不注册执行 Controller。启动接口在缺 Controller 时拒绝；它不代表已经可以执行真实项目。旧 Magpie model/gateway/GUI 入口继续关闭。

后续增加进程内 [execution-host.md](execution-host.md) 的可信 RuntimeFactory 接线，支持同一项目来源/路线检查、Controller 和额度生命周期。该接口不改变此草稿 CLI 的执行开关；真实 Factory/账号准入与产品入口仍待完成。共用 CloseContext 的等待超时不关闭仍被 owned 工作使用的 Store，可继续等待。

## 入口与持久状态

只接受数值地址 `127.0.0.1:0–65535`，不接受 localhost、LAN、IPv6、URL、非规范端口或附加参数。端口0由OS分配。请求实际 peer必须127.0.0.1、Host必须精确匹配最终监听地址；Management仅接受正确Bearer，Origin仅接受该服务自身http origin；不提供CORS放行或URL/cookie管理凭据。原API管理middleware继续落实stage/query credential与cross-site拒绝。

服务状态在私有 FUSION_STATE_ROOT/data/fusion-gateway/control，包含 tasks 任务库与 management.token；根/父目录由当前用户拥有且private，Git外、无symlink，不能与登记项目目录相同或嵌套。Loader/Store继续独立验证。Tasks使用现有schema5与controller lock；两个实例不能同时打开同一库。

首次使用crypto/rand生成32bytes的随机管理能力，fgm_前缀、base64url编码，0600/exclusive创建并fsync。既有文件只读验证私有owner/权限、单hardlink、普通有界文件及固定格式，不覆盖或自动旋转。管理凭据不接收argv/environment、不写stdout/stderr、不传到模型；启动JSON只返回地址、产品、draft模式及execution_enabled/quota_query_enabled/jev，不回显额度参数或私有文件位置。读写失败可能留下仅本人可访问的部分新状态，拒绝启动并由操作者检查，不能自动覆盖。

每请求重查私有来源内容/身份、项目/控制根/tasks文件夹身份与权限，以及管理文件内容/身份；当前不符返回固定503并撤销Management。后台每250ms复核也会撤销已有Management，使SSE在其原有轮询周期退出。已撤销issuer不会因文件恢复而重启权限；需停止服务并明确重新加载。此本机观测不授予Runtime权限，未来执行Resolver仍必须在实际派单目的重查。

HTTP限制为5s ReadHeader、15s Read、30s Idle、16KiB header；SSE沿既有ResponseController设置每次写deadline，不能用全服务WriteTimeout截断正常长连接。Close撤销Management、关闭listener/connections，等待Handler退出后关闭Store；重复Close安全。SIGINT/SIGTERM沿run-dev的受管Popen转发到实际二进制并等待退出，不能仅终止Python后留下服务。停止不会清空任务库、退款或把旧未知执行解释为已停；本项未启动Worker。

## 本机启动

预先准备Go1.26.3、公开依赖/cache、Xcode SDK，并按 [local-project-source.md](local-project-source.md) 准备Git外私有projects.json。模板path必须替换为本人已选定的已有项目文件夹；read=true，write默认false。以下命令创建独立开发状态，保持前台运行；不存在真实账号准入，不读取GLM key：

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/build-dev.py
fusion_control_root="$HOME/.config/fusion-control-dev"
fusion_project_source="$HOME/.config/fusion-local-registration/config/projects.json"
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/run-dev.py \
  --root "$fusion_control_root" -- \
  fusion-control --projects "$fusion_project_source" --addr 127.0.0.1:0
```

记录启动JSON的control_address。使用Ctrl+C停止；服务退出后再修改私有登记、权限或管理文件并重启。不存在自动配置热采用。当前无GUI认证流程，不把管理凭据粘贴到浏览器URL、聊天、日志或shell参数。

例如另开终端用Python仅在内存中读取私有管理文件；替换非敏感address为实际公告值（端口不是固定值），模板project ID为local-pilot：

```sh
fusion_control_root="$HOME/.config/fusion-control-dev"
fusion_control_address="http://127.0.0.1:REPLACE_PORT"
python3 - "$fusion_control_root" "$fusion_control_address" <<'PYCODE'
import json,sys,urllib.request,urllib.parse
from pathlib import Path
root,address=sys.argv[1:]
u=urllib.parse.urlsplit(address)
assert u.scheme=="http" and u.hostname=="127.0.0.1" and u.port and not u.username and not u.path and not u.query and not u.fragment
token=(Path(root)/"data/fusion-gateway/control/management.token").read_text().strip()
r=urllib.request.Request(address+"/control/v1/projects/local-pilot/configuration",headers={"Authorization":"Bearer "+token})
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with opener.open(r,timeout=5) as response:
    data=json.load(response)
    print(json.dumps({"status":response.status,"configuration_available":True}))
PYCODE
```

此例只输出状态，不输出私有配置或管理能力。错误保持拒绝，不通过改用LAN/无鉴权旧网关规避。记录核对失败原因时不公开私有文件内容。

## 复核与边界

```sh
PATH="$HOME/.local/bin:$PATH" python3 scripts/fusion/check-control-host.py
```

该脚本使用临时private HOME/XDG、空模型合成项目、实际fusion tagged CLI和run-dev启动入口。核对正确/错误管理、无绝对项目路径或管理能力输出、preview422、旧model路径404、来源改变503、SIGTERM转发及实际退出0。失败先清理本脚本拥有的隔离进程组，再删除临时目录；不操作其他进程或真实key。通过时JSON只包含布尔结果与计数。Go package的5项新增Host race tests另覆盖Host/Origin、private/symlink、source/folder/tasks/token变化、持久重启、双控制器拒绝、已有SSE撤销；CLI2项测试覆盖参数拒绝与旧命令保持。

证据见 [CONTROL-HOST-01](../work-items/WP-15/CONTROL-HOST-01/summary.md)。当前真实模型/额度调用0，未创建用户试点文件夹或安装真实项目配置；真实路线/计费/额度准入、Controller/Native/GUI和工程闭环仍待接线。草稿服务注册不能代替完整WP-15和最终Gate。
