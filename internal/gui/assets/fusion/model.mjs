export const roles = ["design","implementation","testing","review","acceptance"];
export const labels = {design:"设计",implementation:"实施",testing:"测试",review:"审查",acceptance:"验收"};
export const groups = [
 {id:"design_planning",title:"设计与规划",roles:["design"]},
 {id:"implementation_testing",title:"实施与测试",roles:["implementation","testing"]},
 {id:"review_acceptance",title:"审查与验收",roles:["review","acceptance"]},
];
export const copy = value => JSON.parse(JSON.stringify(value));
export function expandLayer(layer = {}) {
 if (!layer || typeof layer !== "object" || Array.isArray(layer)) throw Error("layer_invalid");
 const out = {roles:Object.fromEntries(roles.map(role=>[role,{mode:"inherit"}]))};
 for (const [id,binding] of Object.entries(layer.groups || {})) {
  const group=groups.find(g=>g.id===id);if (!group || !binding) throw Error("layer_invalid");
  for (const role of group.roles) out.roles[role]=copy(binding);
 }
 for (const [role,binding] of Object.entries(layer.roles || {})) {
  if (!roles.includes(role) || !binding) throw Error("layer_invalid");
  out.roles[role]=copy(binding);
 }
 return out;
}
const stable = value => value && typeof value === "object" && !Array.isArray(value)
 ? Object.fromEntries(Object.keys(value).sort().map(k=>[k,stable(value[k])]))
 : Array.isArray(value) ? value.map(stable) : value;
export const sameBinding = (a,b) => JSON.stringify(stable(a))===JSON.stringify(stable(b));
export function groupShared(layer,id) {
 const group=groups.find(g=>g.id===id);if (!group) throw Error("group_invalid");
 return group.roles.every(role=>sameBinding(layer.roles[role],layer.roles[group.roles[0]]));
}
export function shareGroup(layer,id) {
 const out=expandLayer(layer),group=groups.find(g=>g.id===id);
 if (!group) throw Error("group_invalid");
 for (const role of group.roles) out.roles[role]=copy(out.roles[group.roles[0]]);
 return out;
}
export function targetFor(route) {
 if (!route || !Number.isSafeInteger(route.revision) || route.revision < 1 || !route.id || !route.model) throw Error("route_unavailable");
 return {route:{id:route.id,revision:route.revision},model:route.model,effort:null};
}
export const routeKey = route => JSON.stringify([route.id,route.revision,route.model]);
// Metadata is copied from the exact project response, never used as an execution
// grant. Opaque credential identities and unknown fields stay out of the view.
export function configurationRoutes(reply,project) {
 const body=reply?.body;
 if(body?.project_id!==project||!Number.isSafeInteger(body.revision)||body.revision<1||reply.etag!=='"'+body.revision+'"'||!body.configuration||typeof body.configuration!=="object"||Array.isArray(body.configuration)||!Object.hasOwn(body.configuration,"routes")||![body.configuration.global,body.configuration.project].every(v=>v&&typeof v==="object"&&!Array.isArray(v)))throw Error("configuration_invalid");
 const rows=body.configuration.routes??[];
 if(!Array.isArray(rows)||rows.length>256)throw Error("configuration_invalid");
 const text=(v,empty=false)=>typeof v==="string"&&(empty||v.length>0)&&v.length<=4096&&!/[\u0000-\u001f\u007f]/.test(v);
 const refs=new Set();
 return rows.map(r=>{
  if(!r||![r.id,r.model,r.account,r.workspace,r.runtime_version].every(v=>text(v))||!text(r.billing_path,true)||!Number.isSafeInteger(r.revision)||r.revision<1||![r.admitted,r.billing_known,r.no_effort].every(v=>typeof v==="boolean")||!["controlled_calls","primary_only","unverified"].includes(r.lock_enforcement)||!(r.plugin_version===null||text(r.plugin_version))||!(r.default_effort===null||text(r.default_effort)))throw Error("configuration_invalid");
  const ref=JSON.stringify([r.id,r.revision]);if(refs.has(ref))throw Error("configuration_invalid");refs.add(ref);
  const list=value=>{const v=value??[];if(!Array.isArray(v)||v.length>128||!v.every(x=>text(x))||new Set(v).size!==v.length)throw Error("configuration_invalid");return [...v]};
  const efforts=list(r.efforts),capabilities=list(r.capabilities);
  if(r.default_effort!==null&&!efforts.includes(r.default_effort))throw Error("configuration_invalid");
  return {id:r.id,revision:r.revision,model:r.model,account:r.account,workspace:r.workspace,runtime_version:r.runtime_version,plugin_version:r.plugin_version,billing_path:r.billing_path,billing_known:r.billing_known,admitted:r.admitted,no_effort:r.no_effort,default_effort:r.default_effort,lock_enforcement:r.lock_enforcement,efforts,capabilities};
 });
}
export function exactRoute(target,routes) {
 const found=routes.filter(r=>target?.route?.id===r.id && target.route.revision===r.revision && target.model===r.model && Number.isSafeInteger(r.revision));
 return found.length===1 ? found[0] : null;
}
function targetIssue(target,routes) {
 const route=exactRoute(target,routes);if (!route) return "route_unavailable";
 if (!target.effort) return "effort_unspecified";
 const e=target.effort;
 if (e.mode==="none") return route.no_effort===true ? null : "effort_unsupported";
 if (e.mode==="default") return route.default_effort && (route.efforts||[]).includes(route.default_effort) ? null : "effort_unsupported";
 if (e.mode==="explicit") return e.value && (route.efforts||[]).includes(e.value) ? null : "effort_unsupported";
 return "effort_unsupported";
}
export function layerIssues(layer,routes) {
 const out=[];
 for (const role of roles) {
  const b=layer.roles?.[role]||{mode:"inherit"};let code=null;
  if (b.mode==="locked") code=targetIssue(b,routes);
  else if (b.mode==="auto") {
   if (!Array.isArray(b.candidates) || !b.candidates.length) code="candidates_missing";
   else {
    const keys=b.candidates.map(c=>JSON.stringify(c.route));
    code=new Set(keys).size===keys.length ? b.candidates.map(c=>targetIssue(c,routes)).find(Boolean) : "candidate_duplicate";
   }
  } else if (b.mode!=="inherit") code="mode_invalid";
  if (b.accept_primary_only===true) code="lock_exception_unaccepted";
  if (code) out.push({role,code});
 }
 return out;
}
