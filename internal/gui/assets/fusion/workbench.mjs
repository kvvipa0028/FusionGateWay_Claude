import {labels,roles} from './model.mjs';
const $=id=>document.getElementById(id);
const integer=(n,min=0)=>Number.isSafeInteger(n)&&n>=min;
const opaque=s=>typeof s==='string'&&s.length>0&&new TextEncoder().encode(s).length<=256&&!/[\s\p{Cc}/\\]/u.test(s);
const bad=()=>Object.assign(Error('response_unavailable'),{status:200,code:'response_unavailable'});
function node(tag,text,cls){const e=document.createElement(tag);if(text!==null)e.textContent=text;if(cls)e.className=cls;return e}
function taskValue(task,project,etag){
 if(!opaque(task?.id)||task.project_id!==project||typeof task.goal!=='string'||!opaque(task.state)||!integer(task.plan_revision,1)||!integer(task.generation))throw bad();
 const tag='"p'+task.plan_revision+'-g'+task.generation+'-'+task.state+'"';if(etag!==undefined&&etag!==tag)throw bad();
 return task;
}
function planValue(plan,revision){
 if(plan?.schema_version!==1||!integer(plan.revision,1)||(revision!==undefined&&plan.revision!==revision)||!/^[0-9a-f]{64}$/.test(plan.hash)||!Array.isArray(plan.required_roles)||!plan.required_roles.length||new Set(plan.required_roles).size!==plan.required_roles.length||!plan.bindings||Array.isArray(plan.bindings)||Object.keys(plan.bindings).length!==plan.required_roles.length)throw bad();
 for(const role of plan.required_roles){
  const b=plan.bindings[role];if(!roles.includes(role)||!['locked','auto'].includes(b?.mode))throw bad();
  const targets=b.mode==='locked'?[b.target]:b.candidates;
  if(!Array.isArray(targets)||!targets.length||targets.some(t=>!t||!opaque(t.route?.id)||!integer(t.route.revision,1)||typeof t.resolved_model!=='string'||!t.resolved_model||typeof t.requested_model!=='string'||!t.requested_model||typeof t.account!=='string'||typeof t.billing_path!=='string'||!['none','default','explicit'].includes(t.effort?.requested_mode)||(t.effort.requested_mode==='none'?t.effort.value!==null:typeof t.effort.value!=='string'||!t.effort.value)||!['controlled_calls','primary_only'].includes(t.lock_enforcement)))throw bad();
 }
 return plan;
}
function budgetValue(b){if(!integer(b?.max_calls,1)||b.max_calls>1000||!integer(b.max_reworks)||b.max_reworks>1||!integer(b.used_calls)||b.used_calls>b.max_calls||!integer(b.used_reworks)||b.used_reworks>b.max_reworks)throw bad();return b}
const stateName=s=>({ready:'待执行',running:'运行中',paused:'已暂停',pausing:'暂停中',cancelling:'取消中',cancelled:'已取消',needs_review:'需要核对',succeeded:'成功',failed:'失败'})[s]||s;
function readFailure(e){return e.status===401||e.status===403?'管理授权已失效，请重新连接。':e.status===503?'本机服务不可用或已撤销。':'读取失败，请重新读取。'}
export function createWorkbench({request,onLock,onNotice}){
 let project='',preview=null,attempt=null,expiry=null,epoch=0,listSerial=0,detailSerial=0,listBusy=false,detailBusy=false,next='',items=[],selected='',busy=false;
 function controls(){
  $('task-submit').hidden=!preview&&!attempt;
  $('submit-task').hidden=!!attempt?.unknown;$('submit-task').disabled=busy||!preview||Date.now()>=preview.expires;
  $('retry-task').hidden=!attempt?.unknown;$('retry-task').disabled=busy;
  $('refresh-tasks').disabled=!project||listBusy;$('older-tasks').hidden=!next;$('older-tasks').disabled=listBusy;
  $('reload-task').disabled=!selected||detailBusy;
 }
 function invalidate(){if(attempt)return;clearTimeout(expiry);expiry=null;preview=null;controls()}
 function renderList(){
  const root=$('task-list');root.replaceChildren();
  for(const task of items){
   const row=node('div',null,'row'),who=node('div',null,'who');
   who.append(node('p',task.goal+(task.goal_truncated?'…':''),'name'),node('p',task.id+' · '+stateName(task.state)+' · 计划 '+task.plan_revision,'sub'));
   const button=node('button','查看任务','text action');button.dataset.taskId=task.id;button.setAttribute('aria-label','查看任务 '+task.id);button.onclick=()=>loadTask(task.id);
   row.append(who,button);root.append(row);
  }
 }
 async function refreshList(older=false){
  if(!project||older&&!next)return;
  const id=project,version=epoch,serial=++listSerial,cursor=older?next:'';listBusy=true;
  if(!older){items=[];next='';renderList()}
  $('task-list-status').textContent='正在读取任务列表…';controls();
  try{
   const {body}=await request('/control/v1/projects/'+encodeURIComponent(id)+'/tasks'+(cursor?'/before/'+encodeURIComponent(cursor):''));
   if(version!==epoch||serial!==listSerial)return;
   if(!Array.isArray(body?.tasks)||body.tasks.length>32||typeof body.next_before!=='string'||(body.next_before&&body.tasks.at(-1)?.id!==body.next_before))throw bad();
   const seen=new Set(older?items.map(t=>t.id):[]);
   for(const t of body.tasks){taskValue(t,id,t.etag);if(typeof t.goal_truncated!=='boolean'||Array.from(t.goal).length>512||seen.has(t.id))throw bad();seen.add(t.id)}
   items=older?[...items,...body.tasks]:body.tasks;next=body.next_before;renderList();
   $('task-list-status').textContent=items.length?'已读取 '+items.length+' 个任务。':'暂无已保存任务。';
  }catch(e){if(version===epoch&&serial===listSerial)$('task-list-status').textContent=readFailure(e)}
  finally{if(version===epoch&&serial===listSerial){listBusy=false;controls()}}
 }
 async function loadTask(id){
  if(!project||!opaque(id))return;
  const scope=project,version=epoch,serial=++detailSerial;selected=id;detailBusy=true;$('task-detail').replaceChildren();$('task-detail-status').textContent='正在读取任务详情…';controls();
  try{
   let data;
   for(let retry=0;retry<2;retry++){
    const first=await request('/agent/v1/tasks/'+encodeURIComponent(id));taskValue(first.body,scope,first.etag);if(first.body.id!==id)throw bad();
    const [plan,budget]=await Promise.all([request('/control/v1/tasks/'+encodeURIComponent(id)+'/plan'),request('/control/v1/tasks/'+encodeURIComponent(id)+'/budget')]);
    const last=await request('/agent/v1/tasks/'+encodeURIComponent(id));taskValue(last.body,scope,last.etag);if(last.body.id!==id)throw bad();
    if(first.etag!==last.etag){if(retry===0)continue;throw bad()}
    planValue(plan.body,last.body.plan_revision);if(plan.etag!=='"'+plan.body.revision+'"')throw bad();budgetValue(budget.body);
    data={task:last.body,plan:plan.body,budget:budget.body};break;
   }
   if(version!==epoch||serial!==detailSerial)return;
   const root=$('task-detail'),{task,plan,budget}=data;
   root.append(node('p',task.id+' · '+stateName(task.state)+' ('+task.state+') · 计划 '+task.plan_revision+' · generation '+task.generation),node('p',task.goal,'task-goal'));
   for(const role of plan.required_roles){
    const b=plan.bindings[role];root.append(node('h3',labels[role]+(b.mode==='locked'?' · 指定并锁定':' · 批准候选内自动')));
    for(const target of b.mode==='locked'?[b.target]:b.candidates){
     root.append(node('p',target.resolved_model,'task-model'),node('p','请求模型：'+target.requested_model+'；路线：'+target.route.id+'@'+target.route.revision+'；effort：'+target.effort.requested_mode+(target.effort.value?' / '+target.effort.value:'')+'；账号：'+target.account+'；计费：'+target.billing_path+'；锁定：'+target.lock_enforcement,'details'));
    }
   }
   root.append(node('p','调用预算：'+budget.used_calls+' / '+budget.max_calls+'；返工预算：'+budget.used_reworks+' / '+budget.max_reworks+'。'),node('p','计划 hash：'+plan.hash,'details'));
   $('task-detail-status').textContent='已读取任务详情。预算为本次单独读取的服务端计数；保存任务不等于阶段执行成功。';
  }catch(e){if(version===epoch&&serial===detailSerial){$('task-detail').replaceChildren();$('task-detail-status').textContent=readFailure(e)}}
  finally{if(version===epoch&&serial===detailSerial){detailBusy=false;controls()}}
 }
 function setProject(id){
  if(attempt)return;
  project=id;epoch++;listSerial++;detailSerial++;items=[];next='';selected='';listBusy=false;detailBusy=false;invalidate();renderList();$('task-detail').replaceChildren();$('task-detail-status').textContent='选择任务读取完整目标、冻结计划和预算。';controls();if(project)refreshList();
 }
 function setPreview(value,context){
  if(attempt)throw bad();planValue(value?.plan,1);
  const expires=Date.parse(value.expires_at);
  if(!opaque(value.preview_id)||!integer(value.configuration_revision,1)||!Number.isFinite(expires)||context.project_id!==project||value.plan.required_roles.length!==1||value.plan.required_roles[0]!==context.required_roles[0])throw bad();
  if(!integer(value.budget?.max_calls,1)||value.budget.max_calls>1000||!integer(value.budget.max_reworks)||value.budget.max_reworks>1)throw bad();
  invalidate();preview={id:value.preview_id,hash:value.plan.hash,revision:value.plan.revision,expires,project:context.project_id,goal:context.goal};
  $('preview').append(node('p','计划 '+value.plan.revision+' · hash '+value.plan.hash,'details'),node('p','调用上限：'+value.budget.max_calls+'；返工上限：'+value.budget.max_reworks+'；预览有效至：'+new Date(expires).toLocaleString(),'details'));
  for(const role of value.plan.required_roles){const b=value.plan.bindings[role];for(const t of b.mode==='locked'?[b.target]:b.candidates)$('preview').append(node('p',labels[role]+' · '+t.route.id+'@'+t.route.revision+' · effort：'+t.effort.requested_mode+(t.effort.value?' / '+t.effort.value:'')+' · 账号：'+t.account+' · 计费：'+t.billing_path+' · 锁定：'+t.lock_enforcement,'details'))}
  function expire(){if(attempt)return;controls();onNotice('预览已到期，请重新预览；未提交任务。',true)}
  if(expires<=Date.now())expire();else expiry=setTimeout(expire,Math.min(expires-Date.now(),2147483647));controls();return expires>Date.now();
 }
 async function submit(){
  if(busy)return;
  if(!attempt){
   if(!preview||Date.now()>=preview.expires){onNotice('预览已到期，请重新预览；未提交任务。',true);controls();return}
   try{const bytes=crypto.getRandomValues(new Uint8Array(16));attempt={...preview,key:'task_ui_'+[...bytes].map(b=>b.toString(16).padStart(2,'0')).join(''),body:JSON.stringify({preview_id:preview.id,plan_hash:preview.hash}),unknown:false}}
   catch{onNotice('无法生成任务提交标识，未发送提交。',true);return}
  }
  busy=true;onLock(true);controls();
  try{
   const saved=await request('/agent/v1/tasks',{method:'POST',headers:{'Idempotency-Key':attempt.key},body:attempt.body});
   taskValue(saved.body,attempt.project);if(saved.status!==201||saved.body.goal!==attempt.goal||saved.body.plan_revision<attempt.revision)throw bad();
   const task=saved.body;attempt=null;busy=false;invalidate();onLock(false);$('preview').hidden=true;
   onNotice('任务已保存：'+task.id+' · '+stateName(task.state)+' ('+task.state+')。本次提交未启动阶段。');refreshList();loadTask(task.id);
  }catch(e){
   const unknown=attempt.unknown||e.status===0||e.status>=500||(e.status>=200&&e.status<300);
   if(unknown){attempt.unknown=true;onNotice('提交结果未确认。本窗口保留原请求，请重试同一任务提交；可读取任务列表核对。',true)}
   else{attempt=null;preview=null;clearTimeout(expiry);onLock(false);$('preview').hidden=true;onNotice(e.status===409?'预览已失效或提交冲突，请重新预览。未获得任务保存回执。':e.status===401||e.status===403?'管理授权已失效，未获得任务保存回执。':'任务提交被拒绝，请核对后重新预览。',true)}
  }finally{busy=false;controls()}
 }
 $('submit-task').onclick=submit;$('retry-task').onclick=submit;$('refresh-tasks').onclick=()=>refreshList();$('older-tasks').onclick=()=>refreshList(true);$('reload-task').onclick=()=>loadTask(selected);
 window.addEventListener('beforeunload',event=>{if(attempt){event.preventDefault();event.returnValue=''}});
 controls();return {setProject,setPreview,invalidate};
}
