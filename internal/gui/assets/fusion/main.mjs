// The original Magpie header markup and app.css are shared by the host.
// No legacy boot, configuration, provider, plugin or update API is invoked.
import {roles,labels,routeKey} from "./model.mjs";
const $=id=>document.getElementById(id);
const pages={agents:["Agents", "阶段角色选择使用 Fusion 面板；本窗口不会改写原客户端配置。"],providers:["Providers", "账号与授权只使用已登记的私有路线；此入口尚未开放登录或插件迁移。"],gateway:["Gateway", "Fusion 任务通过受控入口执行；旧 Gateway 配置与调用接口尚未开放。"],routing:["Routing", "阶段 locked / auto / inherit 在 Fusion 面板设置；Jev 保持 off。"],usage:["Usage", "Fusion 面板提供已登记路线的额度缓存与手动刷新；原用量页面尚未接入。"],sessions:["Sessions", "任务和运行记录使用 Fusion 工作台；原客户端会话读取与终端入口尚未开放。"],library:["Library", "原文件库尚未接入批准的项目范围。"],plugins:["Plugins", "插件管理、安装与账号迁移尚未开放。"],settings:["Settings", "此窗口只编辑 Fusion 的全局、项目与本次任务配置；原 Magpie 设置未开放。"]};
const scroll=new Map();let active="fusion";
let providerState={project:"",routes:[],scope:"project",blocked:true,loading:true,error:""},providerSignature="",providerHandlers={},dialogOrigin=null;
const node=(tag,text,cls)=>{const n=document.createElement(tag);if(text!==null)n.textContent=text;if(cls)n.className=cls;return n};
const pane=view=>$(view==="fusion"?"view-fusion":view==="providers"?"view-providers":"view-unavailable");
export function configureProviders(handlers){providerHandlers=handlers}
function closeProvider(focus=true){
 const open=!$("modal").hidden;$("modal").hidden=true;$("modal").firstElementChild.replaceChildren();
 document.querySelector(".top").inert=false;for(const m of document.querySelectorAll("main.view"))m.inert=false;
 for(const row of $("providers").children){row.classList.remove("selected");row.setAttribute("aria-expanded","false")}
 if(open&&focus&&dialogOrigin?.isConnected)dialogOrigin.focus({preventScroll:true});dialogOrigin=null;
}
function openProvider(route,row){
 if(!providerState.project||!providerState.routes.some(r=>routeKey(r)===routeKey(route)))return;
 closeProvider(false);dialogOrigin=row;row.classList.add("selected");row.setAttribute("aria-expanded","true");
 const modal=$("modal"),dialog=modal.firstElementChild,ed=node("div",null,"editor framed"),head=node("div",null,"ehead"),body=node("div",null,"ebody"),bar=node("div",null,"bar");
 dialog.setAttribute("role","dialog");dialog.setAttribute("aria-modal","true");dialog.setAttribute("aria-label","模型与账号详情");dialog.tabIndex=-1;
 head.append(node("b",route.model));
 const scopes={project:"项目配置",global:"全局默认",task:"本次任务"};
 for(const text of ["项目："+providerState.project+"；配置范围："+scopes[providerState.scope],"账号："+route.account,"工作区："+route.workspace,"执行路径："+route.id+"@"+route.revision+"；Runtime："+route.runtime_version,"计费："+(route.billing_path||"未知")+"；"+(route.billing_known?"已核验":"未核验"),"准入："+(route.admitted?"已登记为准入，启动时仍须核验":"未准入，仅能选择草稿"),"模型锁定："+({controlled_calls:"调用受控",primary_only:"仅主调用",unverified:"未核验"}[route.lock_enforcement]),"推理档位："+(route.efforts.length?route.efforts.join("、"):"无已登记 effort")+(route.no_effort?"；允许不使用 effort":""),"默认 effort："+(route.default_effort||"未登记"),"能力："+(route.capabilities.join("、")||"未登记")])body.append(node("p",text,"details"));
 if(route.plugin_version)body.append(node("p","插件版本："+route.plugin_version,"details"));
 const label=node("label","应用到阶段"),choice=node("select",null,"field");choice.setAttribute("aria-label","应用到阶段");
 for(const role of roles){const option=node("option",labels[role]);option.value=role;choice.append(option)}label.append(choice);body.append(label,node("p","将替换所选阶段的本地模型选择；推理档位需重新确认，保存后生效。其他阶段保持原值。","details"));
 const cancel=node("button","关闭详情","text"),apply=node("button","应用到阶段草稿","text primary");cancel.type=apply.type="button";
 cancel.onclick=()=>closeProvider();apply.disabled=providerState.blocked;
 const project=providerState.project;
 apply.onclick=()=>{
  if(providerState.blocked||project!==providerState.project||!providerState.routes.some(r=>routeKey(r)===routeKey(route)))return;
  const role=choice.value;if(providerHandlers.select?.({project,role,route})!==true)return;
  closeProvider(false);show("fusion");providerHandlers.focus?.(role);
 };
 bar.append(cancel,apply);ed.append(head,body,bar);dialog.replaceChildren(ed);modal.hidden=false;
 document.querySelector(".top").inert=true;for(const m of document.querySelectorAll("main.view"))m.inert=true;cancel.focus();
}
export function updateProviders(next){
 const signature=JSON.stringify(next);if(signature===providerSignature)return;
 providerSignature=signature;providerState=structuredClone(next);closeProvider();
 const list=$("providers"),status=$("fileError");list.replaceChildren();status.hidden=false;status.setAttribute("role","status");status.setAttribute("aria-live","polite");
 status.replaceChildren(node("p",next.loading?"正在读取当前项目的模型与账号…":next.error||"项目："+next.project+"；账号登录和阶段执行资格仍以核验结果为准。","details"));
 const reload=node("button","重新载入模型与账号","text action");reload.type="button";reload.disabled=next.loading||next.blocked;reload.onclick=()=>providerHandlers.reload?.();status.append(reload);
 $("addProvider").title="账号新增和登录须接入已登记的受控路线；本窗口尚未开放。";
 for(const route of next.routes){
  const row=node("div",null,"row provider"),who=node("div",null,"who"),pill=node("span",null,"key "+(route.admitted&&route.billing_known?"acct":"none"));
  row.dataset.id=route.id;row.dataset.revision=String(route.revision);row.setAttribute("role","button");row.tabIndex=0;row.setAttribute("aria-label","模型与账号 "+route.id+"@"+route.revision);row.setAttribute("aria-expanded","false");
  who.append(node("div",route.model,"name"),node("div",route.account+" · "+route.id+"@"+route.revision,"sub"));
  const state=route.admitted&&route.billing_known?"已登记准入":!route.admitted?"未准入":"计费未核验";pill.append(node("span",state));pill.title=state+"；不代表已登录、额度可用或任务已获准启动。";
  row.append(who,pill,node("span","›","chev"));row.onclick=()=>openProvider(route,row);row.onkeydown=e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();openProvider(route,row)}};list.append(row);
 }
 if(!next.routes.length)list.append(node("p",next.loading?"正在载入…":next.project?"此项目没有已登记模型。":"尚无可读取的项目模型。","none"));
}
$("modal").addEventListener("click",e=>{if(e.target===$("modal"))closeProvider()});
document.addEventListener("keydown",e=>{
 if($("modal").hidden)return;
 if(e.key==="Escape"){e.preventDefault();closeProvider();return}
 if(e.key!=="Tab")return;
 const focusable=[...$("modal").querySelectorAll('button:not(:disabled),select:not(:disabled)')],first=focusable[0],last=focusable.at(-1);
 if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus()}
});
for(const b of document.querySelectorAll(".actions button:disabled"))b.title="此原 Magpie 操作尚未开放";
if(location.protocol==="wails:"){
 document.body.classList.remove("web");
 if(/^Mac/.test(navigator.platform))document.body.classList.add("mac");
}
function show(view){
 if(view!=="fusion"&&!Object.hasOwn(pages,view))return;
 closeProvider(false);const current=pane(active);scroll.set(active,current.scrollTop);active=view;
 for(const b of $("nav").querySelectorAll("button[data-view]")){
  b.classList.toggle("on",b.dataset.view===view);
  if(b.dataset.view===view)b.setAttribute("aria-current","page");else b.removeAttribute("aria-current");
 }
 $("prefs").classList.toggle("on",view==="settings");
 $("view-fusion").hidden=view!=="fusion";$("view-providers").hidden=view!=="providers";$("view-unavailable").hidden=view==="fusion"||view==="providers";
 if(view!=="fusion"&&view!=="providers"){$("unavailable-title").textContent=pages[view][0];$("unavailable-reason").textContent=pages[view][1]}
 const next=pane(view);next.scrollTop=scroll.get(view)||0;
 const button=$("nav").querySelector("button.on");if(button)button.scrollIntoView({block:"nearest",inline:"nearest"});
}
for(const b of $("nav").querySelectorAll("button[data-view]"))b.addEventListener("click",()=>{show(b.dataset.view);b.blur()});
$("prefs").addEventListener("click",()=>show("settings"));
$("return-fusion").addEventListener("click",()=>{show("fusion");$("nav").querySelector('[data-view="fusion"]').focus()});
// Same progressive tightening and phone layout as Magpie app.js fitTop.
function fitTop(){
 const top=document.querySelector(".top"),nav=$("nav"),brand=document.querySelector(".brand"),actions=document.querySelector(".actions");
 top.classList.remove("tight","cramped","inrow","crowded","packed");
 if(document.body.classList.contains("web")&&matchMedia("(max-width: 760px), (pointer: coarse) and (max-width: 1024px)").matches)return;
 const fits=()=>{
  const a=actions.getBoundingClientRect(),rect=top.getBoundingClientRect(),style=getComputedStyle(top);
  if(a.right>rect.right-parseFloat(style.paddingRight)+0.5)return false;
  const left=brand.offsetParent?brand.getBoundingClientRect().right:rect.left+parseFloat(style.paddingLeft),n=nav.getBoundingClientRect();
  return left+8<=n.left&&n.right+8<=a.left;
 };
 if(fits())return;
 for(const name of ["tight","cramped","inrow","crowded","packed"]){top.classList.add(name);if(fits())break}
}
const fit=new ResizeObserver(fitTop);for(const node of document.querySelectorAll(".top,.brand,.actions"))fit.observe(node);
matchMedia("(max-width: 760px), (pointer: coarse) and (max-width: 1024px)").addEventListener("change",fitTop);fitTop();
