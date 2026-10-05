const assert=require("node:assert/strict");
const fs=require("node:fs/promises");
const path=require("node:path");
const os=require("node:os");
const {spawn,execFileSync}=require("node:child_process");
const {once}=require("node:events");
const {test}=require("node:test");
const {chromium}=require("playwright");
const repo=path.resolve(__dirname,"../../..");
const binary=path.join(repo,".fusion-dev/fusion-gateway-cli");
const models={a:"fixture-model-a",b:"fixture-model-b",c:"<svg>"};
const key=id=>JSON.stringify(["fixture-"+id,1,models[id]]);
async function fixture(t,admitted=false,withSecondProject=false,executionMode="",withQuota=false,withWorkflow=false){
 const root=await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(),"fusion-stage-ui-")));
 await fs.chmod(root,0o700);
 for(const name of ["workspace","private","private/config"])await fs.mkdir(path.join(root,name),{mode:0o700});
 const doc={schema_version:1,revision:1,global:{roles:{design:{mode:"locked",route:{id:"fixture-a",revision:1},model:models.a,effort:{mode:"none"}}}},
 routes:Object.entries(models).filter(([id])=>!admitted||id!=="c").map(([id,model])=>({id:"fixture-"+id,revision:1,native_route:"glm-cn-claude",model,account:"fixture-account-"+id,workspace:"fixture-workspace",credential_identity:"fixture-identity-"+id,runtime_version:"fixture-runtime",no_effort:true})),
 projects:[{id:"synthetic-ui",name:"界面验证项目",path:path.join(root,"workspace"),read:true,write:false,routes:Object.keys(models).filter(id=>!admitted||id!=="c").map(id=>({id:"fixture-"+id,revision:1})),layer:{}}]};
 if(withWorkflow)for(const role of ["implementation","testing","review","acceptance"])doc.global.roles[role]=structuredClone(doc.global.roles.design);
 if(withSecondProject){await fs.mkdir(path.join(root,"workspace-other"),{mode:0o700});doc.projects.push({...doc.projects[0],id:"synthetic-ui-other",name:"另一个界面验证项目",path:path.join(root,"workspace-other")})}
 const source=path.join(root,"private/config/projects.json");await fs.writeFile(source,JSON.stringify(doc),{mode:0o600});
 const go=execFileSync("which",["go"],{encoding:"utf8"}).trim();
 const control=path.join(root,"state/data/fusion-gateway/control");
 if(admitted)for(const name of ["state","state/data","state/data/fusion-gateway"])await fs.mkdir(path.join(root,name),{mode:0o700});
 const spawnHost=()=>admitted?spawn(path.join(repo,".fusion-dev/task-ui-fixture"),["-test.run=^TestTaskUIFixtureProcess$","-fusion-ui-fixture-source",source,"-fusion-ui-fixture-root",control,...(executionMode?["-fusion-ui-fixture-execution",executionMode]:[]),...(withQuota?["-fusion-ui-fixture-quota"]:[])],{cwd:repo,env:{PATH:"/usr/bin:/bin",HOME:path.join(root,"private"),XDG_CONFIG_HOME:path.join(root,"private/config"),XDG_DATA_HOME:path.join(root,"private/data"),XDG_CACHE_HOME:path.join(root,"private/cache"),TMPDIR:path.join(root,"private")},stdio:["ignore","pipe","pipe"]}):spawn("python3",[path.join(repo,"scripts/fusion/run-dev.py"),"--root",path.join(root,"state"),"--binary",binary,"--go",go,"--","fusion-control","--projects",source],{cwd:repo,stdio:["ignore","pipe","pipe"]});
 let child=spawnHost();
 let output="",errors="",browser=null,token=null;
 t.after(async()=>{
  try{if(browser)await browser.close()}finally{
   try{
    if(child.exitCode===null){const done=once(child,"exit");let timer;child.kill("SIGTERM");try{await Promise.race([done,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error("fixture shutdown deadline")),8000)})])}finally{clearTimeout(timer)}}
    assert.equal(child.exitCode,0);if(token){assert.ok(!output.includes(token));assert.ok(!errors.includes(token));}
   }finally{if(child.exitCode!==null||child.signalCode!==null)await fs.rm(root,{recursive:true,force:true})}
  }
 });
 const announce=async current=>{
  current.stderr.on("data",b=>errors+=b);let runOutput="";
  return new Promise((resolve,reject)=>{
   const timer=setTimeout(()=>reject(Error("fixture startup deadline")),15000);
   current.once("exit",()=>{clearTimeout(timer);reject(Error("fixture exited before startup"))});
   current.stdout.on("data",b=>{output+=b;runOutput+=b;for(const line of runOutput.split("\n").slice(0,-1)){if(admitted&&!line.startsWith("{"))continue;clearTimeout(timer);try{resolve(JSON.parse(line))}catch{reject(Error("invalid startup response"))}break}});
  });
 };
 const announcement=await announce(child);
 let origin=announcement.control_address;
 if(admitted){assert.equal(announcement.synthetic_fixture,true);assert.equal(announcement.execution_supported,!!executionMode)}else assert.equal(announcement.execution_enabled,false);
 assert.equal(announcement.jev,"off");
 token=(await fs.readFile(path.join(root,"state/data/fusion-gateway/control/management.token"),"utf8")).trim();
 for(const name of ["index.html","editor.mjs","model.mjs","workbench.mjs","quota.mjs","editor.css","app.css"]){
  const response=await fetch(origin+"/fusion/"+name,{headers:{Authorization:"Bearer "+token},redirect:"error"});
  assert.equal(response.status,200);
  const actual=await response.text(),expected=await fs.readFile(path.join(repo,"internal/gui/assets",name==="app.css"?name:path.join("fusion",name)),"utf8");
  assert.ok(actual===expected,"stage bundle differs from current source");
 }
 browser=await chromium.launch({channel:"chrome",headless:true});
 const newPage=async(beforeLoad=null,viewport={width:1140,height:1000})=>{
  const context=await browser.newContext({viewport,extraHTTPHeaders:{Authorization:"Bearer "+token}});
  await context.route("**/*",r=>new URL(r.request().url()).origin===origin?r.continue():r.abort());
  const page=await context.newPage();
  const errors=[];page.on("pageerror",e=>errors.push(e.message));
  if(beforeLoad)await beforeLoad(context);
  await page.goto(origin+"/fusion/");await page.locator("#status").filter({hasText:/已载入当前配置|已恢复原任务请求|已恢复原阶段启动请求/}).waitFor();
  return {page,context,errors};
 };
 return {get origin(){return origin},newPage,async restart(){
  assert.ok(admitted,"restart only owns the synthetic test host");
  const done=once(child,"exit");child.kill("SIGTERM");let timer;
  try{await Promise.race([done,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error("fixture restart shutdown deadline")),8000)})])}finally{clearTimeout(timer)}
  assert.equal(child.exitCode,0);child=spawnHost();const next=await announce(child);
  assert.equal(next.synthetic_fixture,true);assert.equal(next.execution_supported,!!executionMode);assert.equal(next.jev,"off");origin=next.control_address;
  token=(await fs.readFile(path.join(control,"management.token"),"utf8")).trim();
 },async request(url,method="GET",body=null,tag=null,key=null){
  const response=await fetch(origin+url,{method,redirect:"error",headers:{Authorization:"Bearer "+token,"Content-Type":"application/json",...(tag?{"If-Match":tag}:{}),...(key?{"Idempotency-Key":key}:{})},...(body?{body:JSON.stringify(body)}:{})});
  return {status:response.status,body:await response.json(),etag:response.headers.get("ETag")};
 }};
}
async function choose(page,label,id){
 await page.getByLabel(label+"模式",{exact:true}).selectOption("locked");
 await page.getByLabel(label+"模型",{exact:true}).selectOption(key(id));
 await page.getByLabel(label+"推理档位",{exact:true}).selectOption("none");
}
async function status(page,text){await page.getByRole("status").filter({hasText:text}).waitFor()}
test("successful synthetic server preview renders the actual role-indexed bindings without execution",async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();
 await page.getByLabel("配置范围").selectOption("task");
 await page.getByLabel("任务目标").fill("合成设计预览，不启动 Runtime");
 const response=page.waitForResponse(r=>r.url().endsWith("/control/v1/tasks/preview"));
 await page.getByRole("button",{name:"预览单阶段任务",exact:true}).click();
 const actual=await response;assert.equal(actual.status(),200);
 const preview=await actual.json();assert.equal(preview.plan.bindings.design.target.resolved_model,models.a);
 await page.getByRole("status").filter({hasText:"已核对冻结阶段计划"}).waitFor({timeout:2000});
 assert.equal(await page.getByRole("region",{name:"计划预览"}).getByText("设计：fixture-model-a",{exact:true}).count(),1);
 const tasks=await f.request("/control/v1/projects/synthetic-ui/tasks");assert.equal(tasks.status,200);assert.deepEqual(tasks.body.tasks,[]);
 assert.deepEqual(errors,[]);
 await page.getByLabel("任务目标").fill("修改后的目标");assert.equal(await page.getByRole("region",{name:"计划预览"}).isVisible(),false);
 await page.getByLabel("设计模式",{exact:true}).selectOption("auto");
 await page.getByLabel("设计添加批准候选",{exact:true}).click();
 await page.getByLabel("设计候选1模型",{exact:true}).selectOption(key("b"));
 await page.getByLabel("设计候选1推理档位",{exact:true}).selectOption("none");
 await page.getByRole("button",{name:"预览单阶段任务",exact:true}).click();await status(page,"已核对冻结阶段计划");
 assert.equal(await page.getByRole("region",{name:"计划预览"}).getByText("设计：批准候选：fixture-model-b",{exact:true}).count(),1);
 await page.getByLabel("本次阶段",{exact:true}).selectOption("review");assert.equal(await page.getByRole("region",{name:"计划预览"}).isVisible(),false);
 await page.getByLabel("本次阶段",{exact:true}).selectOption("design");
 await context.route("**/control/v1/tasks/preview",async route=>{const actual=await route.fetch(),body=await actual.json();body.plan.bindings=[];await route.fulfill({response:actual,json:body})});
 await page.getByRole("button",{name:"预览单阶段任务",exact:true}).click();await status(page,"请求未完成");
 assert.equal(await page.getByRole("region",{name:"计划预览"}).isVisible(),false);
 assert.deepEqual(errors,[]);
});
test("actual private CLI/API: independent roles, fold/reload, explicit overwrite and safe model text",async t=>{
 const f=await fixture(t),{page,errors}=await f.newPage();
 await page.emulateMedia({colorScheme:"light"});
 assert.deepEqual(await page.evaluate(()=>({font:getComputedStyle(document.body).fontSize,header:document.querySelector(".top").getBoundingClientRect().height,background:getComputedStyle(document.body).backgroundColor})),{font:"13px",header:46,background:"rgb(244, 244, 246)"});
 await page.emulateMedia({colorScheme:"dark"});
 assert.equal(await page.evaluate(()=>getComputedStyle(document.body).backgroundColor),"rgb(26, 26, 30)");
 await page.emulateMedia({colorScheme:"light"});
 await page.getByLabel("实施与测试展开").click();
 await choose(page,"实施","a");await choose(page,"测试","b");
 await page.getByLabel("审查与验收展开").click();await choose(page,"审查","a");await choose(page,"验收","c");
 await page.getByLabel("实施与测试展开").click();
 await page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(page,"已保存版本 1");
 const saved=await f.request("/control/v1/projects/synthetic-ui/defaults");
 assert.equal(saved.body.layer.roles.testing.model,models.b);assert.equal(saved.body.layer.roles.acceptance.model,models.c);
 await page.reload();await status(page,"已载入当前配置");
 await page.getByLabel("实施与测试展开").click();await page.getByLabel("审查与验收展开").click();
 assert.equal(await page.getByLabel("测试模型",{exact:true}).inputValue(),key("b"));
 assert.equal(await page.getByLabel("验收模型",{exact:true}).inputValue(),key("c"));
 assert.equal(await page.locator(".group svg").count(),0);
 page.once("dialog",d=>d.dismiss());await page.getByLabel("实施与测试共用").click();
 assert.equal(await page.getByLabel("测试模型",{exact:true}).inputValue(),key("b"));
 page.once("dialog",d=>d.accept());await page.getByLabel("实施与测试共用").click();
 assert.equal(await page.getByLabel("测试模型",{exact:true}).inputValue(),key("a"));
 await page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(page,"已保存版本 2");
 await page.locator("#view-fusion").evaluate(n=>n.scrollTop=0);
 await page.screenshot({path:path.join(repo,".fusion-dev/implementation/stage-editor-desktop.png"),fullPage:true});
 const mobile=await f.newPage(null,{width:390,height:844});
 await mobile.page.getByLabel("实施与测试展开").click();await mobile.page.getByLabel("审查与验收展开").click();
 assert.ok(await mobile.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await mobile.page.locator("#view-fusion").evaluate(n=>n.scrollTop=0);
 await mobile.page.screenshot({path:path.join(repo,".fusion-dev/implementation/stage-editor-mobile.png"),fullPage:true});
 assert.deepEqual(mobile.errors,[]);
 assert.deepEqual(errors,[]);
});
test("actual API: stale editor keeps draft; lost acknowledgement retries the identical version/body",async t=>{
 const f=await fixture(t),a=await f.newPage(),b=await f.newPage();
 await choose(a.page,"设计","a");await choose(b.page,"设计","b");
 await a.page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(a.page,"已保存版本 1");
 await b.page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(b.page,"其他窗口更新");
 assert.equal(await b.page.getByLabel("设计模型",{exact:true}).inputValue(),key("b"));
 await choose(a.page,"设计","b");
 let sent=null,retries=[];
 await a.context.route(f.origin+"/control/v1/projects/synthetic-ui/defaults",async r=>{
  if(r.request().method()!=="PUT")return r.continue();
  retries.push({tag:r.request().headers()["if-match"],body:r.request().postData()});
  if(!sent){sent=retries[0];await r.fetch({maxRedirects:0});await r.abort("failed");return}
  await r.continue();
 });
 await a.page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(a.page,"保存状态未确认");
 assert.equal(await a.page.getByLabel("设计模式",{exact:true}).isDisabled(),true);
 await a.page.getByRole("button",{name:"重试同一保存",exact:true}).click();await status(a.page,"已保存版本 2");
 assert.equal(retries.length,2);assert.deepEqual(retries[0],retries[1]);
 const saved=await f.request("/control/v1/projects/synthetic-ui/defaults");assert.equal(saved.body.revision,2);
 assert.equal(saved.body.layer.roles.design.model,models.b);
 assert.deepEqual(a.errors,[]);assert.deepEqual(b.errors,[]);
});
test("actual API: explicit preset version, auto candidates and single-role preview remains unadmitted",async t=>{
 const f=await fixture(t);
 const binding=id=>({mode:"locked",route:{id:"fixture-"+id,revision:1},model:models[id],effort:{mode:"none"}});
 const presetPath="/control/v1/projects/synthetic-ui/presets/fixture-preset";
 assert.equal((await f.request(presetPath,"PUT",{name:"Fixture preset",layer:{roles:{design:binding("a"),testing:binding("b"),acceptance:binding("c")}}},'"0"')).status,201);
 assert.equal((await f.request(presetPath,"PUT",{name:"New head",layer:{roles:{design:binding("b")}}},'"1"')).status,201);
 const {page,context,errors}=await f.newPage();await page.getByLabel("配置范围").selectOption("task");
 await page.getByLabel("预设",{exact:true}).selectOption("fixture-preset");await page.getByLabel("预设版本").fill("1");
 await page.getByRole("button",{name:"载入此版本",exact:true}).click();await status(page,"已载入明确版本");
 assert.equal(await page.getByLabel("设计模型",{exact:true}).inputValue(),key("a"));
 await page.getByLabel("实施与测试展开").click();assert.equal(await page.getByLabel("测试模型",{exact:true}).inputValue(),key("b"));
 await page.getByLabel("设计模式",{exact:true}).selectOption("auto");
 assert.equal(await page.getByRole("button",{name:"预览单阶段任务",exact:true}).isDisabled(),true);
 await page.getByLabel("设计添加批准候选").click();
 await page.getByLabel("设计候选1模型",{exact:true}).selectOption(key("a"));
 await page.getByLabel("设计候选1推理档位",{exact:true}).selectOption("none");
 await page.getByLabel("任务目标").fill("Synthetic readonly design");
 await page.getByLabel("本次阶段").selectOption("design");
 let input=null;
 await context.route(f.origin+"/control/v1/tasks/preview",async r=>{input=r.request().postDataJSON();await r.continue()});
 await page.getByRole("button",{name:"预览单阶段任务",exact:true}).click();await status(page,"缺少有效阶段模型或执行准入");
 assert.deepEqual(input.required_roles,["design"]);assert.deepEqual(input.preset,{id:"fixture-preset",revision:1});
 assert.equal(input.task.roles.design.candidates.length,1);assert.equal(input.task.roles.testing.model,models.b);
 assert.equal(await page.locator("#preview").isVisible(),false);
 assert.deepEqual(errors,[]);
});

test("actual API: edit layer and version come from the same defaults response",async t=>{
 const f=await fixture(t);
 const old=await f.request("/control/v1/projects/synthetic-ui/configuration");
 const target={mode:"locked",route:{id:"fixture-b",revision:1},model:models.b,effort:{mode:"none"}};
 const saved=await f.request("/control/v1/projects/synthetic-ui/defaults","PUT",{layer:{roles:{testing:target}}},'"0"');
 assert.equal(saved.status,201);
 const {page,errors}=await f.newPage(async context=>{
  await context.route(f.origin+"/control/v1/projects/synthetic-ui/configuration",r=>r.fulfill({status:200,contentType:"application/json",body:JSON.stringify(old.body)}));
 });
 await page.getByLabel("实施与测试展开").click();
 assert.equal(await page.getByLabel("测试模式",{exact:true}).inputValue(),"locked");
 assert.equal(await page.getByLabel("测试模型",{exact:true}).inputValue(),key("b"));
 await choose(page,"设计","a");
 await page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(page,"已保存版本 2");
 const latest=await f.request("/control/v1/projects/synthetic-ui/defaults");
 assert.equal(latest.body.layer.roles.testing.model,models.b);
 assert.deepEqual(errors,[]);
});
test("actual API: malformed successful save acknowledgement freezes the identical retry",async t=>{
 const f=await fixture(t),{page,context,errors}=await f.newPage();
 await choose(page,"设计","a");let requests=[];
 await context.route(f.origin+"/control/v1/projects/synthetic-ui/defaults",async r=>{
  if(r.request().method()!=="PUT")return r.continue();
  requests.push({tag:r.request().headers()["if-match"],body:r.request().postData()});
  if(requests.length===1){await r.fetch({maxRedirects:0});await r.fulfill({status:200,contentType:"application/json",body:"{}"});return}
  await r.continue();
 });
 await page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(page,"保存状态未确认");
 assert.equal(await page.getByLabel("设计模式",{exact:true}).isDisabled(),true);
 await page.getByRole("button",{name:"重试同一保存",exact:true}).click();await status(page,"已保存版本 1");
 assert.equal(requests.length,2);assert.deepEqual(requests[0],requests[1]);
 assert.deepEqual(errors,[]);
});

test("actual API: expired route never selects a replacement; global and project drafts stay separate",async t=>{
 const f=await fixture(t);
 const target={mode:"locked",route:{id:"fixture-b",revision:1},model:models.b,effort:{mode:"none"}};
 assert.equal((await f.request("/control/v1/projects/synthetic-ui/defaults","PUT",{layer:{roles:{design:target}}},'"0"')).status,201);
 const {page,context,errors}=await f.newPage(async context=>{
  await context.route(f.origin+"/control/v1/projects/synthetic-ui/configuration",async r=>{
   const response=await r.fetch({maxRedirects:0}),body=await response.json();
   body.configuration.routes=body.configuration.routes.filter(route=>route.id!=="fixture-b");
   await r.fulfill({status:200,contentType:"application/json",body:JSON.stringify(body)});
  });
 });
 assert.equal(await page.getByLabel("设计模型",{exact:true}).inputValue(),"expired");
 assert.equal(await page.getByRole("button",{name:"保存项目配置",exact:true}).isDisabled(),true);
 assert.ok(await page.getByRole("alert").filter({hasText:"版本已失效"}).isVisible());
 await page.getByLabel("设计模型",{exact:true}).selectOption(key("a"));
 assert.equal(await page.getByRole("button",{name:"保存项目配置",exact:true}).isDisabled(),true);
 assert.ok(await page.getByRole("alert").filter({hasText:"请选择推理档位"}).isVisible());
 await page.getByLabel("设计推理档位",{exact:true}).selectOption("none");
 assert.ok(await page.locator('[data-role="design"] p.details').filter({hasText:"额度尚未读取。"}).isVisible());
 await page.getByLabel("配置范围",{exact:true}).selectOption("global");
 await choose(page,"设计","c");
 await page.getByRole("button",{name:"保存全局默认",exact:true}).click();await status(page,"已保存版本 1");
 assert.equal((await f.request("/control/v1/defaults/global")).body.layer.roles.design.model,models.c);
 await page.getByLabel("配置范围",{exact:true}).selectOption("project");
 assert.equal(await page.getByLabel("设计模型",{exact:true}).inputValue(),key("a"));
 await page.getByRole("button",{name:"保存项目配置",exact:true}).click();await status(page,"已保存版本 2");
 assert.equal((await f.request("/control/v1/projects/synthetic-ui/defaults")).body.layer.roles.design.model,models.a);
 assert.deepEqual(errors,[]);
});

test("preset UI: creates five-role preset and revisions without changing defaults or old task source",async t=>{
 const f=await fixture(t),{page,errors}=await f.newPage();
 assert.equal(await page.getByRole("button",{name:"保存为新预设",exact:true}).count(),1,"preset creation UI missing");
 await page.getByLabel("实施与测试展开").click();await choose(page,"实施","a");await choose(page,"测试","b");
 await page.getByLabel("审查与验收展开").click();await choose(page,"验收","c");
 await page.getByLabel("预设名称",{exact:true}).fill("Fixture <svg> preset");
 await page.getByRole("button",{name:"保存为新预设",exact:true}).click();await status(page,"已保存预设版本 1");
 const list=await f.request("/control/v1/projects/synthetic-ui/presets");assert.equal(list.body.presets.length,1);
 const id=list.body.presets[0].id,presetPath="/control/v1/projects/synthetic-ui/presets/"+id;
 assert.match(id,/^p_[0-9a-f]{32}$/);
 const v1=await f.request(presetPath+"/versions/1");assert.equal(v1.body.layer.roles.testing.model,models.b);assert.equal(v1.body.layer.roles.acceptance.model,models.c);
 assert.equal((await f.request("/control/v1/projects/synthetic-ui/defaults")).body.revision,0);
 assert.equal(await page.locator(".presets svg").count(),0);
 await page.getByLabel("配置范围").selectOption("task");
 await page.getByRole("button",{name:"载入此版本",exact:true}).click();await status(page,"已载入明确版本");
 await choose(page,"设计","b");await page.getByLabel("预设名称",{exact:true}).fill("Revised preset");
 await page.getByRole("button",{name:"保存预设新版本",exact:true}).click();await status(page,"已保存预设版本 2");
 assert.ok(await page.getByText("本次任务使用预设 "+id+"@1",{exact:true}).isVisible());
 assert.equal((await f.request(presetPath)).body.revision,2);
 assert.deepEqual((await f.request(presetPath+"/versions/1")).body,v1.body);
 assert.equal((await f.request(presetPath+"/versions/2")).body.layer.roles.design.model,models.b);
 assert.equal((await f.request("/control/v1/projects/synthetic-ui/defaults")).body.revision,0);
 assert.deepEqual(errors,[]);
});

test("preset UI: stale loaded revision conflicts; committed but malformed acknowledgement retries identical body/tag/id",async t=>{
 const f=await fixture(t);const presetPath="/control/v1/projects/synthetic-ui/presets/existing";
 const binding=id=>({mode:"locked",route:{id:"fixture-"+id,revision:1},model:models[id],effort:{mode:"none"}});
 assert.equal((await f.request(presetPath,"PUT",{name:"Initial",layer:{roles:{design:binding("a")}}},'"0"')).status,201);
 const a=await f.newPage(),b=await f.newPage();
 await a.page.getByLabel("预设名称",{exact:true}).fill("Unsaved local name");
 await a.page.getByLabel("预设",{exact:true}).selectOption("existing");
 assert.equal(await a.page.getByLabel("预设名称",{exact:true}).inputValue(),"Unsaved local name");
 a.page.once("dialog",d=>d.dismiss());await a.page.getByRole("button",{name:"载入此版本",exact:true}).click();
 assert.equal(await a.page.getByLabel("预设名称",{exact:true}).inputValue(),"Unsaved local name");
 a.page.once("dialog",d=>d.accept());
 for(const {page} of [a,b]){
  await page.getByLabel("预设",{exact:true}).selectOption("existing");
  await page.getByRole("button",{name:"载入此版本",exact:true}).click();await status(page,"已载入明确版本");
 }
 await choose(a.page,"设计","b");await a.page.getByRole("button",{name:"保存预设新版本",exact:true}).click();await status(a.page,"已保存预设版本 2");
 await choose(b.page,"设计","c");await b.page.getByRole("button",{name:"保存预设新版本",exact:true}).click();await status(b.page,"预设已被其他窗口更新");
 assert.equal(await b.page.getByLabel("设计模型",{exact:true}).inputValue(),key("c"));assert.equal((await f.request(presetPath)).body.revision,2);
 const retries=[];
 await a.context.route(f.origin+presetPath,async r=>{
  if(r.request().method()!=="PUT")return r.continue();
  retries.push({url:r.request().url(),tag:r.request().headers()["if-match"],body:r.request().postData()});
  if(retries.length===1){await r.fetch({maxRedirects:0});return r.fulfill({status:201,headers:{ETag:'"3"',"Content-Type":"application/json"},body:JSON.stringify({preset:{id:"wrong",revision:3,layer:{roles:{}}}})})}
  return r.continue();
 });
 await choose(a.page,"设计","c");await a.page.getByRole("button",{name:"保存预设新版本",exact:true}).click();await status(a.page,"预设保存状态未确认");
 assert.equal(await a.page.getByLabel("预设名称",{exact:true}).isDisabled(),true);
 assert.equal(await a.page.getByRole("button",{name:"保存项目配置",exact:true}).isDisabled(),true);
 await a.page.getByRole("button",{name:"重试同一预设保存",exact:true}).click();await status(a.page,"已保存预设版本 3");
 assert.equal(retries.length,2);assert.deepEqual(retries[0],retries[1]);assert.equal((await f.request(presetPath)).body.revision,3);
 assert.equal((await f.request(presetPath)).body.layer.roles.design.model,models.c);assert.deepEqual(a.errors,[]);assert.deepEqual(b.errors,[]);
});

test("preset UI: creation retry keeps its id/base after head advances and invalid selection/name blocks save",async t=>{
 const f=await fixture(t),{page,context,errors}=await f.newPage();
 await page.getByLabel("预设名称",{exact:true}).fill("中".repeat(100));
 assert.equal(await page.getByRole("button",{name:"保存为新预设",exact:true}).isDisabled(),true);
 await page.getByLabel("预设名称",{exact:true}).fill("Original creation");
 await page.getByLabel("设计模式",{exact:true}).selectOption("locked");
 assert.equal(await page.getByRole("button",{name:"保存为新预设",exact:true}).isDisabled(),true);
 await page.getByLabel("设计模型",{exact:true}).selectOption(key("a"));
 await page.getByLabel("设计推理档位",{exact:true}).selectOption("none");
 assert.equal(await page.getByRole("button",{name:"保存预设新版本",exact:true}).isDisabled(),true);
 const retries=[];
 await context.route(f.origin+"/control/v1/projects/synthetic-ui/presets/p_*",async r=>{
  if(r.request().method()!=="PUT")return r.continue();
  retries.push({url:r.request().url(),tag:r.request().headers()["if-match"],body:r.request().postData()});
  if(retries.length===1){await r.fetch({maxRedirects:0});return r.abort("failed")}
  return r.continue();
 });
 await page.getByRole("button",{name:"保存为新预设",exact:true}).click();await status(page,"预设保存状态未确认");
 assert.equal(await page.getByRole("button",{name:"保存为新预设",exact:true}).isDisabled(),true);
 const requestPath=new URL(retries[0].url).pathname;
 const saved=JSON.parse(retries[0].body);
 assert.equal((await f.request(requestPath,"PUT",{name:"Concurrent newer head",layer:saved.layer},'"1"')).status,201);
 await page.getByRole("button",{name:"重试同一预设保存",exact:true}).click();await status(page,"已保存预设版本 1");
 assert.equal(retries.length,2);assert.deepEqual(retries[0],retries[1]);assert.equal((await f.request(requestPath)).body.revision,2);
 assert.equal((await f.request(requestPath+"/versions/1")).body.name,"Original creation");
 assert.equal((await f.request("/control/v1/projects/synthetic-ui/presets")).body.presets.length,1);
 await page.getByRole("button",{name:"保存预设新版本",exact:true}).click();await status(page,"预设已被其他窗口更新");
 assert.equal((await f.request(requestPath)).body.revision,2);assert.equal((await f.request("/control/v1/projects/synthetic-ui/defaults")).body.revision,0);
 assert.deepEqual(errors,[]);
});
async function taskPreview(page,goal){
 await page.getByLabel('配置范围').selectOption('task');await page.getByLabel('任务目标').fill(goal);
 await page.getByRole('button',{name:'预览单阶段任务',exact:true}).click();await page.getByRole('status').filter({hasText:'已核对冻结阶段计划'}).waitFor({timeout:5000});
}
test('submission recovery: task POST follows persisted original preparation and matching acknowledgement',async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();let beforeTask;const taskPosts=[];
 await context.route('**/agent/v1/tasks',async route=>{
  beforeTask=await f.request('/control/v1/projects/synthetic-ui/submission');taskPosts.push({body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']});await route.continue();
 });
 const goal='保存原请求后再提交';await taskPreview(page,goal);await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'任务已保存');
 assert.equal(beforeTask.body.submission?.state,'prepared');assert.equal(beforeTask.body.submission.goal,goal);assert.equal(beforeTask.body.submission.key,taskPosts[0].key);assert.equal(beforeTask.body.submission.preview.preview_id,taskPosts[0].body.preview_id);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission')).body.submission,null);
 assert.equal(taskPosts.length,1);assert.equal(await page.getByLabel('任务目标').isDisabled(),false);assert.deepEqual(errors,[]);
});
test('submission recovery: opening a window restores a prepared original request without posting a task',async t=>{
 const f=await fixture(t,true);const goal='<img src=x onerror=alert(1)> 原冻结目标';
 const p=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui',goal,required_roles:['design']});assert.equal(p.status,200);
 const body={preview_id:p.body.preview_id,plan_hash:p.body.plan.hash},key='original-prepared-window-key';assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission','POST',body,null,key)).status,200);
 const {page,errors}=await f.newPage();await page.getByRole('status').filter({hasText:'已恢复原任务请求'}).waitFor({timeout:2000});
 assert.equal(await page.getByLabel('任务目标').inputValue(),goal);assert.equal(await page.getByLabel('任务目标').isDisabled(),true);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 const record=page.locator('#submission-record');assert.ok((await record.innerText()).includes(key));assert.ok((await record.innerText()).includes(p.body.plan.hash));assert.equal(await record.locator('img').count(),0);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,0);
 await page.locator('#submission-recovery').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/submission-ui-desktop.png'),fullPage:true});
 const mobile=await f.newPage(null,{width:390,height:844});assert.equal(await mobile.page.getByLabel('任务目标').isDisabled(),true);assert.ok((await mobile.page.locator('#submission-record').innerText()).includes(key));
 assert.ok(await mobile.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'recovery section must fit the existing narrow layout');
 await mobile.page.locator('#submission-recovery').scrollIntoViewIfNeeded();await mobile.page.screenshot({path:path.join(repo,'.fusion-dev/implementation/submission-ui-mobile.png'),fullPage:true});assert.deepEqual(mobile.errors,[]);await mobile.context.close();
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'放弃未提交请求',exact:true}).click();await status(page,'原请求已封存');
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission')).body.submission,null);
 assert.equal((await f.request('/agent/v1/tasks','POST',body,null,key)).status,409);assert.equal(await page.getByLabel('任务目标').isDisabled(),false);assert.deepEqual(errors,[]);
});
test('submission recovery: closed window restores committed original and same-task retry clears acknowledgement',async t=>{
 const f=await fixture(t,true),first=await f.newPage();let original;
 await first.context.route('**/agent/v1/tasks',async route=>{
  original={body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']};await route.fetch();await route.fulfill({status:503,json:{error:{code:'lost_receipt'}}});
 });
 const goal='关闭窗口后核对原任务';await taskPreview(first.page,goal);await first.page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(first.page,'提交结果未确认');
 await first.context.close();await f.restart();const second=await f.newPage();await second.page.getByRole('status').filter({hasText:'已恢复原任务请求'}).waitFor({timeout:2000});
 assert.equal(await second.page.getByLabel('任务目标').inputValue(),goal);let retried;
 await second.context.route('**/agent/v1/tasks',async route=>{retried={body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']};await route.continue()});
 await second.page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(second.page,'任务已保存');assert.deepEqual(retried,original);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,1);assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission')).body.submission,null);
 assert.deepEqual(await second.page.evaluate(()=>[localStorage.length,sessionStorage.length]),[0,0]);assert.deepEqual(first.errors,[]);assert.deepEqual(second.errors,[]);
});
test('task workbench: frozen submit persists ready task and reload reads actual plan/budget safely',async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();
 const goal='<img src=x onerror=alert(1)> 只提交设计任务';await taskPreview(page,goal);
 await page.getByRole('button',{name:'冻结提交任务',exact:true}).waitFor({timeout:2000});
 await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'任务已保存');
 const index=await f.request('/control/v1/projects/synthetic-ui/tasks');assert.equal(index.body.tasks.length,1);
 const saved=index.body.tasks[0];assert.equal(saved.state,'ready');assert.equal(saved.generation,0);
 await page.getByRole('button',{name:'查看任务 '+saved.id,exact:true}).waitFor();
 await page.getByRole('button',{name:'查看任务 '+saved.id,exact:true}).click();await status(page,'已读取任务详情');
 assert.equal(await page.locator('#task-detail img').count(),0);assert.ok((await page.locator('#task-detail').innerText()).includes(goal));
 assert.ok((await page.locator('#task-detail').innerText()).includes('fixture-model-a'));assert.ok((await page.locator('#task-detail').innerText()).includes('调用预算：0 / 50'));
 await page.reload();await status(page,'已载入当前配置');await page.getByRole('button',{name:'查看任务 '+saved.id,exact:true}).click();await status(page,'已读取任务详情');
 await page.locator('#task-detail').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/task-ui-desktop.png'),fullPage:true});
 await context.route('**/control/v1/tasks/*/plan',async route=>{const actual=await route.fetch(),body=await actual.json();body.revision++;await route.fulfill({response:actual,json:body})});
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'读取失败');assert.equal(await page.locator('#task-detail').innerText(),'');
 await context.unroute('**/control/v1/tasks/*/plan');await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'已读取任务详情');
 const mobile=await f.newPage(null,{width:390,height:844});await mobile.page.getByRole('button',{name:'查看任务 '+saved.id,exact:true}).click();await status(mobile.page,'已读取任务详情');assert.ok(await mobile.page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await mobile.page.locator('#task-detail').scrollIntoViewIfNeeded();await mobile.page.screenshot({path:path.join(repo,'.fusion-dev/implementation/task-ui-mobile.png'),fullPage:true});assert.deepEqual(mobile.errors,[]);
 assert.deepEqual(errors,[]);
});
test('submission recovery: unknown preparation never posts a task; window reopen recovers the original key',async t=>{
 const f=await fixture(t,true),first=await f.newPage();let preparation,taskPosts=0;
 await first.context.route('**/control/v1/projects/synthetic-ui/submission',async route=>{
  if(route.request().method()!=='POST')return route.continue();preparation={body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']};await route.fetch();await route.fulfill({status:503,json:{error:{code:'lost_preparation'}}});
 });
 first.page.on('request',r=>{if(new URL(r.url()).pathname==='/agent/v1/tasks')taskPosts++});
 await taskPreview(first.page,'丢失原请求保存回执');await first.page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(first.page,'原请求保存结果未确认');assert.equal(taskPosts,0);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,0);assert.equal(await first.page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await first.context.close();const second=await f.newPage();await status(second.page,'已恢复原任务请求');let actual;
 await second.context.route('**/agent/v1/tasks',async route=>{actual={body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']};await route.continue()});
 await second.page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(second.page,'任务已保存');assert.deepEqual(actual,preparation);assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,1);
 assert.deepEqual(first.errors,[]);assert.deepEqual(second.errors,[]);
});
test('submission recovery: lost acknowledgement retries only the original acknowledgement and does not repeat task POST',async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();const acknowledgements=[];let taskPosts=0;
 page.on('request',r=>{if(new URL(r.url()).pathname==='/agent/v1/tasks')taskPosts++});
 await context.route('**/control/v1/projects/synthetic-ui/submission/acknowledge',async route=>{
  acknowledgements.push({body:route.request().postData(),key:route.request().headers()['idempotency-key']});const actual=await route.fetch();if(acknowledgements.length===1)await route.fulfill({status:200,json:{submission:null}});else await route.fulfill({response:actual});
 });
 await taskPreview(page,'确认回执丢失不重复任务提交');await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'原记录确认未核对');assert.equal(taskPosts,1);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission')).body.submission,null);assert.equal(await page.getByLabel('任务目标').isDisabled(),true);
 await page.getByRole('button',{name:'重试任务保存确认',exact:true}).click();await status(page,'任务已保存');assert.equal(taskPosts,1);assert.deepEqual(acknowledgements[0],acknowledgements[1]);assert.equal(await page.getByLabel('任务目标').isDisabled(),false);assert.deepEqual(errors,[]);
});
test('submission recovery: lost seal response cannot release editing or send another task POST',async t=>{
 const f=await fixture(t,true);const p=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui',goal:'封存回执丢失',required_roles:['design']});const body={preview_id:p.body.preview_id,plan_hash:p.body.plan.hash},key='seal-original';assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission','POST',body,null,key)).status,200);
 const {page,context,errors}=await f.newPage();const seals=[];let taskPosts=0;page.on('request',r=>{if(new URL(r.url()).pathname==='/agent/v1/tasks')taskPosts++});
 await context.route('**/control/v1/projects/synthetic-ui/submission/abandon',async route=>{seals.push({body:route.request().postData(),key:route.request().headers()['idempotency-key']});const actual=await route.fetch();if(seals.length===1)await route.fulfill({status:503,json:{error:{code:'lost_seal'}}});else await route.fulfill({response:actual})});
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'放弃未提交请求',exact:true}).click();await status(page,'放弃结果未确认');assert.equal(await page.getByLabel('任务目标').isDisabled(),true);assert.equal(await page.getByRole('button',{name:'重试同一任务提交',exact:true}).isVisible(),false);assert.equal(taskPosts,0);
 await page.getByRole('button',{name:'重试放弃原请求',exact:true}).click();await status(page,'原请求已封存');assert.deepEqual(seals[0],seals[1]);assert.equal(taskPosts,0);assert.equal((await f.request('/agent/v1/tasks','POST',body,null,key)).status,409);assert.deepEqual(errors,[]);
});
test('submission recovery: prepared request survives real host restart; lost preview requires explicit seal',async t=>{
 const f=await fixture(t,true);const goal='重启后不复活预览';const p=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui',goal,required_roles:['design']});const body={preview_id:p.body.preview_id,plan_hash:p.body.plan.hash},key='restart-prepared';assert.equal((await f.request('/control/v1/projects/synthetic-ui/submission','POST',body,null,key)).status,200);
 await f.restart();const {page,errors}=await f.newPage();await status(page,'已恢复原任务请求');assert.equal(await page.getByLabel('任务目标').inputValue(),goal);
 await page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(page,'提交结果未确认');assert.equal(await page.getByLabel('任务目标').isDisabled(),true);assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,0);
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'放弃未提交请求',exact:true}).click();await status(page,'原请求已封存');assert.equal(await page.getByLabel('任务目标').isDisabled(),false);assert.deepEqual(errors,[]);
});
test('submission recovery: startup discovers the pending project instead of creating a new request in the first project',async t=>{
 const f=await fixture(t,true,true);const project='synthetic-ui-other',goal='另一个项目的原请求';const p=await f.request('/control/v1/tasks/preview','POST',{project_id:project,goal,required_roles:['design']});assert.equal((await f.request('/control/v1/projects/'+project+'/submission','POST',{preview_id:p.body.preview_id,plan_hash:p.body.plan.hash},null,'other-project-original')).status,200);
 const {page,errors}=await f.newPage();await status(page,'已恢复原任务请求');assert.equal(await page.getByLabel('项目',{exact:true}).inputValue(),project);assert.equal(await page.getByLabel('任务目标').inputValue(),goal);assert.ok((await page.locator('#submission-record').innerText()).includes('other-project-original'));assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,0);assert.deepEqual(errors,[]);
});
test('submission recovery: malformed prepare metadata keeps original identity and never posts a task',async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();const preparations=[];let taskPosts=0;
 page.on('request',r=>{if(new URL(r.url()).pathname==='/agent/v1/tasks')taskPosts++});
 await context.route('**/control/v1/projects/synthetic-ui/submission',async route=>{
  if(route.request().method()!=='POST')return route.continue();preparations.push({body:route.request().postData(),key:route.request().headers()['idempotency-key']});const actual=await route.fetch();const body=await actual.json();if(preparations.length===1)body.submission.preview.budget.max_calls++;await route.fulfill({response:actual,json:body});
 });
 await taskPreview(page,'不接受替换原预算');await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'原请求保存结果未确认');assert.equal(taskPosts,0);assert.equal(await page.getByLabel('任务目标').isDisabled(),true);
 await page.getByRole('button',{name:'重试保存原请求',exact:true}).click();await status(page,'任务已保存');assert.deepEqual(preparations[0],preparations[1]);assert.equal(taskPosts,1);assert.deepEqual(errors,[]);
});
test('submission recovery: another window reads the same pending original without submitting its new preview',async t=>{
 const f=await fixture(t,true),first=await f.newPage(),second=await f.newPage();let taskPosts=0,original;
 second.page.on('request',r=>{if(new URL(r.url()).pathname==='/agent/v1/tasks')taskPosts++});
 await first.context.route('**/agent/v1/tasks',async route=>{original={body:route.request().postDataJSON(),key:route.request().headers()['idempotency-key']};await route.fetch();await route.fulfill({status:503,json:{error:{code:'lost_receipt'}}})});
 await taskPreview(first.page,'先前窗口的原目标');await taskPreview(second.page,'另一个窗口的新目标');await first.page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(first.page,'提交结果未确认');
 await second.page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(second.page,'已恢复原任务请求');assert.equal(taskPosts,0);assert.equal(await second.page.getByLabel('任务目标').inputValue(),'先前窗口的原目标');assert.ok((await second.page.locator('#submission-record').innerText()).includes(original.key));
 await first.context.unroute('**/agent/v1/tasks');await first.page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(first.page,'任务已保存');
 await second.page.getByRole('button',{name:'重新核对原请求',exact:true}).click();await status(second.page,'任务已保存');assert.equal(taskPosts,0);assert.equal((await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks.length,1);assert.deepEqual(first.errors,[]);assert.deepEqual(second.errors,[]);
});
test('submission recovery: a substituted task ID is unproved until the original journal and task read agree',async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();
 await context.route('**/agent/v1/tasks',async route=>{const actual=await route.fetch(),body=await actual.json();body.id='fake-ui-task';await route.fulfill({response:actual,json:body})});
 await taskPreview(page,'错误任务 ID 不能当作原任务');await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'原记录确认未核对');assert.equal(await page.getByLabel('任务目标').isDisabled(),true);
 assert.ok(!(await page.locator('#submission-record').innerText()).includes('原任务：fake-ui-task'));
 await page.getByRole('button',{name:'重新核对原请求',exact:true}).click();await status(page,'任务已保存');const tasks=await f.request('/control/v1/projects/synthetic-ui/tasks');assert.equal(tasks.body.tasks.length,1);assert.notEqual(tasks.body.tasks[0].id,'fake-ui-task');assert.equal(await page.getByLabel('任务目标').isDisabled(),false);assert.deepEqual(errors,[]);
});
test('submission recovery: an unavailable project journal blocks editing until a successful explicit reread',async t=>{
 const f=await fixture(t,true,true),{page,context,errors}=await f.newPage();
 const pendingPath=f.origin+'/control/v1/projects/synthetic-ui-other/submission';
 await context.route(pendingPath,route=>route.fulfill({status:503,json:{error:{code:'unavailable'}}}));
 await page.getByLabel('项目',{exact:true}).selectOption('synthetic-ui-other');await status(page,'原请求核对失败');assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'保存项目配置',exact:true}).isDisabled(),true);
 await context.unroute(pendingPath);await page.getByRole('button',{name:'重新核对原请求',exact:true}).click();await page.getByRole('status').filter({hasText:'已核对，没有未解决原请求'}).waitFor({timeout:2000});assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),false);assert.equal(await page.getByRole('button',{name:'保存项目配置',exact:true}).isDisabled(),false);assert.deepEqual(errors,[]);
});
for(const receiptMode of ['503','malformed201'])test('task workbench: lost successful receipt '+receiptMode+' freezes editing; identical request retry returns one task',async t=>{
 const f=await fixture(t,true),{page,context,errors}=await f.newPage();const attempts=[];
 await context.route('**/agent/v1/tasks',async route=>{
  attempts.push({body:route.request().postData(),key:route.request().headers()['idempotency-key']});
  if(attempts.length===2){await route.fulfill({status:409,json:{error:{code:'preview_expired_or_changed'}}});return}
  const response=await route.fetch();if(attempts.length===1)await route.fulfill(receiptMode==='503'?{status:503,json:{error:{code:'transport_unconfirmed'}}}:{status:201,json:{id:'incomplete'}});else await route.fulfill({response});
 });
 await taskPreview(page,'冻结提交的回执丢失');await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'提交结果未确认');
 assert.equal(await page.getByLabel('任务目标').isDisabled(),true);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'预览单阶段任务',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'刷新任务列表',exact:true}).click();await status(page,'已读取 1 个任务');
 await page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(page,'提交结果未确认');assert.equal(await page.getByLabel('任务目标').isDisabled(),true);
 await page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(page,'任务已保存');
 assert.equal(attempts.length,3);assert.deepEqual(attempts[0],attempts[1]);assert.deepEqual(attempts[0],attempts[2]);assert.ok(attempts[0].key);
 const tasks=await f.request('/control/v1/projects/synthetic-ui/tasks');assert.equal(tasks.body.tasks.length,1);assert.equal(tasks.body.tasks[0].state,'ready');
 assert.equal(await page.getByLabel('任务目标').isDisabled(),false);assert.deepEqual(errors,[]);
});
test('task workbench: changed configuration rejects old preview; expired preview never submits',async t=>{
 const f=await fixture(t,true),{page}=await f.newPage();await taskPreview(page,'不可替换旧预览');
 const updated=await f.request('/control/v1/projects/synthetic-ui/defaults','PUT',{layer:{}},'"0"');assert.equal(updated.status,201);
 await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'预览已失效');assert.equal(await page.getByLabel('任务目标').isDisabled(),false);
 await page.clock.install();await taskPreview(page,'重新预览');let submissions=0;page.on('request',r=>{if(new URL(r.url()).pathname==='/agent/v1/tasks')submissions++});
 await page.clock.fastForward(301000);await status(page,'预览已到期');assert.equal(await page.getByRole('button',{name:'冻结提交任务',exact:true}).isDisabled(),true);assert.equal(submissions,0);
 const tasks=await f.request('/control/v1/projects/synthetic-ui/tasks');assert.deepEqual(tasks.body.tasks,[]);
});
test('task workbench: actual 32+1 pagination and late previous-project response cannot replace current list',async t=>{
 const f=await fixture(t,true,true);
 for(let i=0;i<33;i++){
  const p=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui',goal:'任务索引 '+i,required_roles:['design']});assert.equal(p.status,200);
  const s=await f.request('/agent/v1/tasks','POST',{preview_id:p.body.preview_id,plan_hash:p.body.plan.hash},null,'browser-index-'+i);assert.equal(s.status,201);
 }
 const {page,context,errors}=await f.newPage();await status(page,'已读取 32 个任务');assert.equal(await page.locator('#task-list button[data-task-id]').count(),32);
 await page.getByRole('button',{name:'读取更早任务',exact:true}).click();await status(page,'已读取 33 个任务');assert.equal(await page.locator('#task-list button[data-task-id]').count(),33);
 const buttons=page.locator('#task-list button[data-task-id]'),oldID=await buttons.nth(0).getAttribute('data-task-id'),newID=await buttons.nth(1).getAttribute('data-task-id');
 let releaseDetail;const detailGate=new Promise(resolve=>releaseDetail=resolve);let detailStarted;const detailReady=new Promise(resolve=>detailStarted=resolve);
 await context.route('**/control/v1/tasks/'+oldID+'/plan',async route=>{detailStarted();await detailGate;await route.continue()});
 await page.getByRole('button',{name:'查看任务 '+oldID,exact:true}).click();await detailReady;
 await page.getByRole('button',{name:'查看任务 '+newID,exact:true}).click();await status(page,'已读取任务详情');assert.ok((await page.locator('#task-detail').innerText()).includes('任务索引 31'));
 const lateDetail=page.waitForResponse(r=>r.url().endsWith('/agent/v1/tasks/'+oldID));releaseDetail();await(await lateDetail).finished();await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));assert.ok((await page.locator('#task-detail').innerText()).includes('任务索引 31'));
 await context.unroute('**/control/v1/tasks/'+oldID+'/plan');
 let release;const gate=new Promise(resolve=>release=resolve);let started;const ready=new Promise(resolve=>started=resolve);
 await context.route('**/control/v1/projects/synthetic-ui/tasks',async route=>{started();await gate;await route.continue()});
 await page.getByRole('button',{name:'刷新任务列表',exact:true}).click();await ready;
 await page.getByLabel('项目',{exact:true}).selectOption('synthetic-ui-other');await status(page,'暂无已保存任务');
 const reply=page.waitForResponse(r=>r.url().endsWith('/control/v1/projects/synthetic-ui/tasks'));release();await(await reply).finished();await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 assert.equal(await page.locator('#task-list button[data-task-id]').count(),0);assert.equal(await page.getByLabel('项目',{exact:true}).inputValue(),'synthetic-ui-other');assert.deepEqual(errors,[]);
});

