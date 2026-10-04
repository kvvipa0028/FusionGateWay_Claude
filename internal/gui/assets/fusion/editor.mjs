import {roles,labels,groups,copy,expandLayer,groupShared,shareGroup,exactRoute,targetFor,routeKey,layerIssues} from "./model.mjs";
const $=id=>document.getElementById(id);
const state={project:"",scope:"project",routes:[],layers:{},tags:{},config:{},presets:[],preset:null,expanded:new Set(),dirty:new Set(),loading:true,saving:false,uncertain:null,notice:"正在读取本机配置…"};
const messages={route_unavailable:"模型或路线版本已失效，请重新选择。",effort_unspecified:"请选择推理档位。",effort_unsupported:"该模型不支持此推理选择。",candidates_missing:"请明确添加批准的候选。",candidate_duplicate:"同一路线版本不能重复。",mode_invalid:"配置模式无效。",lock_exception_unaccepted:"此配置含未接受的锁定例外。"};
function el(tag,text,attrs={}){const n=document.createElement(tag);if(text!==null)n.textContent=text;for(const[k,v]of Object.entries(attrs))n.setAttribute(k,v);return n}
function option(select,value,text){select.append(el("option",text,{value}))}
class RequestError extends Error{constructor(status,code){super("request_failed");this.status=status;this.code=code}}
function defaultSnapshot(value,etag,status=500,fallback){
 try{
  if(!/^"(0|[1-9][0-9]*)"$/.test(etag)||!Number.isSafeInteger(value?.revision)||value.revision<0||etag!=='"'+value.revision+'"')throw Error();
  const layer=value.configured===false&&value.revision===0&&fallback!==undefined?fallback:value.layer;
  if(!layer||typeof layer!=="object")throw Error();
  return {layer:expandLayer(layer),etag};
 }catch{throw new RequestError(status,"response_unavailable")}
}
async function request(path,options={}){
 let response;try{response=await fetch(path,{...options,cache:"no-store",credentials:"omit",redirect:"error",headers:{"Content-Type":"application/json",...(options.headers||{})}})}catch{throw new RequestError(0,"transport_unconfirmed")}
 let body;try{body=await response.json()}catch{throw new RequestError(response.status,"response_unavailable")}
 if(!response.ok)throw new RequestError(response.status,body.error?.code);
 return {body,etag:response.headers.get("ETag")};
}
function errorText(e,saving=false){
 if(e.status===412)return"配置已被其他窗口更新。本地修改已保留；重新载入后再保存。";
 if(e.status===401||e.status===403)return"管理授权已失效，请重新连接本机服务。";
 if(e.status===422)return"缺少有效阶段模型或执行准入，无法预览执行。";
 if(e.status===503)return"本机服务已撤销或当前功能不可用，请重新连接。";
 if(e.status===0)return saving?"保存状态未确认。请重试同一保存，或重新载入核对。":"本机配置读取失败，请重新载入。";
 return"请求未完成，请检查配置并重新载入。";
}
const disabled=()=>state.loading||state.saving||!!state.uncertain;
function activeLayer(){return state.layers[state.scope]||expandLayer()}
function setBinding(role,binding){state.layers[state.scope].roles[role]=binding;state.dirty.add(state.scope);state.notice="本地选择尚未保存。";$("preview").hidden=true;render()}
function controlsFor(target,onchange,prefix){
 const fragment=document.createDocumentFragment(),routeLabel=el("label","模型"),select=el("select",null,{"aria-label":prefix+"模型"});
 option(select,"","请选择已登记模型");
 for(const r of state.routes){if(Number.isSafeInteger(r.revision))option(select,routeKey(r),r.model+" · "+r.account+" · "+r.id+"@"+r.revision)}
 const current=exactRoute(target,state.routes);
 if(current)select.value=routeKey(current);
 else if(target?.route){option(select,"expired","已失效："+(target.model||target.route.id));select.value="expired"}
 select.disabled=disabled();routeLabel.append(select);
 select.onchange=()=>{const r=state.routes.find(r=>routeKey(r)===select.value);if(r)onchange(targetFor(r))};
 const effortLabel=el("label","推理档位"),effort=el("select",null,{"aria-label":prefix+"推理档位"});
 option(effort,"","请选择推理档位");
 if(current?.no_effort)option(effort,"none","不使用 effort");
 if(current?.default_effort && (current.efforts||[]).includes(current.default_effort))option(effort,"default","模型默认（"+current.default_effort+"）");
 for(const v of current?.efforts||[])option(effort,"explicit:"+v,v);
 const value=target?.effort?.mode==="explicit"?"explicit:"+target.effort.value:target?.effort?.mode;
 if(value){if(![...effort.options].some(o=>o.value===value))option(effort,value,"已失效："+value);effort.value=value}
 effort.disabled=disabled()||!current;effortLabel.append(effort);
 effort.onchange=()=>{const next=copy(target);next.effort=effort.value.startsWith("explicit:")?{mode:"explicit",value:effort.value.slice(9)}:{mode:effort.value};onchange(next)};
 fragment.append(routeLabel,effortLabel);
 return {fragment,current};
}
function routeDetails(route){
 const p=el("p",null,{class:"details"});
 if(!route){p.textContent="请选择具体模型；不会自动改用列表中的其他模型。";return p}
 const lock={controlled_calls:"调用受控",primary_only:"仅主调用",unverified:"锁定能力未核验"}[route.lock_enforcement]||"锁定能力未知";
 p.textContent="账号："+route.account+"；计费："+route.billing_path+"；"+lock+"。额度尚未读取。";
 if(!route.admitted||!route.billing_known){p.classList.add("warning");p.append(el("span"," 路线尚未准入，可保存草稿选择。"))}
 return p;
}
function roleEditor(role){
 const row=el("div",null,{class:"role","data-role":role}),b=activeLayer().roles[role];
 row.append(el("div",labels[role],{class:"role-title"}));
 const controls=el("div",null,{class:"controls"}),modeLabel=el("label","模式"),mode=el("select",null,{"aria-label":labels[role]+"模式"});
 for(const[v,t]of [["inherit","继承"],["locked","指定并锁定"],["auto","批准候选内自动"]])option(mode,v,t);
 mode.value=b.mode;mode.disabled=disabled();modeLabel.append(mode);controls.append(modeLabel);
 mode.onchange=()=>setBinding(role,mode.value==="auto"?{mode:"auto",candidates:[]}:mode.value==="locked"?{mode:"locked",route:null,model:"",effort:null}:{mode:"inherit"});
 if(b.mode==="locked"){
  const fields=controlsFor(b,target=>setBinding(role,{...copy(b),...target}),labels[role]);
  controls.append(fields.fragment);row.append(controls,routeDetails(fields.current));
 }else if(b.mode==="auto"){
  row.append(controls,el("p","只在下方明确批准的候选中选择，候选失败不扩大清单。",{class:"details"}));
  (b.candidates||[]).forEach((candidate,index)=>{
   const item=el("div",null,{class:"candidate"}),grid=el("div",null,{class:"controls"});
   const fields=controlsFor(candidate,target=>{const next=copy(b);next.candidates[index]=target;setBinding(role,next)},labels[role]+"候选"+(index+1));
   const remove=el("button","移除候选",{"aria-label":labels[role]+"移除候选"+(index+1)});remove.disabled=disabled();
   remove.onclick=()=>{const next=copy(b);next.candidates.splice(index,1);setBinding(role,next)};
   grid.append(fields.fragment,remove);item.append(grid,routeDetails(fields.current));row.append(item);
  });
  const add=el("button","添加批准候选",{"aria-label":labels[role]+"添加批准候选"});add.disabled=disabled();
  add.onclick=()=>{const next=copy(b);next.candidates.push({route:null,model:"",effort:null});setBinding(role,next)};row.append(add);
 }else{
  row.append(controls);const lower=state.scope==="task"?["project","global"]:state.scope==="project"?["global"]:[];
  let inherited=null;for(const scope of lower){const candidate=expandLayer(state.config[scope]||{}).roles[role];if(candidate.mode!=="inherit"){inherited=candidate;break}}
  row.append(el("p",inherited?"继承已保存的"+(inherited.model||"自动候选")+"完整绑定。":"较低层尚无可继承的模型配置。",{class:"details"}));
 }
 const issue=layerIssues(activeLayer(),state.routes).find(i=>i.role===role);
 if(issue)row.append(el("p",messages[issue.code],{class:"error",role:"alert"}));
 return row;
}
function render(){
 const focus=document.activeElement?.getAttribute("aria-label"),root=$("groups");root.replaceChildren();
 if(state.layers[state.scope])for(const group of groups){
  const section=el("section",null,{class:"group"}),heading=el("div",null,{class:"group-heading"});
  heading.append(el("h2",group.title));section.append(heading,roleEditor(group.roles[0]));
  if(group.roles.length>1){
   const separate=!groupShared(activeLayer(),group.id);
   if(separate)heading.append(el("span","两个角色分别设置"));
   const open=state.expanded.has(group.id),toggle=el("button",open?"收起独立设置":"展开独立"+labels[group.roles[1]]+"设置",{"aria-expanded":String(open),"aria-label":group.title+"展开"});
   toggle.disabled=disabled();toggle.onclick=()=>{open?state.expanded.delete(group.id):state.expanded.add(group.id);render()};heading.append(toggle);
   if(open){
    section.append(roleEditor(group.roles[1]));
    const tools=el("div",null,{class:"group-tools"}),merge=el("button","共用"+labels[group.roles[0]]+"设置",{"aria-label":group.title+"共用"});
    merge.disabled=disabled()||!separate;
    merge.onclick=()=>{if(!window.confirm("将覆盖独立的"+labels[group.roles[1]]+"模型与推理设置，改为共用"+labels[group.roles[0]]+"完整绑定。继续？"))return;state.layers[state.scope]=shareGroup(activeLayer(),group.id);state.dirty.add(state.scope);state.notice="本地选择尚未保存。";$("preview").hidden=true;render()};
    tools.append(merge,el("span","收起仅隐藏控件，始终保留两个角色的值。"));section.append(tools);
   }
  }
  root.append(section);
 }
 $("scope").disabled=disabled();$("project").disabled=disabled();$("reload").disabled=state.loading||state.saving;
 $("preset").disabled=disabled();$("preset-version").disabled=disabled();$("apply-preset").disabled=disabled()||!$("preset").value;
 $("task").hidden=state.scope!=="task";$("goal").disabled=disabled();$("task-role").disabled=disabled();
 $("save").textContent=state.uncertain?"重试同一保存":state.scope==="task"?"预览单阶段任务":state.scope==="global"?"保存全局默认":"保存项目配置";
 $("save").disabled=state.loading||state.saving||!state.project||(!state.uncertain&&layerIssues(activeLayer(),state.routes).length>0);
 $("status").textContent=state.notice;$("status").className=state.error?"error":"";
 $("preset-source").textContent=state.preset?"本次任务使用预设 "+state.preset.id+"@"+state.preset.revision:"";
 if(focus)[...document.querySelectorAll("[aria-label]")].find(n=>n.getAttribute("aria-label")===focus)?.focus({preventScroll:true});
}
async function loadProject(id){
 state.loading=true;state.error=false;state.notice="正在读取项目配置…";render();
 try{
  const base="/control/v1/projects/"+encodeURIComponent(id);
  const [config,global,project,presets]=await Promise.all([request(base+"/configuration"),request("/control/v1/defaults/global"),request(base+"/defaults"),request(base+"/presets")]);
  const globalDefault=defaultSnapshot(global.body,global.etag,500,config.body.configuration.global||{}),projectDefault=defaultSnapshot(project.body,project.etag,500,config.body.configuration.project||{});
  state.project=id;state.routes=config.body.configuration.routes||[];state.config={global:globalDefault.layer,project:projectDefault.layer};
  state.layers={global:expandLayer(state.config.global),project:expandLayer(state.config.project),task:expandLayer()};
  state.tags={global:global.etag,project:project.etag};state.presets=presets.body.presets||[];
  state.preset=null;state.uncertain=null;state.dirty.clear();state.expanded.clear();
  $("preset").replaceChildren();option($("preset"),"","不载入预设");for(const p of state.presets)option($("preset"),p.id,p.name+" · 当前版本 "+p.revision);
  $("preset-version").value="";$("preview").hidden=true;state.notice="已载入当前配置。保存选择不会启动模型。";
 }catch(e){state.project="";state.layers={};state.error=true;state.notice=errorText(e)}
 state.loading=false;render();
}
$("scope").onchange=()=>{state.scope=$("scope").value;state.error=false;state.notice=state.dirty.has(state.scope)?"本地选择尚未保存。":"已切换配置范围。";$("preview").hidden=true;render()};
$("project").onchange=()=>{const id=$("project").value;if(state.dirty.size&&!window.confirm("重新载入项目会丢弃未保存的选择。继续？")){$("project").value=state.project;return}loadProject(id)};
$("reload").onclick=()=>{if((state.dirty.size||state.uncertain)&&!window.confirm("重新载入并核对服务端配置，本地未保存的选择将被丢弃。继续？"))return;loadProject(state.project||$("project").value)};
$("preset").onchange=()=>{const p=state.presets.find(p=>p.id===$("preset").value);$("preset-version").value=p?String(p.revision):"";render()};
$("apply-preset").onclick=async()=>{
 const revision=Number($("preset-version").value),id=$("preset").value;
 if(!/^[1-9][0-9]*$/.test($("preset-version").value)||!Number.isSafeInteger(revision)){state.error=true;state.notice="请选择明确的正整数预设版本。";render();return}
 if(state.dirty.has(state.scope)&&!window.confirm("载入预设会覆盖当前范围的五角色本地选择。继续？"))return;
 state.loading=true;render();
 try{const p=await request("/control/v1/projects/"+encodeURIComponent(state.project)+"/presets/"+encodeURIComponent(id)+"/versions/"+revision);state.layers[state.scope]=expandLayer(p.body.layer);if(state.scope==="task")state.preset={id,revision};state.dirty.add(state.scope);state.error=false;state.notice="已载入明确版本，尚未保存。"}catch(e){state.error=true;state.notice=errorText(e)}
 state.loading=false;render();
};
$("save").onclick=async()=>{
 const scope=state.scope,id=state.project;state.saving=true;state.error=false;render();
 if(scope==="task"){
  try{const goal=$("goal").value.trim();if(!goal){state.notice="请填写任务目标。";return}
   const body={project_id:id,goal,required_roles:[$("task-role").value],task:activeLayer()};if(state.preset)body.preset=state.preset;
   const preview=await request("/control/v1/tasks/preview",{method:"POST",body:JSON.stringify(body)});
   $("preview").replaceChildren(el("h2","服务端计划预览"));for(const b of preview.body.plan.bindings)$("preview").append(el("p",labels[b.role]+"："+(b.target?.resolved_model||"批准候选内自动选择")));
   $("preview").hidden=false;state.notice="已核对冻结阶段计划，尚未提交或启动任务。";
  }catch(e){state.error=true;state.notice=errorText(e)}
  finally{state.saving=false;render()}
  return;
 }
 const attempt=state.uncertain||{scope,url:scope==="global"?"/control/v1/defaults/global":"/control/v1/projects/"+encodeURIComponent(id)+"/defaults",etag:state.tags[scope],body:JSON.stringify({layer:activeLayer()})};
 try{
  const saved=await request(attempt.url,{method:"PUT",headers:{"If-Match":attempt.etag},body:attempt.body});
  const snapshot=defaultSnapshot(saved.body.default_layer,saved.etag,200);
  state.layers[scope]=snapshot.layer;state.config[scope]=copy(snapshot.layer);state.tags[scope]=snapshot.etag;
  state.uncertain=null;state.dirty.delete(scope);state.notice="已保存版本 "+saved.etag?.replaceAll('"',"")+"，未启动模型。";
 }catch(e){const uncertain=e.status===0||(e.status>=200&&e.status<300&&e.code==="response_unavailable");state.error=true;state.notice=uncertain?errorText(new RequestError(0,"transport_unconfirmed"),true):errorText(e,true);if(uncertain)state.uncertain=attempt;else state.uncertain=null}
 state.saving=false;render();
};
for(const role of roles)option($("task-role"),role,labels[role]);
async function boot(){
 try{const list=await request("/control/v1/projects");for(const p of list.body.projects)option($("project"),p.id,p.id);if(!list.body.projects.length){state.loading=false;state.notice="尚无已登记项目，请先在本机配置中登记项目。";render();return}await loadProject(list.body.projects[0].id)}
 catch(e){state.loading=false;state.error=true;state.notice=errorText(e);render()}
}
boot();
