// The original Magpie header markup and app.css are shared by the host.
// No legacy boot, configuration, provider, plugin or update API is invoked.
const $=id=>document.getElementById(id);
const pages={agents:["Agents", "阶段角色选择使用 Fusion 面板；本窗口不会改写原客户端配置。"],providers:["Providers", "账号与授权只使用已登记的私有路线；此入口尚未开放登录或插件迁移。"],gateway:["Gateway", "Fusion 任务通过受控入口执行；旧 Gateway 配置与调用接口尚未开放。"],routing:["Routing", "阶段 locked / auto / inherit 在 Fusion 面板设置；Jev 保持 off。"],usage:["Usage", "Fusion 面板提供已登记路线的额度缓存与手动刷新；原用量页面尚未接入。"],sessions:["Sessions", "任务和运行记录使用 Fusion 工作台；原客户端会话读取与终端入口尚未开放。"],library:["Library", "原文件库尚未接入批准的项目范围。"],plugins:["Plugins", "插件管理、安装与账号迁移尚未开放。"],settings:["Settings", "此窗口只编辑 Fusion 的全局、项目与本次任务配置；原 Magpie 设置未开放。"]};
const scroll=new Map();let active="fusion";
for(const b of document.querySelectorAll(".actions button:disabled"))b.title="此原 Magpie 操作尚未开放";
if(location.protocol==="wails:"){
 document.body.classList.remove("web");
 if(/^Mac/.test(navigator.platform))document.body.classList.add("mac");
}
function show(view){
 if(view!=="fusion"&&!Object.hasOwn(pages,view))return;
 const current=active==="fusion"?$("view-fusion"):$("view-unavailable");scroll.set(active,current.scrollTop);active=view;
 for(const b of $("nav").querySelectorAll("button[data-view]")){
  b.classList.toggle("on",b.dataset.view===view);
  if(b.dataset.view===view)b.setAttribute("aria-current","page");else b.removeAttribute("aria-current");
 }
 $("prefs").classList.toggle("on",view==="settings");
 $("view-fusion").hidden=view!=="fusion";$("view-unavailable").hidden=view==="fusion";
 if(view!=="fusion"){$("unavailable-title").textContent=pages[view][0];$("unavailable-reason").textContent=pages[view][1]}
 const next=view==="fusion"?$("view-fusion"):$("view-unavailable");next.scrollTop=scroll.get(view)||0;
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