async function savedControlTask(t,f){
 const view=await f.newPage();await taskPreview(view.page,'单阶段运行控制验证');await view.page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(view.page,'任务已保存');
 await view.page.locator('#task-detail-status').filter({hasText:'已读取任务详情'}).waitFor();
 const tasks=(await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks;assert.equal(tasks.length,1);return {...view,task:tasks[0]};
}
test('execution controls: idle pause continue cancel use the frozen Task condition without starting Runtime',async t=>{
 const f=await fixture(t,true),{page,task,errors}=await savedControlTask(t,f);
 assert.equal(await page.getByRole('button',{name:'暂停任务',exact:true}).count(),1);
 await page.getByRole('button',{name:'暂停任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'控制回执已核对'}).waitFor();await page.locator('#task-detail').filter({hasText:'(paused)'}).waitFor();assert.ok((await page.locator('#task-detail').innerText()).includes('paused'));
 await page.getByRole('button',{name:'继续派单',exact:true}).click();await page.locator('#execution-status').filter({hasText:'控制回执已核对'}).waitFor();await page.locator('#task-detail').filter({hasText:'(ready)'}).waitFor();assert.ok((await page.locator('#task-detail').innerText()).includes('ready'));
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'取消任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'控制回执已核对'}).waitFor();await page.locator('#task-detail').filter({hasText:'(cancelled)'}).waitFor();assert.ok((await page.locator('#task-detail').innerText()).includes('cancelled'));
 const current=await f.request('/agent/v1/tasks/'+task.id);assert.equal(current.body.generation,3);assert.equal(current.body.state,'cancelled');assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);assert.deepEqual(errors,[]);
});
test('execution controls: a lost start receipt reconciles the exact original journal then cancels one run',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);const posts=[];let originalRun;
 assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).count(),1);
 await context.route('**/control/v1/tasks/*/start',async route=>{posts.push({body:route.request().postData(),key:route.request().headers()['idempotency-key'],tag:route.request().headers()['if-match']});const response=await route.fetch();const body=await response.json();originalRun=body.run.id;if(posts.length===1)await route.fulfill({status:503,json:{error:{code:'lost_start_reply'}}});else await route.fulfill({response})});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重试原运行请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原启动记录已确认'}).waitFor();assert.equal(posts.length,1);assert.equal(posts[0].tag,'"p1-g0-ready"');assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);
 const original=(await f.request('/control/v1/tasks/'+task.id+'/start-request','GET',null,posts[0].tag,posts[0].key)).body.request;assert.equal(original.state,'acknowledged');assert.equal(original.run_id,originalRun);assert.equal(original.role,JSON.parse(posts[0].body).role);assert.equal(original.etag,posts[0].tag);await page.locator('#run-record').filter({hasText:originalRun}).waitFor();
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'取消任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'控制回执已核对'}).waitFor();await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#task-detail').filter({hasText:'cancelled'}).waitFor();
 assert.equal((await f.request('/agent/v1/tasks/'+task.id+'/runs/'+originalRun)).body.run.state,'cancelled');assert.deepEqual(await page.evaluate(()=>[localStorage.length,sessionStorage.length]),[0,0]);assert.deepEqual(errors,[]);
});

