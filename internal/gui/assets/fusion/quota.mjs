// Read-only observations. Neither a successful read nor a percentage admits a run.
const $=id=>document.getElementById(id);
const statuses={available:"可用观察",zero:"已耗尽",unknown:"未知",stale:"已过期",unverified:"未核验",auth_required:"需要授权",unsupported:"不支持查询"};
const refKey=r=>JSON.stringify([r.id,r.revision]);
const positive=n=>Number.isSafeInteger(n)&&n>0;
const text=v=>typeof v==="string"&&v.length<=1024&&!/[\u0000-\u001f\u007f]/.test(v);
const percent=v=>v===null||(typeof v==="number"&&Number.isFinite(v)&&v>=0&&v<=100);
const time=v=>typeof v==="string"&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/.test(v)&&Number.isFinite(Date.parse(v));
const identityKey=i=>JSON.stringify([i.provider,i.account,i.workspace,i.region,i.generation]);
function validIdentity(i,route){return i&&text(i.provider)&&i.provider!==""&&text(i.region)&&i.region!==""&&positive(i.generation)&&i.account===route.account&&i.workspace===route.workspace}
function validPool(p,i){
 if(!p||typeof p.verified!=="boolean"||![p.id,p.provider,p.region,p.scope,p.owner].every(text))return false;
 return !p.verified||(!!p.id&&p.provider===i.provider&&p.region===i.region&&!!p.owner&&["account","workspace","team"].includes(p.scope));
}
function validSnapshot(s,i,status){
 if(!s||identityKey(s.identity||{})!==identityKey(i)||s.status!==status||!text(s.source)||!s.source||typeof s.complete!=="boolean"||!time(s.received_at)||(s.observed_at!==null&&!time(s.observed_at))||!validPool(s.pool,i))return false;
 if(!Array.isArray(s.windows)||s.windows.length>32||!Array.isArray(s.resources)||s.resources.length>32)return false;
 for(const w of s.windows)if(!w||![w.name,w.kind,w.unit].every(text)||!percent(w.used_percent)||!percent(w.remaining_percent)||(w.span_seconds!==null&&!positive(w.span_seconds))||(w.reset_at!==null&&!time(w.reset_at)))return false;
 return s.resources.every(r=>r&&[r.kind,r.unit,r.display].every(text));
}
function validateReply(body,routes){
 if(!body||!positive(body.configuration_revision)||!Array.isArray(body.routes)||body.routes.length!==routes.length||routes.length>256)throw Error("quota_reply_invalid");
 const expected=new Map(routes.map(r=>[refKey(r),r])),out=new Map();
 for(const v of body.routes){
  const route=expected.get(refKey(v?.route||{}));
  if(!route||out.has(refKey(v.route))||!Object.hasOwn(statuses,v.status))throw Error("quota_reply_invalid");
  if(v.identity!==undefined&&!validIdentity(v.identity,route))throw Error("quota_reply_invalid");
  if(v.snapshot!==undefined&&(!v.identity||!validSnapshot(v.snapshot,v.identity,v.status)))throw Error("quota_reply_invalid");
  if(v.error!==undefined&&v.error?.code!=="quota_query_failed")throw Error("quota_reply_invalid");
  // Unknown response fields, raw payloads and upstream messages are never rendered.
  out.set(refKey(v.route),structuredClone({route:v.route,status:v.status,identity:v.identity,snapshot:v.snapshot,failed:!!v.error}));
 }
 return out;
}
function displayState(view){
 if(!view)return "额度尚未读取";
 const s=view.snapshot;
 if(s&&(view.status==="available"||view.status==="zero")){
  const observed=Date.parse(s.observed_at),received=Date.parse(s.received_at),now=Date.now();
  if(!Number.isFinite(observed)||observed>received||observed>now)return statuses.unknown;
  if(now-observed>60000||s.windows.some(w=>w.reset_at!==null&&Date.parse(w.reset_at)<=now))return statuses.stale;
  if(!s.complete||!s.pool.verified)return statuses.unverified;
  const windows=s.windows.filter(w=>w.kind==="subscription"||w.kind==="coding_plan");
  if(!windows.length||windows.some(w=>w.unit!=="percent"||w.used_percent===null))return statuses.unknown;
  if(windows.some(w=>w.used_percent===100))return statuses.zero;
 }
 return statuses[view.status];
}
function el(tag,value,cls){const n=document.createElement(tag);if(value!==null)n.textContent=value;if(cls)n.className=cls;return n}
const dateLabel=v=>v===null?"未知":new Date(v).toLocaleString("zh-CN",{hour12:false});
function errorMessage(e,refresh){
 if(e?.status===401||e?.status===403)return "管理授权已失效，请重新连接本机服务。";
 if(e?.status===409)return "额度来源已变更，请重新载入项目。";
 if(e?.status===429)return "额度查询繁忙；请稍后手动读取缓存。";
 return refresh?"额度查询结果未确认。请手动读取缓存核对；不会自动重试查询。":"额度缓存读取失败，请手动重新读取。";
}
export function createQuotaView({request,onChange}){
 let project="",routes=[],epoch=0,serial=0,busy=false,blocked=true,views=null,historical=false,notice="额度尚未读取。",error=false,timer;
 const changed=()=>onChange();
 const stopTimer=()=>{clearTimeout(timer);timer=undefined};
 function armExpiry(){
  stopTimer();let next=Infinity;
  for(const v of views?.values()||[])if(v.snapshot&&(v.status==="available"||v.status==="zero")){
   const s=v.snapshot,ends=[Date.parse(s.observed_at)+60001,...s.windows.filter(w=>w.reset_at!==null).map(w=>Date.parse(w.reset_at))];
   for(const at of ends)if(at>Date.now())next=Math.min(next,at);
  }
  if(Number.isFinite(next)){const generation=epoch;timer=setTimeout(()=>{if(generation===epoch)changed()},Math.min(next-Date.now(),2147483647))}
 }
 function clear(){epoch++;serial++;stopTimer();project="";routes=[];views=null;busy=false;historical=false;notice="额度尚未读取。";error=false}
 async function read(route){
  if(!project||busy||blocked&&route)return;
  const generation=epoch,requestID=++serial,scope=project,catalogue=routes;
  busy=true;error=false;notice=route?"正在手动查询额度…":"正在读取额度缓存…";changed();
  try{
   const reply=await request("/control/v1/projects/"+encodeURIComponent(scope)+"/quota"+(route?"/"+encodeURIComponent(route.id)+"/refresh":""),route?{method:"POST",body:"{}"}:{});
   if(generation!==epoch||requestID!==serial)return;
   const next=validateReply(reply.body,catalogue);views=next;historical=false;
   error=[...next.values()].some(v=>v.failed);
   notice=error?"供应商查询失败；展示缓存观察，请手动核对。":route?"已完成手动额度查询。观察不代表生成准入。":"已读取额度缓存；未自动查询供应商。";
   armExpiry();
  }catch(e){if(generation!==epoch||requestID!==serial)return;historical=!!views;error=true;notice=errorMessage(e,!!route)}
  finally{if(generation===epoch&&requestID===serial){busy=false;changed()}}
 }
 function setProject(id,catalogue){clear();project=id;routes=structuredClone(catalogue);void read()}
 function summary(route){const v=views?.get(refKey(route));if(!v)return "额度尚未读取。";return (historical?"历史额度（本次未确认）：":"额度：")+displayState(v)+"。"}
 function render(isBlocked){
  blocked=isBlocked;const list=$("quota-list");list.replaceChildren();
  $("read-quota").disabled=blocked||busy||!project;$("read-quota").onclick=()=>{if(!blocked)void read()};
  for(const route of routes){
   const v=views?.get(refKey(route)),s=v?.snapshot,row=el("div",null,"row"),who=el("div",null,"who");
   who.append(el("div",route.model+" · "+route.id+"@"+route.revision,"name"),el("div","账号："+route.account,"sub"),el("p",summary(route),"details"));
   if(s){
    who.append(el("p","来源："+s.source+"；观察时间："+dateLabel(s.observed_at)+"；接收时间："+dateLabel(s.received_at),"details"));
    for(const w of s.windows){
     const value=w.unit==="percent"?"已用 "+(w.used_percent===null?"未知":w.used_percent+"%")+"；剩余 "+(w.remaining_percent===null?"未知":w.remaining_percent+"%"):"单位 "+w.unit+"，百分比不可比较";
     who.append(el("p",w.name+" · "+w.kind+"："+value+"；重置："+dateLabel(w.reset_at),"details"));
    }
    for(const r of s.resources)who.append(el("p","独立资源 "+r.kind+"（"+r.unit+"）："+r.display+"；不计入订阅百分比。","details"));
    const pool=s.pool;
    who.append(el("p",pool.verified?"已核验共享池："+pool.id+"（"+pool.scope+"）；相同池仅为别名，不累加额度。":"物理池未核验；不合并账号额度。","details"));
    if(v.failed)who.append(el("p","本次供应商查询失败；以上为缓存观察。","details"));
   }
   const refresh=el("button","刷新额度 "+route.id,"text action");refresh.type="button";refresh.setAttribute("aria-label","刷新额度 "+route.id);
   refresh.disabled=blocked||busy||!v?.identity||v.status==="unsupported";refresh.onclick=()=>{if(!refresh.disabled)void read(route)};
   row.append(who,refresh);list.append(row);
  }
  $("quota-status").textContent=notice;$("quota-status").className=error?"details error":"details";
 }
 return {clear,setProject,summary,render};
}
