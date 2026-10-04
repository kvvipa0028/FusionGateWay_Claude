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
async function fixture(t){
 const root=await fs.realpath(await fs.mkdtemp(path.join(os.tmpdir(),"fusion-stage-ui-")));
 await fs.chmod(root,0o700);
 for(const name of ["workspace","private","private/config"])await fs.mkdir(path.join(root,name),{mode:0o700});
 const doc={schema_version:1,revision:1,global:{roles:{design:{mode:"locked",route:{id:"fixture-a",revision:1},model:models.a,effort:{mode:"none"}}}},
 routes:Object.entries(models).map(([id,model])=>({id:"fixture-"+id,revision:1,native_route:"glm-cn-claude",model,account:"fixture-account-"+id,workspace:"fixture-workspace",credential_identity:"fixture-identity-"+id,runtime_version:"fixture-runtime",no_effort:true})),
 projects:[{id:"synthetic-ui",name:"界面验证项目",path:path.join(root,"workspace"),read:true,write:false,routes:Object.keys(models).map(id=>({id:"fixture-"+id,revision:1})),layer:{}}]};
 const source=path.join(root,"private/config/projects.json");await fs.writeFile(source,JSON.stringify(doc),{mode:0o600});
 const go=execFileSync("which",["go"],{encoding:"utf8"}).trim();
 const child=spawn("python3",[path.join(repo,"scripts/fusion/run-dev.py"),"--root",path.join(root,"state"),"--binary",binary,"--go",go,"--","fusion-control","--projects",source],{cwd:repo,stdio:["ignore","pipe","pipe"]});
 let output="",errors="",browser=null,token=null;
 t.after(async()=>{
  try{if(browser)await browser.close()}finally{
   if(child.exitCode===null){const done=once(child,"exit");child.kill("SIGTERM");await Promise.race([done,new Promise((_,reject)=>setTimeout(()=>reject(Error("fixture shutdown deadline")),8000))]);}
   assert.equal(child.exitCode,0);if(token){assert.ok(!output.includes(token));assert.ok(!errors.includes(token));}
   await fs.rm(root,{recursive:true,force:true});
  }
 });
 child.stderr.on("data",b=>errors+=b);
 const announcement=await new Promise((resolve,reject)=>{
  const timer=setTimeout(()=>reject(Error("fixture startup deadline")),15000);
  child.once("exit",()=>{clearTimeout(timer);reject(Error("fixture exited before startup"))});
  child.stdout.on("data",b=>{output+=b;if(output.includes("\n")){clearTimeout(timer);try{resolve(JSON.parse(output.split("\n")[0]))}catch{reject(Error("invalid startup response"))}}});
 });
 const origin=announcement.control_address;
 assert.equal(announcement.execution_enabled,false);assert.equal(announcement.jev,"off");
 token=(await fs.readFile(path.join(root,"state/data/fusion-gateway/control/management.token"),"utf8")).trim();
 for(const name of ["index.html","editor.mjs","model.mjs","editor.css"]){
  const response=await fetch(origin+"/fusion/"+name,{headers:{Authorization:"Bearer "+token},redirect:"error"});
  assert.equal(response.status,200);
  const actual=await response.text(),expected=await fs.readFile(path.join(repo,"internal/gui/assets/fusion",name),"utf8");
  assert.ok(actual===expected,"stage bundle differs from current source");
 }
 browser=await chromium.launch({channel:"chrome",headless:true});
 const newPage=async(beforeLoad=null)=>{
  const context=await browser.newContext({viewport:{width:1140,height:1000},extraHTTPHeaders:{Authorization:"Bearer "+token}});
  await context.route("**/*",r=>new URL(r.request().url()).origin===origin?r.continue():r.abort());
  const page=await context.newPage();
  const errors=[];page.on("pageerror",e=>errors.push(e.message));
  if(beforeLoad)await beforeLoad(context);
  await page.goto(origin+"/fusion/");await page.getByRole("status").filter({hasText:"已载入当前配置"}).waitFor();
  return {page,context,errors};
 };
 return {origin,newPage,async request(url,method="GET",body=null,tag=null){
  const response=await fetch(origin+url,{method,redirect:"error",headers:{Authorization:"Bearer "+token,"Content-Type":"application/json",...(tag?{"If-Match":tag}:{})},...(body?{body:JSON.stringify(body)}:{})});
  return {status:response.status,body:await response.json(),etag:response.headers.get("ETag")};
 }};
}
async function choose(page,label,id){
 await page.getByLabel(label+"模式",{exact:true}).selectOption("locked");
 await page.getByLabel(label+"模型",{exact:true}).selectOption(key(id));
 await page.getByLabel(label+"推理档位",{exact:true}).selectOption("none");
}
async function status(page,text){await page.getByRole("status").filter({hasText:text}).waitFor()}
test("actual private CLI/API: independent roles, fold/reload, explicit overwrite and safe model text",async t=>{
 const f=await fixture(t),{page,errors}=await f.newPage();
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
 await page.screenshot({path:path.join(repo,".fusion-dev/implementation/stage-editor-desktop.png"),fullPage:true});
 await page.setViewportSize({width:390,height:844});
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.screenshot({path:path.join(repo,".fusion-dev/implementation/stage-editor-mobile.png"),fullPage:true});
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
 assert.ok(await page.getByText("额度尚未读取。",{exact:false}).isVisible());
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