test('execution controls: substituted successful run ID keeps the original start request until independent run read proves it',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);const posts=[];let actualRun;
 await context.route('**/control/v1/tasks/*/start',async route=>{posts.push({body:route.request().postData(),key:route.request().headers()['idempotency-key'],tag:route.request().headers()['if-match']});const response=await route.fetch();const body=await response.json();actualRun=body.run.id;if(posts.length===1){body.run.id='substituted-ui-run';await route.fulfill({response,json:body})}else await route.fulfill({response})});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();
 assert.equal(await page.getByRole('button',{name:'重试原运行请求',exact:true}).isVisible(),true);
 assert.ok(!(await page.locator('#run-record').innerText()).includes('substituted-ui-run'));
 await page.getByRole('button',{name:'重试原运行请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原启动记录已确认'}).waitFor();await page.locator('#run-record').filter({hasText:actualRun}).waitFor();assert.equal(posts.length,1);const original=(await f.request('/control/v1/tasks/'+task.id+'/start-request','GET',null,posts[0].tag,posts[0].key)).body.request;assert.equal(original.state,'acknowledged');assert.equal(original.run_id,actualRun);assert.equal(original.role,JSON.parse(posts[0].body).role);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});

test('execution controls: stale Task condition rejects start and requires a new explicit Task read',async t=>{
 const f=await fixture(t,true),{page,context,task,errors}=await savedControlTask(t,f);let posted=0,prepared=0;
 await context.route('**/control/v1/tasks/*/start',async route=>{posted++;await route.continue()});
 await context.route('**/control/v1/tasks/*/start-request',async route=>{if(route.request().method()==='POST')prepared++;await route.continue()});
 const current=await f.request('/agent/v1/tasks/'+task.id);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/pause','POST',{},current.etag)).status,200);
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'任务条件已变化'}).waitFor();assert.equal(prepared,1);assert.equal(posted,0);assert.equal((await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request,null);assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#task-detail').filter({hasText:'paused'}).waitFor();assert.equal(await page.getByRole('button',{name:'继续派单',exact:true}).isDisabled(),false);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});
test('execution controls: lost pause receipt reads the changed Task condition without resending or starting',async t=>{
 const f=await fixture(t,true),{page,context,task,errors}=await savedControlTask(t,f);let pauses=0;
 await context.route('**/control/v1/tasks/*/pause',async route=>{pauses++;await route.fetch();await route.fulfill({status:503,json:{error:{code:'lost_pause_reply'}}})});
 await page.getByRole('button',{name:'暂停任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原控制条件已失效'}).waitFor();assert.equal(pauses,1);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),false);assert.equal(await page.getByRole('button',{name:'继续派单',exact:true}).isDisabled(),false);
 assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.state,'paused');assert.deepEqual(errors,[]);
});
test('execution controls: running pause is needs_review after cancelled outcome and never a blind continue',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,task,errors}=await savedControlTask(t,f);
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#run-record').filter({hasText:'running'}).waitFor();await page.getByRole('button',{name:'暂停任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'控制回执已核对'}).waitFor();
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#task-detail').filter({hasText:'needs_review'}).waitFor();assert.equal(await page.getByRole('button',{name:'继续派单',exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});
test('execution controls: successful synthetic stage stays separate from whole-task acceptance in the original Magpie layout',async t=>{
 const f=await fixture(t,true,false,'success'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0;await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#run-record').filter({hasText:'已知阶段执行'}).waitFor();
 for(let i=0;i<10;i++){await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#task-detail-status').filter({hasText:'已读取任务详情'}).waitFor();if((await page.locator('#run-record').innerText()).includes('succeeded'))break}
 assert.ok((await page.locator('#run-record').innerText()).includes('succeeded'));assert.ok((await page.locator('#run-record').innerText()).includes('阶段结果不等于整项验收'));assert.equal(starts,1);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.state,'ready');
 await page.locator('#execution-controls').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/control-ui-desktop.png'),fullPage:true});await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'original control layout must fit narrow windows');assert.ok((await page.locator('#execution-controls label').boundingBox()).width>=40,'stage label must remain readable rather than one character wide');await page.locator('#execution-controls').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/control-ui-mobile.png'),fullPage:true});assert.deepEqual(errors,[]);
});

test('execution controls: unavailable run detail keeps independently read Task state and never enables another start',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#run-record').filter({hasText:'running'}).waitFor();await page.waitForFunction(()=>!document.getElementById('reload-task').disabled);
 const failure=async route=>route.fulfill({status:503,json:{error:{code:'run_temporarily_unavailable'}}});await context.route('**/agent/v1/tasks/*/runs/*',failure);
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#execution-status').filter({hasText:'阶段执行回读失败'}).waitFor();await page.waitForFunction(()=>!document.getElementById('reload-task').disabled);assert.ok((await page.locator('#task-detail').innerText()).includes('running'));assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);assert.equal(await page.locator('#run-record').innerText(),'');
 await context.unroute('**/agent/v1/tasks/*/runs/*',failure);await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#run-record').filter({hasText:'running'}).waitFor();assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});

test('execution controls: changing project disables old task actions while new configuration is still loading',{timeout:15000},async t=>{
 const f=await fixture(t,true,true),{page,context,errors}=await savedControlTask(t,f);let entered,release;const began=new Promise(r=>entered=r),held=new Promise(r=>release=r);
 await context.route('**/control/v1/projects/synthetic-ui-other/configuration',async route=>{entered();await held;await route.continue().catch(()=>{})});
 page.once('dialog',d=>d.accept());await page.getByLabel('项目',{exact:true}).selectOption('synthetic-ui-other');await Promise.race([began,new Promise((_,reject)=>setTimeout(()=>reject(Error('project configuration request did not begin')),3000))]);
 try{assert.equal(await page.locator('#execution-controls').isVisible(),false);assert.equal(await page.locator('#start-role').isDisabled(),true);assert.equal(await page.locator('#pause-task').isDisabled(),true)}finally{release()}
 await page.getByRole('status').filter({hasText:'已载入当前配置'}).waitFor();assert.equal(await page.locator('#execution-controls').isVisible(),false);assert.deepEqual(errors,[]);
});

test('quota pane: initial cache read never refreshes suppliers and unsupported routes remain explicit',async t=>{
 const f=await fixture(t),requests=[];
 const {page,errors}=await f.newPage(async context=>{await context.route('**/control/v1/projects/*/quota**',async route=>{requests.push({method:route.request().method(),url:route.request().url()});await route.continue()})});
 assert.equal(await page.locator('#quota-panel').count(),1);
 await page.locator('#quota-panel summary').click();await page.locator('#quota-status').filter({hasText:'已读取额度缓存'}).waitFor();
 assert.equal(requests.filter(r=>r.method==='POST').length,0);assert.equal(requests.filter(r=>r.method==='GET').length,1);
 assert.equal(await page.locator('#quota-list .row').count(),3);assert.ok((await page.locator('#quota-list').innerText()).includes('不支持查询'));
 assert.equal(await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).isDisabled(),true);assert.deepEqual(errors,[]);
});
const quotaBase='/control/v1/projects/synthetic-ui/quota';
async function openQuota(page){await page.locator('#quota-panel summary').click();await page.locator('#quota-status').filter({hasText:'已读取额度缓存'}).waitFor()}
const quotaStatus=(page,text)=>page.locator('#quota-status').filter({hasText:text}).waitFor();
test('quota pane: manual synthetic refresh shows zero usage as unverified and preserves source timestamps on cache reads',async t=>{
 const f=await fixture(t,true,false,'',true),requests=[],{page,errors}=await f.newPage(async context=>{
  await context.route('**/control/v1/projects/*/quota**',async r=>{requests.push(r.request().method());await r.continue()})
 });
 await openQuota(page);assert.deepEqual(requests,['GET']);
 await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await quotaStatus(page,'已完成手动额度查询');
 const first=await f.request(quotaBase),snapshot=first.body.routes[0].snapshot;
 assert.equal(snapshot.status,'unverified');assert.equal(snapshot.windows[0].used_percent,0);assert.equal(snapshot.pool.verified,false);
 const row=page.locator('#quota-list .row').filter({hasText:'fixture-a@1'});
 assert.match(await row.textContent(),/已用 0%；剩余 100%/);assert.match(await row.textContent(),/额度：未核验/);
 assert.match(await row.textContent(),/独立资源 credit（CNY）：2.50；不计入订阅百分比/);
 assert.match(await row.textContent(),/观察时间：.+；接收时间：/);
 await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'已读取额度缓存');
 const again=await f.request(quotaBase);assert.deepEqual(again.body.routes[0].snapshot,snapshot);assert.deepEqual(requests,['GET','POST','GET']);
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await row.scrollIntoViewIfNeeded();assert.ok(await row.getByText('额度：未核验。',{exact:true}).isVisible());await row.screenshot({path:path.join(repo,'.fusion-dev/implementation/quota-ui-mobile-row.png')});
 await page.locator('#view-fusion').evaluate(n=>n.scrollTop=0);await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/quota-ui-mobile.png'),fullPage:true});
 await page.setViewportSize({width:1140,height:1000});await row.scrollIntoViewIfNeeded();await row.screenshot({path:path.join(repo,'.fusion-dev/implementation/quota-ui-desktop-row.png')});
 await page.locator('#view-fusion').evaluate(n=>n.scrollTop=0);await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/quota-ui-desktop.png'),fullPage:true});
 assert.deepEqual(errors,[]);
});
test('quota pane: lost manual query acknowledgement reconciles through cache without duplicate refresh',async t=>{
 const f=await fixture(t,true,false,'',true),{page,context,errors}=await f.newPage();await openQuota(page);let sends=0;
 await context.route('**/quota/fixture-a/refresh',async r=>{sends++;await r.fetch();await r.abort('failed')});
 await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await quotaStatus(page,'额度查询结果未确认');
 assert.equal(sends,1);assert.match(await page.locator('#quota-list').textContent(),/历史额度（本次未确认）/);
 await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'已读取额度缓存');
 assert.equal(sends,1);assert.match(await page.locator('#quota-list').textContent(),/已用 0%/);assert.doesNotMatch(await page.locator('#quota-list').textContent(),/历史额度/);
 assert.deepEqual(errors,[]);
});
test('quota pane: malformed or substituted identity never replaces the previous independent observation',async t=>{
 const f=await fixture(t,true,false,'',true),{page,context,errors}=await f.newPage();await openQuota(page);
 await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await quotaStatus(page,'已完成手动额度查询');
 const actual=(await f.request(quotaBase)).body;
 for(const mutation of [body=>{body.routes[0].identity.account='foreign-account'},body=>{body.routes[0].snapshot.windows[0].used_percent=101},body=>{body.routes.push(body.routes[0])}]){
  const bad=structuredClone(actual);mutation(bad);bad.private_key='never-render-this';
  await context.route('**/control/v1/projects/synthetic-ui/quota',r=>r.fulfill({status:200,json:bad}));
  await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'额度缓存读取失败');
  const displayed=await page.locator('#quota-list').textContent();assert.match(displayed,/历史额度（本次未确认）/);assert.match(displayed,/已用 0%/);assert.doesNotMatch(displayed,/foreign-account|never-render-this|101%/);
  await context.unroute('**/control/v1/projects/synthetic-ui/quota');
 }
 assert.deepEqual(errors,[]);
});
test('quota pane: old project refresh reply cannot populate a newly selected project',async t=>{
 const f=await fixture(t,true,true,'',true),{page,context,errors}=await f.newPage();await openQuota(page);
 let release,entered;const held=new Promise(r=>release=r),started=new Promise(r=>entered=r);
 await context.route('**/quota/fixture-a/refresh',async r=>{const reply=await r.fetch();entered();await held;await r.fulfill({response:reply})});
 try{
  await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await started;
  await page.getByLabel('项目',{exact:true}).selectOption('synthetic-ui-other');await status(page,'已载入当前配置');await quotaStatus(page,'已读取额度缓存');
  const arrived=page.waitForResponse(r=>r.url().endsWith('/quota/fixture-a/refresh'));release();await arrived;
  assert.equal(await page.getByLabel('项目',{exact:true}).inputValue(),'synthetic-ui-other');
  assert.doesNotMatch(await page.locator('#quota-list').textContent(),/已用 0%|synthetic-browser-quota/);
  assert.match(await page.locator('#quota-list').textContent(),/额度：未知/);
 }finally{release()}
 assert.deepEqual(errors,[]);
});
test('quota pane: null usage, expired times and server failures remain distinct from available quota',async t=>{
 const f=await fixture(t,true,false,'',true),{page,context,errors}=await f.newPage();await openQuota(page);
 await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await quotaStatus(page,'已完成手动额度查询');
 const actual=(await f.request(quotaBase)).body;
 for(const [code,label,used] of [['unknown','未知',null],['stale','已过期',0],['zero','已耗尽',100],['auth_required','需要授权',null],['unsupported','不支持查询',null]]){
  const body=structuredClone(actual),v=body.routes[0];v.status=code;v.snapshot.status=code;v.snapshot.windows[0].used_percent=used;v.snapshot.windows[0].remaining_percent=null;
  if(code==='zero'){v.snapshot.complete=true;Object.assign(v.snapshot.pool,{id:'shared-fixture',owner:'fixture-team',scope:'team',verified:true})}
  await context.route('**/control/v1/projects/synthetic-ui/quota',r=>r.fulfill({status:200,json:body}));
  await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'已读取额度缓存');
  const row=page.locator('#quota-list .row').filter({hasText:'fixture-a@1'});assert.match(await row.textContent(),new RegExp('额度：'+label));
  if(used===null)assert.match(await row.textContent(),/已用 未知；剩余 未知/);
  await context.unroute('**/control/v1/projects/synthetic-ui/quota');
 }
 await context.route('**/control/v1/projects/synthetic-ui/quota',r=>r.fulfill({status:403,json:{error:{code:'forbidden',message:'unsafe-secret-message'}}}));
 await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'管理授权已失效');
 assert.doesNotMatch(await page.locator('#quota-panel').textContent(),/unsafe-secret-message/);assert.deepEqual(errors,[]);
});
test('quota pane: an available response with unknown subscription usage is conservatively displayed as unknown',async t=>{
 const f=await fixture(t,true,false,'',true),{page,context}=await f.newPage();await openQuota(page);
 await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await quotaStatus(page,'已完成手动额度查询');
 const body=(await f.request(quotaBase)).body,v=body.routes[0];v.status=v.snapshot.status='available';v.snapshot.complete=true;
 Object.assign(v.snapshot.pool,{id:'shared-fixture',owner:'fixture-team',scope:'team',verified:true});v.snapshot.windows[0].used_percent=null;
 await context.route('**/control/v1/projects/synthetic-ui/quota',r=>r.fulfill({status:200,json:body}));
 await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'已读取额度缓存');
 assert.match(await page.locator('#quota-list .row').filter({hasText:'fixture-a@1'}).textContent(),/额度：未知/);
});
test('quota pane: elapsed source age downgrades cached availability without HTTP polling',async t=>{
 const f=await fixture(t,true,false,'',true),{page,context,errors}=await f.newPage();await openQuota(page);
 await page.getByRole('button',{name:'刷新额度 fixture-a',exact:true}).click();await quotaStatus(page,'已完成手动额度查询');
 const body=(await f.request(quotaBase)).body,v=body.routes[0];v.status=v.snapshot.status='available';v.snapshot.complete=true;
 Object.assign(v.snapshot.pool,{id:'shared-fixture',owner:'fixture-team',scope:'team',verified:true});let reads=0;
 await context.route('**/control/v1/projects/synthetic-ui/quota',r=>{reads++;return r.fulfill({status:200,json:body})});
 await page.getByRole('button',{name:'读取额度缓存',exact:true}).click();await quotaStatus(page,'已读取额度缓存');
 const row=page.locator('#quota-list .row').filter({hasText:'fixture-a@1'});assert.match(await row.textContent(),/额度：可用观察/);
 await page.evaluate(()=>{const current=Date.now.bind(Date);Date.now=()=>current()+61000});
 await page.getByLabel('配置范围',{exact:true}).selectOption('global');
 assert.match(await row.textContent(),/额度：已过期/);assert.equal(reads,1);assert.deepEqual(errors,[]);
});
test('event history: reads durable metadata without creating task work and retains exact sequence on replay',async t=>{
 const f=await fixture(t,true),{page,task,errors}=await savedControlTask(t,f);
 assert.equal(await page.locator('#task-events').count(),1);
 await page.locator('#task-events summary').click();await page.getByRole('button',{name:'读取新事件',exact:true}).click();
 await page.locator('#events-status').filter({hasText:'已读取任务事件'}).waitFor();
 assert.match(await page.locator('#event-history').textContent(),/#1 · created/);
 const before=await f.request('/agent/v1/tasks/'+task.id);const first=await f.request('/agent/v1/tasks/'+task.id+'/events/page/0');
 assert.equal(first.body.events.length,1);assert.equal(first.body.next_after,1);
 await page.locator('#task-events').scrollIntoViewIfNeeded();await page.locator('#task-events').screenshot({path:path.join(repo,'.fusion-dev/implementation/event-page-desktop.png')});
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'original event layout must fit narrow windows');
 assert.ok(await page.locator('#event-history p').isVisible());await page.locator('#task-events').screenshot({path:path.join(repo,'.fusion-dev/implementation/event-page-mobile.png')});
 await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'暂无新事件'}).waitFor();
 assert.equal(await page.locator('#event-history p').count(),1);
 await page.getByRole('button',{name:'从头读取事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'已读取任务事件'}).waitFor();
 assert.equal(await page.locator('#event-history p').count(),1);const after=await f.request('/agent/v1/tasks/'+task.id);assert.deepEqual(after.body,before.body);assert.deepEqual(errors,[]);
});
test('event history: actual idle control events page without gaps or automatic state adoption',async t=>{
 const f=await fixture(t,true),{page,task,errors}=await savedControlTask(t,f);let current=task;
 for(let n=0;n<70;n++)for(const action of ['pause','continue']){
  const r=await f.request('/control/v1/tasks/'+task.id+'/'+action,'POST',{},'"p'+current.plan_revision+'-g'+current.generation+'-'+current.state+'"');
  assert.equal(r.status,200);current=r.body.task;
 }
 await page.locator('#task-events summary').click();
 for(const [next,count] of [[32,32],[64,64],[96,96],[128,128],[141,128]]){
  await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'已读取任务事件至 #'+next+'。'}).waitFor();
  assert.equal(await page.locator('#event-history p').count(),count);
 }
 const seq=await page.locator('#event-history p').allTextContents();assert.deepEqual(seq.map(s=>Number(s.match(/^#(\d+)/)[1])),Array.from({length:128},(_,n)=>n+14));
 assert.match(await page.locator('#task-detail').textContent(),/generation 0/);assert.match(await page.locator('#events-status').textContent(),/仅显示最近 128 条/);
 await page.getByRole('button',{name:'从头读取事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'已读取任务事件至 #32。'}).waitFor();
 assert.equal(await page.locator('#event-history p').count(),32);assert.deepEqual(errors,[]);
});
test('event history: malformed successful pages retain the original cursor and independent metadata',async t=>{
 const f=await fixture(t,true),{page,context,task,errors}=await savedControlTask(t,f);await page.locator('#task-events summary').click();
 await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'已读取任务事件'}).waitFor();
 const url='/agent/v1/tasks/'+task.id+'/events/page/1',original=(await f.request(url)).body,requests=[];
 for(const mutate of [v=>v.next_after=2,v=>{v.has_more=true},v=>{v.task_id='foreign-task'},v=>{v.events=[{task_id:task.id,seq:2,kind:'<svg>',run_id:'',generation:0}];v.next_after=2}]){
  const body=structuredClone(original);mutate(body);body.secret='never-render-this';
  await context.route('**/events/page/*',r=>{requests.push(new URL(r.request().url()).pathname);return r.fulfill({status:200,json:body})});
  await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'原事件和游标已保留'}).waitFor();
  assert.equal(await page.locator('#event-history p').count(),1);assert.doesNotMatch(await page.locator('#task-events').textContent(),/foreign-task|never-render-this|<svg>/);
  await context.unroute('**/events/page/*');
 }
 assert.deepEqual(requests,Array(4).fill(url));assert.deepEqual(errors,[]);
});
test('event history: a lost read reply retries the same page with no additional work',async t=>{
 const f=await fixture(t,true),{page,context,task,errors}=await savedControlTask(t,f);await page.locator('#task-events summary').click();const paths=[];
 await context.route('**/events/page/*',async r=>{paths.push(new URL(r.request().url()).pathname);if(paths.length===1){await r.fetch();return r.abort('failed')}return r.continue()});
 await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'原事件和游标已保留'}).waitFor();assert.equal(await page.locator('#event-history p').count(),0);
 await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'已读取任务事件'}).waitFor();
 assert.deepEqual(paths,Array(2).fill('/agent/v1/tasks/'+task.id+'/events/page/0'));assert.equal(await page.locator('#event-history p').count(),1);
 const body=(await f.request(paths[0])).body;assert.equal(body.events.length,1);assert.deepEqual(errors,[]);
});
test('event history: late reply for another selected task cannot replace current history',async t=>{
 const f=await fixture(t,true),{page,context,task,errors}=await savedControlTask(t,f);
 const preview=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui',goal:'另一个事件任务',required_roles:['design']});assert.equal(preview.status,200);
 const saved=await f.request('/agent/v1/tasks','POST',{preview_id:preview.body.preview_id||preview.body.id,plan_hash:preview.body.plan.hash},null,'fixture-event-other');assert.equal(saved.status,201);
 await page.getByRole('button',{name:'刷新任务列表',exact:true}).click();await page.getByLabel('查看任务 '+saved.body.id).waitFor();
 await page.locator('#task-events summary').click();let release,entered;const held=new Promise(r=>release=r),started=new Promise(r=>entered=r);
 await context.route('**/events/page/*',async r=>{if(r.request().url().includes(task.id)){const reply=await r.fetch();entered();await held;return r.fulfill({response:reply})}return r.continue()});
 try{
  await page.getByRole('button',{name:'读取新事件',exact:true}).click();await started;
  await page.getByLabel('查看任务 '+saved.body.id).click();await page.locator('#task-detail').filter({hasText:'另一个事件任务'}).waitFor();
  const arrived=page.waitForResponse(r=>r.url().includes(task.id+'/events/page/'));release();await arrived;
  assert.equal(await page.locator('#event-history p').count(),0);await page.locator('#task-events summary').click();
  await page.getByRole('button',{name:'读取新事件',exact:true}).click();await page.locator('#events-status').filter({hasText:'已读取任务事件'}).waitFor();
  assert.equal(await page.locator('#event-history p').count(),1);assert.deepEqual(errors,[]);
 }finally{release()}
});

test('start recovery: absent preparation clears only after independent advanced Task proof and a second absence read',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0;const prepares=[];
 await context.route('**/control/v1/tasks/*/start-request',async route=>{if(route.request().method()==='POST'){prepares.push({key:route.request().headers()['idempotency-key'],tag:route.request().headers()['if-match'],body:route.request().postData()});await route.abort('failed')}else await route.continue()});
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();assert.equal(prepares.length,1);assert.equal(starts,0);
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'核对失败'}).waitFor();await page.waitForFunction(()=>!document.getElementById('refresh-start').disabled);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true,'absence alone cannot fence a delayed preparation');
 const current=await f.request('/agent/v1/tasks/'+task.id);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/pause','POST',{},current.etag)).status,200);
 await context.route('**/agent/v1/tasks/'+task.id,async route=>route.fulfill({status:503,json:{error:{code:'task_unavailable'}}}));
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'核对失败'}).waitFor();await page.waitForFunction(()=>!document.getElementById('refresh-start').disabled);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await context.unroute('**/agent/v1/tasks/'+task.id);
 const substituted=await f.request('/agent/v1/tasks/'+task.id);substituted.body.goal='foreign-task-goal';
 await context.route('**/agent/v1/tasks/'+task.id,async route=>route.fulfill({status:200,headers:{ETag:substituted.etag},json:substituted.body}));
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'核对失败'}).waitFor();await page.waitForFunction(()=>!document.getElementById('refresh-start').disabled);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await context.unroute('**/agent/v1/tasks/'+task.id);
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'封存未启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'封存结果未确认'}).waitFor();await page.waitForFunction(()=>!document.getElementById('refresh-start').disabled);
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.waitForFunction(()=>document.getElementById('start-recovery').hidden,{},{timeout:1500});
 await page.locator('#task-detail').filter({hasText:'paused'}).waitFor();assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),false);assert.equal(prepares.length,2);assert.deepEqual(prepares[1],prepares[0]);assert.equal(starts,0);
 const original=prepares[0];assert.equal((await f.request('/control/v1/tasks/'+task.id+'/start-request','POST',JSON.parse(original.body),original.tag,original.key)).status,412);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/start','POST',JSON.parse(original.body),original.tag,original.key)).status,412);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});

test('start recovery: a delayed prepared record found after Task advancement cannot be discarded as absent',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let reads=0,starts=0;
 await context.route('**/control/v1/tasks/*/start-request',async route=>{if(route.request().method()==='POST'){await route.fetch();return route.abort('failed')}reads++;if(reads===1)return route.fulfill({status:404,json:{error:{code:'record_unavailable'}}});return route.continue()});
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();
 const pending=(await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request;assert.equal(pending.state,'prepared');
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/pause','POST',{},pending.etag)).status,200);
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'核对失败'}).waitFor();assert.equal(reads,2,'second read must catch a delayed original preparation');assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#start-record').filter({hasText:'prepared'}).waitFor();assert.match(await page.locator('#start-record').textContent(),new RegExp(pending.key));
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'封存未启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原启动请求已封存'}).waitFor();
 assert.equal(starts,0);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/start','POST',{role:'design'},pending.etag,pending.key)).status,409);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});

test('start recovery: lost preparation persists the original identity across host restart and seals late start',{timeout:25000},async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0;const prepares=[];
 await context.route('**/control/v1/tasks/*/start-request',async route=>{
  if(route.request().method()!=='POST'){await route.continue();return}
  prepares.push({body:route.request().postData(),key:route.request().headers()['idempotency-key'],tag:route.request().headers()['if-match']});await route.fetch();await route.abort('failed');
 });
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:/未确认|受理/}).waitFor();
 assert.equal(prepares.length,1,'original request must be persisted before dispatch');assert.equal(starts,0,'unconfirmed preparation must not dispatch');
 assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,0);
 const pending=(await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request;
 assert.equal(pending.state,'prepared');assert.equal(pending.key,prepares[0].key);
 assert.deepEqual(errors,[]);await context.close();await f.restart();
 const sends=[],restored=await f.newPage(async context=>{await context.route('**/control/v1/tasks/**',async route=>{if(route.request().method()==='POST')sends.push(route.request().url());await route.continue()})});
 await restored.page.locator('#start-recovery').waitFor({state:'visible'});await restored.page.locator('#start-record').filter({hasText:pending.key}).waitFor();
 assert.equal(sends.length,0,'opening a saved request is read-only');assert.equal(await restored.page.getByLabel('项目',{exact:true}).isDisabled(),true);
 assert.match(await restored.page.locator('#start-record').textContent(),/fixture-model-a/);assert.match(await restored.page.locator('#start-record').textContent(),/p1-g0-ready/);
 restored.page.once('dialog',d=>d.accept());await restored.page.getByRole('button',{name:'封存未启动请求',exact:true}).click();await restored.page.locator('#execution-status').filter({hasText:'原启动请求已封存'}).waitFor();
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request,null);
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/start','POST',{role:'design'},pending.etag,pending.key)).status,409);
 assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,0);assert.deepEqual(restored.errors,[]);
});

test('start recovery: committed lost reply reopens and confirms the original run without another start',{timeout:25000},async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0;
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.fetch();await route.abort('failed')});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();
 const pending=(await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request;
 assert.ok(pending,'a committed original start must survive lost reply');assert.equal(pending.state,'committed');assert.ok(pending.run_id);assert.equal(starts,1);
 assert.deepEqual(errors,[]);await context.close();await f.restart();
 let extraStarts=0;const restored=await f.newPage(async context=>{await context.route('**/control/v1/tasks/*/start',async route=>{extraStarts++;await route.continue()})});
 await restored.page.locator('#start-record').filter({hasText:pending.run_id}).waitFor();assert.equal(extraStarts,0);
 await restored.page.getByRole('button',{name:'重试原运行请求',exact:true}).click();await restored.page.locator('#run-record').filter({hasText:pending.run_id}).waitFor();
 await restored.page.waitForFunction(()=>document.getElementById('start-recovery').hidden);
 assert.equal(extraStarts,0,'known committed metadata must reconcile through reads');assert.equal((await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request,null);
 assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);
 assert.equal((await f.request('/agent/v1/tasks/'+task.id+'/runs/'+pending.run_id)).body.run.state,'cancelled');assert.deepEqual(restored.errors,[]);
});

test('start recovery: substituted acknowledgement keeps exact original identity until terminal journal and independent run agree',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0,acks=0;let actual;
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 await context.route('**/start-request/acknowledge',async route=>{acks++;const response=await route.fetch();actual=await response.json();const changed=structuredClone(actual);changed.request.run_id='forged-start-record-run';await route.fulfill({response,json:changed})});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'确认未核对'}).waitFor();
 assert.equal(starts,1);assert.equal(acks,1);assert.equal(await page.getByRole('button',{name:'重试启动记录确认',exact:true}).isVisible(),true);
 assert.ok(!(await page.locator('#start-record').textContent()).includes('forged-start-record-run'));assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request,null,'server ack was committed but its reply was not proven');
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.waitForFunction(()=>document.getElementById('start-recovery').hidden);
 assert.equal(starts,1);assert.equal(acks,1);assert.ok((await page.locator('#run-record').textContent()).includes(actual.request.run_id));assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);
 assert.deepEqual(await page.evaluate(()=>[localStorage.length,sessionStorage.length]),[0,0]);assert.deepEqual(errors,[]);
});

test('start recovery: lost seal reads exact abandoned history and original Magpie controls fit desktop and mobile',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);const originalKey='fixture-ui-seal-original';const tag='"p1-g0-ready"';
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/start-request','POST',{role:'design'},tag,originalKey)).status,200);
 await page.reload();await page.locator('#start-record').filter({hasText:originalKey}).waitFor();await page.waitForFunction(()=>!document.getElementById('abandon-start').disabled);
 await page.locator('#start-recovery').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/start-recovery-desktop.png'),fullPage:true});
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.locator('#start-recovery').scrollIntoViewIfNeeded();await page.screenshot({path:path.join(repo,'.fusion-dev/implementation/start-recovery-mobile.png'),fullPage:true});
 let seals=0,starts=0;await context.route('**/start-request/abandon',async route=>{seals++;await route.fetch();await route.abort('failed')});await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'封存未启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'封存结果未确认'}).waitFor();
 assert.equal(await page.getByRole('button',{name:'重试封存原启动请求',exact:true}).isVisible(),true);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原启动请求已封存'}).waitFor();
 assert.equal(seals,1);assert.equal(starts,0);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/start','POST',{role:'design'},tag,originalKey)).status,409);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,0);assert.deepEqual(errors,[]);
});

test('start recovery: startup finds another project and current defaults cannot replace its frozen model',async t=>{
 const f=await fixture(t,true,true,'hold');
 const preview=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui-other',goal:'另一个项目的原启动目标',required_roles:['design']});assert.equal(preview.status,200);
 const task=await f.request('/agent/v1/tasks','POST',{preview_id:preview.body.preview_id,plan_hash:preview.body.plan.hash},null,'fixture-other-task');assert.equal(task.status,201);
 assert.equal((await f.request('/control/v1/tasks/'+task.body.id+'/start-request','POST',{role:'design'},'"p1-g0-ready"','fixture-other-original-start')).status,200);
 const changed=await f.request('/control/v1/defaults/global','PUT',{layer:{roles:{design:{mode:'locked',route:{id:'fixture-b',revision:1},model:models.b,effort:{mode:'none'}}}}},'"0"');assert.equal(changed.status,201);
 const writes=[],{page,errors}=await f.newPage(async context=>{await context.route('**/control/v1/tasks/**',async route=>{if(route.request().method()==='POST')writes.push(route.request().url());await route.continue()})});
 await page.locator('#start-record').filter({hasText:'fixture-other-original-start'}).waitFor();assert.equal(await page.getByLabel('项目',{exact:true}).inputValue(),'synthetic-ui-other');assert.equal(writes.length,0);
 assert.ok((await page.locator('#groups').textContent()).includes(models.b));assert.ok((await page.locator('#start-record').textContent()).includes(models.a));assert.ok(!(await page.locator('#start-record').textContent()).includes(models.b));
 assert.equal((await f.request('/agent/v1/tasks/'+task.body.id)).body.generation,0);assert.deepEqual(errors,[]);
});

test('start recovery: malformed successful preparation cannot dispatch and explicit retry preserves original bytes',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);const prepares=[];let starts=0;
 await context.route('**/control/v1/tasks/*/start-request',async route=>{
  if(route.request().method()!=='POST'){await route.continue();return}
  prepares.push({body:route.request().postData(),key:route.request().headers()['idempotency-key'],tag:route.request().headers()['if-match']});const response=await route.fetch();
  if(prepares.length===1){const changed=await response.json();changed.request.plan.bindings.design.target.resolved_model=models.b;await route.fulfill({response,json:changed})}else await route.fulfill({response});
 });
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'保存结果未确认'}).waitFor();assert.equal(starts,0);assert.equal(prepares.length,1);
 assert.equal(await page.getByRole('button',{name:'重试保存原启动请求',exact:true}).isVisible(),true);assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重试保存原启动请求',exact:true}).click();await page.locator('#run-record').filter({hasText:'已知阶段执行'}).waitFor();await page.waitForFunction(()=>document.getElementById('start-recovery').hidden);
 assert.deepEqual(prepares[1],prepares[0]);assert.equal(prepares[0].tag,'"p1-g0-ready"');assert.equal(starts,1);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});

test('start recovery: malformed or empty reread retains the exact original and blocks new intent',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0;
 const lostPrepare=async route=>{if(route.request().method()==='POST'){await route.fetch();await route.abort('failed')}else await route.continue()};await context.route('**/control/v1/tasks/*/start-request',lostPrepare);
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'保存结果未确认'}).waitFor();
 const pending=(await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request;
 for(const change of [()=>null,v=>({...v,role:'review'}),v=>({...v,task:{...v.task,id:'foreign-task',goal:'do-not-render-forged-goal'}})]){
  const bad=change(structuredClone(pending));const failure=async route=>route.request().method()==='GET'?route.fulfill({status:200,json:{request:bad}}):route.fallback();await context.route('**/control/v1/tasks/*/start-request',failure);
  await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'核对失败'}).waitFor();assert.equal(await page.getByLabel('项目',{exact:true}).isDisabled(),true);assert.ok((await page.locator('#start-record').textContent()).includes(pending.key));assert.ok(!(await page.locator('#start-record').textContent()).includes('do-not-render-forged-goal'));
  await context.unroute('**/control/v1/tasks/*/start-request',failure);
 }
 await page.getByRole('button',{name:'重新核对原启动请求',exact:true}).click();await page.locator('#execution-status').filter({hasText:'已核对原阶段启动请求'}).waitFor();assert.equal(starts,0);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,0);assert.deepEqual(errors,[]);
});

test('start recovery: independent run failure cannot acknowledge or clear the original committed request',async t=>{
 const f=await fixture(t,true,false,'hold'),{page,context,task,errors}=await savedControlTask(t,f);let starts=0,acks=0;
 await context.route('**/control/v1/tasks/*/start',async route=>{starts++;await route.continue()});await context.route('**/start-request/acknowledge',async route=>{acks++;await route.continue()});
 const failure=route=>route.fulfill({status:503,json:{error:{code:'independent_run_unavailable'}}});await context.route('**/agent/v1/tasks/*/runs/*',failure);
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#execution-status').filter({hasText:'原运行请求已保留'}).waitFor();assert.equal(acks,0);
 await page.getByRole('button',{name:'重试原运行请求',exact:true}).click();await page.waitForFunction(()=>!document.getElementById('retry-execution').disabled);assert.equal(starts,1);assert.equal(acks,0);
 assert.equal((await f.request('/control/v1/projects/synthetic-ui/start-request')).body.request.state,'committed');assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);
 await context.unroute('**/agent/v1/tasks/*/runs/*',failure);await page.getByRole('button',{name:'重试原运行请求',exact:true}).click();await page.waitForFunction(()=>document.getElementById('start-recovery').hidden);
 assert.equal(starts,1);assert.equal(acks,1);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);assert.deepEqual(errors,[]);
});

async function savedWorkflow(t,kind='change'){
 const f=await fixture(t,true,false,'success',false,true),view=await f.newPage(),{page}=view;
 await page.getByLabel('配置范围').selectOption('task');
 assert.equal(await page.getByLabel('任务类型',{exact:true}).count(),1,'finite workflow selector must be present in original task form');
 await page.getByLabel('任务类型',{exact:true}).selectOption(kind);await page.getByLabel('任务目标').fill('<svg> 工作流方案核对');
 await page.getByRole('button',{name:'预览工作流任务',exact:true}).click();await status(page,'已核对冻结阶段计划');
 await page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(page,'任务已保存');await status(page,'已读取任务详情');
 const task=(await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks[0];await page.locator('#workflow-panel summary').click();return {...view,f,task};
}
test('workflow UI: original task form freezes five roles and explicit approval never starts the next stage',async t=>{
 const {f,page,context,task,errors}=await savedWorkflow(t);let starts=0;const writes=[];
 await context.route('**/control/v1/tasks/*/start',async r=>{starts++;await r.continue()});
 await context.route(/\/control\/v1\/tasks\/[^/]+\/workflow(?:\/.*)?$/,async r=>{if(r.request().method()==='POST')writes.push({url:r.request().url(),body:r.request().postDataJSON(),tag:r.request().headers()['if-match']});await r.continue()});
 assert.deepEqual((await f.request('/control/v1/tasks/'+task.id+'/plan')).body.required_roles,['design','implementation','testing','review','acceptance']);
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow,null);
 await page.getByLabel('附加流程',{exact:true}).selectOption('change');await page.getByRole('button',{name:'附加所选流程',exact:true}).click();await status(page,'流程已附加');
 assert.equal(starts,0);assert.equal(writes[0].tag,'"p1-g0-ready"');assert.equal(await page.getByLabel('运行阶段').inputValue(),'design');
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#run-record').filter({hasText:'已知阶段执行'}).waitFor();
 for(let i=0;i<12;i++){await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'已读取任务详情');if((await page.locator('#run-record').innerText()).includes('succeeded')&&(await page.locator('#workflow-record').innerText()).includes('等待冻结完整设计'))break}assert.ok((await page.locator('#workflow-record').innerText()).includes('等待冻结完整设计'));assert.ok(await page.getByLabel('设计运行 ID').inputValue());
 assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'批准当前方案',exact:true}).isDisabled(),true);
 for(const [label,value] of [['方案范围','.'],['方案约束','保持公开接口'],['方案接口','<img src=x onerror=alert(1)> 接口'],['验收标准','针对性回归通过']])await page.getByLabel(label,{exact:true}).fill(value);
 await page.getByRole('button',{name:'冻结当前设计',exact:true}).click();await status(page,'设计已冻结');
 const frozen=(await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow;assert.ok(frozen.design);assert.equal(frozen.approval,null);assert.equal(starts,1);
 assert.equal(await page.locator('#workflow-record img,#workflow-record svg').count(),0);assert.ok((await page.locator('#workflow-record').innerText()).includes(frozen.design.snapshot.acceptance_hash));
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'批准当前方案',exact:true}).click();await status(page,'当前方案已批准');
 const approved=(await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow;assert.equal(approved.approval.design_hash,frozen.design.snapshot.hash);assert.equal(approved.approval.plan_revision,1);assert.equal(starts,1);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,1);
 assert.deepEqual(writes.at(-1).body,{design_hash:frozen.design.snapshot.hash,acceptance_hash:frozen.design.snapshot.acceptance_hash});assert.equal(writes.at(-1).tag,'"p1-g1-ready"');
 assert.equal(await page.getByLabel('运行阶段').inputValue(),'implementation');assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),false);
 await page.locator('#workflow-panel').scrollIntoViewIfNeeded();await page.locator('#workflow-panel').screenshot({path:path.join(repo,'.fusion-dev/implementation/workflow-ui-desktop.png')});
 await page.setViewportSize({width:390,height:844});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));await page.locator('#workflow-panel').screenshot({path:path.join(repo,'.fusion-dev/implementation/workflow-ui-mobile.png')});
 await page.reload();await page.getByRole('button',{name:'查看任务 '+task.id,exact:true}).click();await status(page,'已读取任务详情');await page.locator('#workflow-panel summary').click();assert.ok((await page.locator('#workflow-record').innerText()).includes('当前计划已批准'));assert.equal(starts,1);
 assert.deepEqual(await page.evaluate(()=>[localStorage.length,sessionStorage.length]),[0,0]);assert.deepEqual(errors,[]);
});

for(const kind of ['investigate','review','bugfix'])test('workflow UI: explicit '+kind+' uses only its frozen finite roles',async t=>{
 const {f,page,task,errors}=await savedWorkflow(t,kind);
 const expected=kind==='investigate'?['design']:kind==='review'?['review']:['design','implementation','testing','review','acceptance'];
 assert.deepEqual((await f.request('/control/v1/tasks/'+task.id+'/plan')).body.required_roles,expected);
 await page.getByLabel('附加流程',{exact:true}).selectOption(kind);await page.getByRole('button',{name:'附加所选流程',exact:true}).click();await status(page,'流程已附加');
 const v=(await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow;assert.equal(v.definition.kind,kind);assert.deepEqual(v.definition.required_roles,expected);assert.equal(v.next,expected[0]);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,0);
 if(kind==='review'){assert.equal(await page.locator('#workflow-design-form').isVisible(),false);assert.equal(await page.getByRole('button',{name:'批准当前方案',exact:true}).isDisabled(),true)}
 assert.deepEqual(errors,[]);
});
async function freezeWorkflowDesign(view){
 const {page}=view;await page.getByLabel('附加流程').selectOption('change');await page.getByRole('button',{name:'附加所选流程',exact:true}).click();await status(page,'流程已附加');
 await page.getByRole('button',{name:'启动所选阶段',exact:true}).click();await page.locator('#run-record').filter({hasText:'已知阶段执行'}).waitFor();
 for(let i=0;i<12;i++){await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'已读取任务详情');if((await page.locator('#run-record').innerText()).includes('succeeded')&&(await page.locator('#workflow-record').innerText()).includes('等待冻结完整设计'))break}assert.ok((await page.locator('#workflow-record').innerText()).includes('等待冻结完整设计'));assert.ok(await page.getByLabel('设计运行 ID').inputValue());
 for(const label of ['方案范围','方案约束','方案接口','验收标准'])await page.getByLabel(label,{exact:true}).fill(label==='方案范围'?'.':label+' fixture');
 await page.getByRole('button',{name:'冻结当前设计',exact:true}).click();await status(page,'设计已冻结');
}
test('workflow UI: lost committed approval reply blocks writes and start until an explicit current read',async t=>{
 const view=await savedWorkflow(t),{f,page,context,task,errors}=view;await freezeWorkflowDesign(view);let approvals=0,starts=0;
 await context.route('**/control/v1/tasks/*/start',async r=>{starts++;await r.continue()});
 await context.route('**/control/v1/tasks/*/workflow/approve',async r=>{approvals++;await r.fetch();await r.fulfill({status:503,json:{error:{code:'lost_workflow_reply'}}})});
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'批准当前方案',exact:true}).click();await status(page,'决定结果尚待核对');
 assert.ok((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow.approval);assert.equal(await page.getByRole('button',{name:'批准当前方案',exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'已读取任务详情');assert.ok((await page.locator('#workflow-record').innerText()).includes('当前计划已批准'));assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),false);assert.equal(approvals,1);assert.equal(starts,0);assert.deepEqual(errors,[]);
});
test('workflow UI: changed Task condition cannot approve the previous display or start a stage',async t=>{
 const view=await savedWorkflow(t),{f,page,task,errors}=view;await freezeWorkflowDesign(view);
 const current=await f.request('/agent/v1/tasks/'+task.id);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/pause','POST',{},current.etag)).status,200);
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'批准当前方案',exact:true}).click();await status(page,'任务条件已变化');
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow.approval,null);assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'已读取任务详情');assert.equal(await page.getByRole('button',{name:'批准当前方案',exact:true}).isDisabled(),true);assert.deepEqual(errors,[]);
});
test('workflow UI: malformed cross-plan approval and extra private fields never render or enable execution',async t=>{
 const view=await savedWorkflow(t),{f,page,context,task,errors}=view;await freezeWorkflowDesign(view);
 const wfpath='**/control/v1/tasks/*/workflow';
 for(const edit of [body=>{body.workflow.approval={plan_revision:2,plan_hash:body.workflow.design.plan_hash,generation:1,design_hash:body.workflow.design.snapshot.hash,acceptance_hash:body.workflow.design.snapshot.acceptance_hash}},body=>{body.workflow.token='forbidden-private-value'}]){
  await context.route(wfpath,async r=>{const actual=await r.fetch(),body=await actual.json();edit(body);await r.fulfill({response:actual,json:body})});
  await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await page.locator('#task-detail-status').filter({hasText:'读取失败'}).waitFor();assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'批准当前方案',exact:true}).isDisabled(),true);assert.equal(await page.locator('#workflow-record').innerText(),'');await context.unroute(wfpath);
 }
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow.approval,null);assert.deepEqual(errors,[]);
});

test('workflow UI: denied management approval preserves the original form and never enables start',async t=>{
 const view=await savedWorkflow(t),{f,page,context,task,errors}=view;await freezeWorkflowDesign(view);let denied=0;
 await context.route('**/control/v1/tasks/*/workflow/approve',async r=>{denied++;await r.fulfill({status:401,json:{error:{code:'authorization_required'}}})});
 page.once('dialog',d=>d.accept());await page.getByRole('button',{name:'批准当前方案',exact:true}).click();await status(page,'管理授权已失效');
 assert.equal(denied,1);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow.approval,null);assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);assert.equal(await page.getByRole('button',{name:'批准当前方案',exact:true}).isDisabled(),true);assert.deepEqual(errors,[]);
});
test('workflow UI: late original task workflow cannot replace a newly selected task',async t=>{
 const {f,page,context,task,errors}=await savedWorkflow(t);
 const preview=await f.request('/control/v1/tasks/preview','POST',{project_id:'synthetic-ui',goal:'另一个独立审查任务',required_roles:['review']});assert.equal(preview.status,200);
 const other=await f.request('/agent/v1/tasks','POST',{preview_id:preview.body.preview_id,plan_hash:preview.body.plan.hash},null,'workflow-other-task');assert.equal(other.status,201);
 await page.getByRole('button',{name:'刷新任务列表',exact:true}).click();await status(page,'已读取 2 个任务');
 let release,entered;const gate=new Promise(r=>{release=r}),started=new Promise(r=>{entered=r});let first=true;
 await context.route('**/control/v1/tasks/'+task.id+'/workflow',async r=>{const actual=await r.fetch();if(first){first=false;entered();await gate}await r.fulfill({response:actual})});
 t.after(()=>release());await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await started;
 await page.getByRole('button',{name:'查看任务 '+other.body.id,exact:true}).click();await status(page,'已读取任务详情');const late=page.waitForResponse(r=>r.url().endsWith('/'+task.id+'/workflow'));release();await late;
 assert.ok((await page.locator('#task-detail').innerText()).includes(other.body.goal));assert.equal(await page.getByLabel('运行阶段').inputValue(),'review');assert.equal(await page.locator('#approve-design').isDisabled(),true);assert.deepEqual(errors,[]);
});

test('workflow UI: idle pause and continue cannot retroactively attach a workflow',async t=>{
 const {f,page,task,errors}=await savedWorkflow(t);let current=await f.request('/agent/v1/tasks/'+task.id);
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/pause','POST',{},current.etag)).status,200);current=await f.request('/agent/v1/tasks/'+task.id);
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/continue','POST',{},current.etag)).status,200);current=await f.request('/agent/v1/tasks/'+task.id);assert.ok(current.body.generation>0);assert.equal(current.body.state,'ready');
 await page.getByRole('button',{name:'重新读取任务',exact:true}).click();await status(page,'已读取任务详情');await page.getByLabel('附加流程').selectOption('change');
 assert.equal(await page.getByRole('button',{name:'附加所选流程',exact:true}).isDisabled(),true,'workflow attachment requires the original generation zero condition');
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow','POST',{kind:'change'},current.etag)).status,409);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow,null);assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,current.body.generation);assert.deepEqual(errors,[]);
});

test('workflow UI: multiple-role task cannot start before explicitly attaching its finite flow',async t=>{
 const {f,page,context,task,errors}=await savedWorkflow(t);let starts=0;
 await context.route('**/control/v1/tasks/*/start',async r=>{starts++;await r.continue()});
 assert.equal(await page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true,'multiple-role UI tasks require explicit workflow attachment before start');
 assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow,null);assert.equal(starts,0);assert.deepEqual(errors,[]);
});

test('workflow recovery: lost five-role submission restores its original plan without guessing or starting a flow',async t=>{
 const f=await fixture(t,true,false,'success',false,true),first=await f.newPage();let original;
 await first.page.getByLabel('配置范围').selectOption('task');await first.page.getByLabel('任务类型').selectOption('bugfix');await first.page.getByLabel('任务目标').fill('关闭后恢复原五角色修复任务');
 await first.page.getByRole('button',{name:'预览工作流任务',exact:true}).click();await status(first.page,'已核对冻结阶段计划');
 await first.context.route('**/agent/v1/tasks',async r=>{original={body:r.request().postDataJSON(),key:r.request().headers()['idempotency-key']};await r.fetch();await r.fulfill({status:503,json:{error:{code:'lost_five_role_submission'}}})});
 await first.page.getByRole('button',{name:'冻结提交任务',exact:true}).click();await status(first.page,'提交结果未确认');await first.context.close();await f.restart();
 const second=await f.newPage();await status(second.page,'已恢复原任务请求');const record=(await f.request('/control/v1/projects/synthetic-ui/submission')).body.submission;
 assert.deepEqual(record.preview.plan.required_roles,['design','implementation','testing','review','acceptance']);assert.equal(await second.page.getByLabel('任务类型').isDisabled(),true);
 let retried;await second.context.route('**/agent/v1/tasks',async r=>{retried={body:r.request().postDataJSON(),key:r.request().headers()['idempotency-key']};await r.continue()});
 await second.page.getByRole('button',{name:'重试同一任务提交',exact:true}).click();await status(second.page,'任务已保存');await status(second.page,'已读取任务详情');assert.deepEqual(retried,original);
 const task=(await f.request('/control/v1/projects/synthetic-ui/tasks')).body.tasks[0];assert.equal(task.generation,0);assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow,null);assert.equal(await second.page.getByRole('button',{name:'启动所选阶段',exact:true}).isDisabled(),true);
 await second.page.locator('#workflow-panel summary').click();await second.page.getByLabel('附加流程').selectOption('bugfix');await second.page.getByRole('button',{name:'附加所选流程',exact:true}).click();await status(second.page,'流程已附加');assert.equal((await f.request('/control/v1/tasks/'+task.id+'/workflow')).body.workflow.definition.kind,'bugfix');assert.equal((await f.request('/agent/v1/tasks/'+task.id)).body.generation,0);assert.deepEqual(first.errors,[]);assert.deepEqual(second.errors,[]);
});
