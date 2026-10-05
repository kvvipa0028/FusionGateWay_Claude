import {labels,roles} from './model.mjs';
export const workflowRoles=Object.freeze({investigate:['design'],review:['review'],change:roles,bugfix:roles});
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
function previewValue(value){
 planValue(value?.plan,1);const expires=Date.parse(value.expires_at);
 if(!opaque(value.preview_id)||!integer(value.configuration_revision,1)||!Number.isFinite(expires)||!integer(value.budget?.max_calls,1)||value.budget.max_calls>1000||!integer(value.budget.max_reworks)||value.budget.max_reworks>1)throw bad();
 return {id:value.preview_id,hash:value.plan.hash,revision:value.plan.revision,expires,value:JSON.parse(JSON.stringify(value))};
}
function canonical(value){if(Array.isArray(value))return value.map(canonical);if(value&&typeof value==='object')return Object.fromEntries(Object.keys(value).sort().map(k=>[k,canonical(value[k])]));return value}
function samePreview(a,b){const value=p=>({...p,expires_at:Date.parse(p.expires_at)});return JSON.stringify(canonical(value(a)))===JSON.stringify(canonical(value(b)))}
function journalValue(body,scope,original){
 if(!body||Object.keys(body).length!==1||!Object.hasOwn(body,'submission'))throw bad();
 const v=body.submission;if(v===null)return null;
 if(!v||Array.isArray(v)||Object.keys(v).length!==6||v.project_id!==scope||typeof v.goal!=='string'||!v.goal||new TextEncoder().encode(v.goal).length>65536||!opaque(v.key)||!['prepared','committed','acknowledged','abandoned'].includes(v.state)||(['prepared','abandoned'].includes(v.state)?v.task_id!=='':!opaque(v.task_id)))throw bad();
 previewValue(v.preview);
 if(original&&(v.key!==original.key||v.goal!==original.goal||!samePreview(v.preview,original.value)))throw bad();
 return v;
}
const stateName=s=>({ready:'待执行',running:'运行中',paused:'已暂停',pausing:'暂停中',cancelling:'取消中',cancelled:'已取消',needs_review:'需要核对',succeeded:'成功',failed:'失败',advisory_only:'等待人工验收',completed:'已完成'})[s]||s;
function startJournalValue(body,scope,original){
 if(!body||Array.isArray(body)||Object.keys(body).length!==1||!Object.hasOwn(body,'request'))throw bad();
 const v=body.request;if(v===null)return null;
 if(!v||Array.isArray(v)||Object.keys(v).length!==7||!opaque(v.key)||!roles.includes(v.role)||!['prepared','committed','acknowledged','abandoned'].includes(v.state)||(['prepared','abandoned'].includes(v.state)?v.run_id!=='':!opaque(v.run_id)))throw bad();
 taskValue(v.task,scope,v.etag);planValue(v.plan,v.task.plan_revision);
 if(v.task.state!=='ready'||!v.task.goal||new TextEncoder().encode(v.task.goal).length>65536||!v.plan.required_roles.includes(v.role))throw bad();
 if(original&&(v.key!==original.key||v.role!==original.role||v.etag!==original.etag||JSON.stringify(canonical(v.task))!==JSON.stringify(canonical(original.task))||JSON.stringify(canonical(v.plan))!==JSON.stringify(canonical(original.plan))))throw bad();
 const run=original?.runId||original?.journal?.run_id;if(run&&v.run_id!==run)throw bad();
 return v;
}
const hash=s=>typeof s==='string'&&/^[0-9a-f]{64}$/.test(s);
const fields=(v,names)=>v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===names.length&&names.every(n=>Object.hasOwn(v,n));
const same=(a,b)=>JSON.stringify(canonical(a))===JSON.stringify(canonical(b));
function workflowValue(reply,data,scope){
 const body=reply?.body;if(reply.status!==200||!fields(body,['task','workflow']))throw bad();
 taskValue(body.task,scope,reply.etag);if(!same(body.task,data.task))throw bad();
 const v=body.workflow;if(v===null)return null;
 const d=v?.definition;
 if(!fields(v,['task_id','definition','design','approval','next','blocker'])||v.task_id!==data.task.id||!fields(d,['schema_version','kind','required_roles','hash'])||d.schema_version!==1||!hash(d.hash)||!workflowRoles[d.kind]||!same(d.required_roles,workflowRoles[d.kind])||!same([...d.required_roles].sort(),[...data.plan.required_roles].sort())||!(v.next===''||d.required_roles.includes(v.next))||!['','stage_active','stop_unverified','stage_unverified','stage_failed','design_required','approval_required','workflow_complete','task_not_ready'].includes(v.blocker))throw bad();
 if(v.design!==null){
  const design=v.design,snapshot=design?.snapshot,doc=snapshot?.document;
  if(!fields(design,['run_id','plan_revision','plan_hash','generation','snapshot'])||!opaque(design.run_id)||!integer(design.plan_revision,1)||design.plan_revision>data.task.plan_revision||!hash(design.plan_hash)||!integer(design.generation,1)||design.generation>data.task.generation||!fields(snapshot,['schema_version','document','hash','acceptance_hash'])||snapshot.schema_version!==1||!hash(snapshot.hash)||!hash(snapshot.acceptance_hash)||!fields(doc,['goal','scope','constraints','interfaces','acceptance'])||doc.goal!==data.task.goal||new TextEncoder().encode(JSON.stringify(doc)).length>65536||['scope','constraints','interfaces','acceptance'].some(n=>!Array.isArray(doc[n])||!doc[n].length||doc[n].length>128||doc[n].some(x=>typeof x!=='string'||!x.trim()||x.includes('\0'))))throw bad();
 }
 if(v.approval!==null){
  const a=v.approval;if(!v.design||!fields(a,['plan_revision','plan_hash','generation','design_hash','acceptance_hash'])||a.plan_revision!==data.plan.revision||a.plan_hash!==data.plan.hash||!integer(a.generation,1)||a.generation>data.task.generation||a.design_hash!==v.design.snapshot.hash||a.acceptance_hash!==v.design.snapshot.acceptance_hash)throw bad();
 }
 if(v.blocker==='approval_required'&&(!v.design||v.approval)||v.blocker==='design_required'&&v.design)throw bad();
 return v;
}
const reasonValue=s=>typeof s==='string'&&s.trim()&&new TextEncoder().encode(s).length<=8192&&!s.includes('\0');
const reportKeys=['run_id','text_hash','tree_hash','spec_hash','design_hash','acceptance_hash'];
function humanValue(reply,data,scope,sent=null){
 const b=reply?.body;if(reply.status!==200||!fields(b,['task','report','decision','unavailable']))throw bad();
 taskValue(b.task,scope,reply.etag);
 if(sent){if(b.task.id!==data.task.id||b.task.goal!==data.task.goal||b.task.plan_revision!==data.task.plan_revision||b.task.generation!==data.task.generation||b.task.state!==(sent.action==='accept'?'completed':'needs_review'))throw bad()}
 else if(!same(b.task,data.task))throw bad();
 if(!['','acceptance_pending','verification_reader_unavailable'].includes(b.unavailable)||(b.report===null)!==!!b.unavailable)throw bad();
 const r=b.report,v=data.workflow;
 if(r){
  if(!fields(r,[...reportKeys,'model','model_valid','hard'])||!opaque(r.run_id)||reportKeys.slice(1).some(k=>!hash(r[k]))||typeof r.model_valid!=='boolean'||v?.blocker!=='workflow_complete'||!v.approval||r.design_hash!==v.design.snapshot.hash||r.acceptance_hash!==v.design.snapshot.acceptance_hash||!fields(r.hard,['status','reason','tests','skipped'])||!['passed','failed','unverified','superseded'].includes(r.hard.status)||typeof r.hard.reason!=='string'||!integer(r.hard.tests)||!integer(r.hard.skipped)||r.hard.skipped>r.hard.tests||r.hard.status==='passed'&&r.hard.tests===0)throw bad();
  const m=r.model,criteria=v.design.snapshot.document.acceptance;
  if(!fields(m,['version','verdict','criteria'])||m.version!==1||!['accepted','rejected','unverified'].includes(m.verdict)||!Array.isArray(m.criteria)||new TextEncoder().encode(JSON.stringify(m)).length>65536)throw bad();
  if(r.model_valid){
   if(m.criteria.length!==criteria.length||new Set(m.criteria.map(c=>c?.index)).size!==criteria.length||m.criteria.some(c=>!fields(c,['index','status','reason'])||!integer(c.index)||c.index>=criteria.length||!['met','not_met','unverified'].includes(c.status)||!reasonValue(c.reason)))throw bad();
   const no=m.criteria.some(c=>c.status==='not_met'),unknown=m.criteria.some(c=>c.status==='unverified');
   if(m.verdict!==(no?'rejected':unknown?'unverified':'accepted'))throw bad();
  }else if(m.verdict!=='unverified'||m.criteria.length)throw bad();
 }
 const d=b.decision;
 if(d){
  if(!fields(d,['version','task_id','plan_revision','generation',...reportKeys,'action','reason','authority','at'])||d.version!==1||d.task_id!==data.task.id||!integer(d.plan_revision,1)||d.plan_revision>data.task.plan_revision||!integer(d.generation,1)||d.generation>data.task.generation||!opaque(d.run_id)||reportKeys.slice(1).some(k=>!hash(d[k]))||!['accept','return'].includes(d.action)||!reasonValue(d.reason)||d.authority!=='management'||typeof d.at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(d.at)||!Number.isFinite(Date.parse(d.at)))throw bad();
  if(d.plan_revision===data.task.plan_revision&&d.generation===data.task.generation&&(!r||reportKeys.some(k=>d[k]!==r[k])||b.task.state!==(d.action==='accept'?'completed':'needs_review')))throw bad();
 }
 if(sent&&(!r||reportKeys.some(k=>sent[k]!==r[k])||!d||d.action!==sent.action||d.reason!==sent.reason||d.plan_revision!==data.task.plan_revision||d.generation!==data.task.generation))throw bad();
 return b;
}
function readFailure(e){return e.status===401||e.status===403?'管理授权已失效，请重新连接。':e.status===503?'本机服务不可用或已撤销。':'读取失败，请重新读取。'}
export function createWorkbench({request,onLock,onNotice,onRestore}){
 let project='',preview=null,attempt=null,expiry=null,epoch=0,listSerial=0,detailSerial=0,listBusy=false,detailBusy=false,next='',items=[],selected='',busy=false,recoveryBlocked=false,taskData=null,controlAttempt=null,controlBusy=false,knownRun=null,workflowUncertain=false,humanData=null,humanBusy=false,humanSerial=0,humanUncertain=false;
 let eventTask='',eventSerial=0,eventCursor=0,eventRows=[],eventBusy=false,startBlocked=false,startSerial=0;
 function clearEvents(id=''){
  eventTask=id;eventSerial++;eventCursor=0;eventRows=[];eventBusy=false;$('event-history').replaceChildren();$('task-events').open=false;$('events-status').textContent=id?'尚未读取任务事件。':'选择任务后读取事件。';
 }
 function renderEvents(){
  const root=$('event-history');root.replaceChildren();
  for(const v of eventRows)root.append(node('p','#'+v.seq+' · '+v.kind+' · generation '+v.generation+(v.run_id?' · run '+v.run_id:''),'details'));
 }
 async function readEvents(reset=false){
  if(eventBusy||detailBusy||!taskData||selected!==taskData.task.id||!eventTask)return;
  const id=eventTask,version=epoch,serial=++eventSerial,after=reset?0:eventCursor;
  eventBusy=true;$('events-status').textContent='正在读取任务事件…';controls();
  try{
   const result=await request('/agent/v1/tasks/'+encodeURIComponent(id)+'/events/page/'+after),v=result.body;
   if(version!==epoch||serial!==eventSerial||id!==eventTask)return;
   if(result.status!==200||v?.task_id!==id||v.after!==after||!integer(v.next_after)||typeof v.has_more!=='boolean'||!Array.isArray(v.events)||v.events.length>32||v.has_more&&v.events.length!==32)throw bad();
   const rows=v.events.map((e,n)=>{
    if(e?.task_id!==id||e.seq!==after+n+1||!integer(e.seq,1)||!integer(e.generation)||typeof e.kind!=='string'||!(/^[a-z0-9_]{1,64}$/).test(e.kind)||!(e.run_id===''||opaque(e.run_id)))throw bad();
    return {seq:e.seq,kind:e.kind,generation:e.generation,run_id:e.run_id};
   });
   if(v.next_after!==(rows.length?rows.at(-1).seq:after))throw bad();
   const combined=reset?rows:[...eventRows,...rows];eventRows=combined.slice(-128);eventCursor=v.next_after;renderEvents();
   $('events-status').textContent=(rows.length?'已读取任务事件至 #'+eventCursor+'。':'暂无新事件，已核对至 #'+eventCursor+'。')+(v.has_more?'仍有更晚事件，请继续读取。':'')+(eventRows.length===128?'仅显示最近 128 条；可从头读取历史。':'');
  }catch(e){if(version===epoch&&serial===eventSerial&&id===eventTask)$('events-status').textContent=readFailure(e)+'原事件和游标已保留，不自动重试。'}
  finally{if(version===epoch&&serial===eventSerial&&id===eventTask){eventBusy=false;controls()}}
 }
 function lock(value){onLock(value||controlBusy||!!controlAttempt||startBlocked)}
 function executionNotice(message){$('execution-status').textContent=message}
 function controls(){
  $('task-submit').hidden=!preview&&!attempt;
  $('submit-task').hidden=!!attempt;$('submit-task').disabled=busy||controlBusy||!!controlAttempt||recoveryBlocked||startBlocked||!preview||Date.now()>=preview.expires;
  $('retry-task').hidden=!attempt||attempt.action==='abandon';$('retry-task').disabled=busy;
  $('retry-task').textContent=attempt?.task?'重试任务保存确认':attempt&&!attempt.journal?'重试保存原请求':'重试同一任务提交';
  $('submission-recovery').hidden=!attempt&&!recoveryBlocked;
  $('refresh-submission').disabled=busy||controlBusy||!project;
  $('abandon-submission').hidden=!attempt||!!attempt.task||['committed','acknowledged'].includes(attempt.journal?.state);
  $('abandon-submission').disabled=busy||controlBusy;$('abandon-submission').textContent=attempt?.action==='abandon'?'重试放弃原请求':'放弃未提交请求';
  $('refresh-tasks').disabled=!project||listBusy;$('older-tasks').hidden=!next;$('older-tasks').disabled=listBusy;
  $('reload-task').disabled=!selected||detailBusy||controlBusy;
  $('task-events').hidden=!selected;$('read-events').disabled=eventBusy||detailBusy||!taskData||busy||controlBusy;$('reset-events').disabled=$('read-events').disabled;
  const unavailable=busy||!!attempt||recoveryBlocked||startBlocked||controlBusy||!!controlAttempt||detailBusy||!taskData;
  $('execution-controls').hidden=!selected;
  $('execution-role').disabled=unavailable;
  $('start-role').disabled=workflowUncertain||!!taskData&&taskData.workflow===null&&taskData.plan.required_roles.length>1||!!taskData?.workflow&&(!!taskData.workflow.blocker||$('execution-role').value!==taskData.workflow.next)||unavailable||taskData?.task.state!=='ready'||taskData.budget.used_calls>=taskData.budget.max_calls;
  workflowControls(unavailable||humanBusy);humanControls(unavailable);
  $('pause-task').disabled=unavailable||!['ready','running','paused','pausing'].includes(taskData?.task.state);
  $('continue-task').disabled=unavailable||taskData?.task.state!=='paused';
  $('cancel-task').disabled=unavailable||!['ready','running','paused','pausing','cancelling','failed','advisory_only'].includes(taskData?.task.state);
  $('retry-execution').hidden=!controlAttempt;$('retry-execution').disabled=controlBusy||busy||detailBusy;
  const original=controlAttempt?.action==='start'?controlAttempt:null;
  $('retry-execution').textContent=original?.phase==='abandon'?'重试封存原启动请求':original?.phase==='acknowledge'?'重试启动记录确认':original&&!original.journal?'重试保存原启动请求':'重试原运行请求';
  $('start-recovery').hidden=!original&&!startBlocked;$('refresh-start').disabled=busy||controlBusy||!project;
  $('abandon-start').hidden=!original||!!original.runId||!!original.journal?.run_id;
  $('abandon-start').disabled=busy||controlBusy||detailBusy;
 }
 function invalidate(){if(attempt||recoveryBlocked)return;clearTimeout(expiry);expiry=null;preview=null;controls()}
 function renderList(){
  const root=$('task-list');root.replaceChildren();
  for(const task of items){
   const row=node('div',null,'row'),who=node('div',null,'who');
   who.append(node('p',task.goal+(task.goal_truncated?'…':''),'name'),node('p',task.id+' · '+stateName(task.state)+' · 计划 '+task.plan_revision,'sub'));
   const button=node('button','查看任务','text action');button.disabled=controlBusy||!!controlAttempt;button.dataset.taskId=task.id;button.setAttribute('aria-label','查看任务 '+task.id);button.onclick=()=>loadTask(task.id);
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
 async function loadTask(id,internal=false){
  if(!project||!opaque(id)||controlBusy&&!internal||controlAttempt&&controlAttempt.task.id!==id)return;
  if(eventTask!==id)clearEvents(id);
  const scope=project,version=epoch,serial=++detailSerial;if(selected!==id){knownRun=null;$('run-record').replaceChildren();executionNotice('')}selected=id;taskData=null;detailBusy=true;clearWorkflow(id);$('task-detail').replaceChildren();$('task-detail-status').textContent='正在读取任务详情…';controls();
  try{
   let data;
   for(let retry=0;retry<2;retry++){
    const first=await request('/agent/v1/tasks/'+encodeURIComponent(id));taskValue(first.body,scope,first.etag);if(first.body.id!==id)throw bad();
    const [plan,budget]=await Promise.all([request('/control/v1/tasks/'+encodeURIComponent(id)+'/plan'),request('/control/v1/tasks/'+encodeURIComponent(id)+'/budget')]);
    const last=await request('/agent/v1/tasks/'+encodeURIComponent(id));taskValue(last.body,scope,last.etag);if(last.body.id!==id)throw bad();
    if(first.etag!==last.etag){if(retry===0)continue;throw bad()}
    planValue(plan.body,last.body.plan_revision);if(plan.etag!=='"'+plan.body.revision+'"')throw bad();budgetValue(budget.body);
    data={task:last.body,plan:plan.body,budget:budget.body};const flow=await request('/control/v1/tasks/'+encodeURIComponent(id)+'/workflow');data.workflow=workflowValue(flow,data,scope);break;
   }
   if(version!==epoch||serial!==detailSerial)return;
   workflowUncertain=false;taskData={...data,etag:'"p'+data.task.plan_revision+'-g'+data.task.generation+'-'+data.task.state+'"'};
   const choices=$('execution-role'),picked=choices.value;choices.replaceChildren();for(const role of data.plan.required_roles){const option=node('option',labels[role]);option.value=role;option.disabled=!!data.workflow&&role!==data.workflow.next;choices.append(option)}if(data.workflow?.next)choices.value=data.workflow.next;else if(data.plan.required_roles.includes(picked))choices.value=picked;
   const root=$('task-detail'),{task,plan,budget}=data;
   root.append(node('p',task.id+' · '+stateName(task.state)+' ('+task.state+') · 计划 '+task.plan_revision+' · generation '+task.generation),node('p',task.goal,'task-goal'));
   for(const role of plan.required_roles){
    const b=plan.bindings[role];root.append(node('h3',labels[role]+(b.mode==='locked'?' · 指定并锁定':' · 批准候选内自动')));
    for(const target of b.mode==='locked'?[b.target]:b.candidates){
     root.append(node('p',target.resolved_model,'task-model'),node('p','请求模型：'+target.requested_model+'；路线：'+target.route.id+'@'+target.route.revision+'；effort：'+target.effort.requested_mode+(target.effort.value?' / '+target.effort.value:'')+'；账号：'+target.account+'；计费：'+target.billing_path+'；锁定：'+target.lock_enforcement,'details'));
    }
   }
   root.append(node('p','调用预算：'+budget.used_calls+' / '+budget.max_calls+'；返工预算：'+budget.used_reworks+' / '+budget.max_reworks+'。'),node('p','计划 hash：'+plan.hash,'details'));
   renderWorkflow();$('task-detail-status').textContent='已读取任务详情。预算为本次单独读取的服务端计数；保存任务不等于阶段执行成功。';
   if(controlAttempt?.action!=='start'&&controlAttempt&&taskData.etag!==controlAttempt.etag){controlAttempt=null;lock(!!attempt||recoveryBlocked);executionNotice('已读取当前任务条件；原控制条件已失效。请核对实际状态，不自动重试。');renderList()}
   if(knownRun){try{const receipt=await request('/agent/v1/tasks/'+encodeURIComponent(id)+'/runs/'+encodeURIComponent(knownRun.run.id));if(version!==epoch||serial!==detailSerial)return;knownRun.run=runValue(receipt,knownRun.origin,false,true);renderRun();if(knownRun.run.role==='design'&&knownRun.run.state==='succeeded'&&!taskData.workflow?.design)$('design-run').value=knownRun.run.id}catch(e){if(version!==epoch||serial!==detailSerial)return;$('run-record').replaceChildren();executionNotice('任务条件已读取；阶段执行回读失败或版本已变化，请重新读取核对。')}}

  }catch(e){if(version===epoch&&serial===detailSerial){taskData=null;$('task-detail').replaceChildren();$('run-record').replaceChildren();$('task-detail-status').textContent=readFailure(e)}}
  finally{if(version===epoch&&serial===detailSerial){detailBusy=false;controls()}}
 }
 let workflowTask='';
 function clearWorkflow(id=''){
  humanData=null;humanBusy=false;humanSerial++;humanUncertain=false;$('human-report').replaceChildren();$('human-status').textContent='尚未读取当前验收证据。';if(workflowTask!==id)$('human-reason').value='';
  if(workflowTask!==id){workflowTask=id;$('workflow-panel').open=false;for(const n of ['run','scope','constraints','interfaces','acceptance'])$('design-'+n).value='';$('workflow-kind').value=''}
  $('workflow-record').replaceChildren();$('workflow-status').textContent='';
 }
 function workflowControls(unavailable){
  const v=taskData?.workflow,off=unavailable||workflowUncertain;
  $('workflow-panel').hidden=!selected;$('workflow-attach').hidden=!taskData||v!==null;
  $('workflow-kind').disabled=off;for(const opt of $('workflow-kind').options)if(opt.value)opt.disabled=!taskData||!same([...workflowRoles[opt.value]].sort(),[...taskData.plan.required_roles].sort());
  $('attach-workflow').disabled=off||v!==null||!workflowRoles[$('workflow-kind').value]||!same([...workflowRoles[$('workflow-kind').value]].sort(),[...taskData.plan.required_roles].sort())||taskData.task.state!=='ready'||taskData.task.generation!==0;
  $('workflow-design-form').hidden=!v||!v.definition.required_roles.includes('design')||!!v.design;
  for(const n of ['run','scope','constraints','interfaces','acceptance'])$('design-'+n).disabled=off;
  $('freeze-design').disabled=off||v?.blocker!=='design_required'||taskData.task.state!=='ready'||!opaque($('design-run').value)||['scope','constraints','interfaces','acceptance'].some(n=>!$('design-'+n).value.trim());
  $('approve-design').disabled=off||v?.blocker!=='approval_required'||!v.design||!!v.approval||taskData.task.state!=='ready';
 }
 function renderWorkflow(){
  const root=$('workflow-record'),v=taskData.workflow;root.replaceChildren();
  if(!v){root.append(node('p','尚未附加流程。新任务须在状态操作前明确附加；多角色任务须先附加再启动。此操作不改变权限或启动阶段。'));return}
  const names={investigate:'调查与规划',review:'独立审查',change:'工程变更',bugfix:'问题修复'};
  const blockers={stage_active:'阶段仍在运行',stop_unverified:'停止释放待核对',stage_unverified:'阶段结果待核对',stage_failed:'前序阶段失败',design_required:'等待冻结完整设计',approval_required:'等待明确批准当前计划',workflow_complete:'有限阶段已结束，仍需工程验收',task_not_ready:'任务当前不可派单'};
  root.append(node('p',names[v.definition.kind]+' · '+v.definition.required_roles.map(r=>labels[r]).join(' → ')),node('p',v.blocker?blockers[v.blocker]:'下一阶段：'+labels[v.next]),node('p','附加、冻结和批准均不自动启动阶段。范围声明不改变文件权限。','details'));
  if(v.design){const d=v.design.snapshot;root.append(node('p',d.document.goal,'task-goal'));for(const [key,label] of [['scope','范围'],['constraints','约束'],['interfaces','接口'],['acceptance','验收标准']]){root.append(node('h3',label));for(const value of d.document[key])root.append(node('p',value,'task-goal'))}root.append(node('p','设计 hash：'+d.hash,'details'),node('p','验收标准 hash：'+d.acceptance_hash,'details'),node('p','设计运行：'+v.design.run_id,'details'))}
  if(v.approval)root.append(node('p','当前计划已批准 · 计划 '+v.approval.plan_revision+'。批准不替代 Runtime、预算或写权限准入。'));
 }
 function humanControls(unavailable){
  $('human-panel').hidden=!taskData?.plan.required_roles.includes('acceptance');
  $('read-decision').disabled=unavailable||humanBusy;
  $('human-reason').disabled=unavailable||humanBusy;
  const r=humanData?.report,d=humanData?.decision,t=taskData?.task;
  const decided=d&&d.plan_revision===t?.plan_revision&&d.generation===t?.generation;
  const off=unavailable||humanBusy||humanUncertain||workflowUncertain||!r||decided||!reasonValue($('human-reason').value)||!['advisory_only','needs_review'].includes(t?.state);
  $('human-return').disabled=!!off;
  $('human-accept').disabled=!!off||t?.state!=='advisory_only'||r?.hard.status!=='passed'||!r.model_valid||r.model.verdict!=='accepted';
 }
 function renderHuman(){
  const root=$('human-report');root.replaceChildren();const b=humanData,r=b?.report,d=b?.decision;
  if(r){
   const hard={passed:'通过',failed:'失败',unverified:'未验证',superseded:'已失效'},verdict={accepted:'建议接受',rejected:'建议退回',unverified:'未验证'},opinion={met:'符合',not_met:'不符合',unverified:'未验证'};
   root.append(node('p','真实测试结果：'+hard[r.hard.status]+' · 执行 '+r.hard.tests+' 项，跳过 '+r.hard.skipped+' 项。'),node('p','模型验收意见：'+verdict[r.model.verdict]+(r.model_valid?'':'（响应格式无效）')+'。'));
   const criteria=taskData.workflow.design.snapshot.document.acceptance;
   for(const [i,text] of criteria.entries()){const c=r.model.criteria.find(c=>c.index===i);root.append(node('p',(i+1)+'. '+text,'task-goal'),node('p',c?opinion[c.status]+'：'+c.reason:'尚无有效逐项意见。','details'))}
   root.append(node('p','交付运行：'+r.run_id,'details'),node('p','交付内容 hash：'+r.tree_hash,'details'));
  }
  if(d)root.append(node('p','人工决定：'+(d.action==='accept'?'已接受':'已退回')+' · 计划 '+d.plan_revision+' · generation '+d.generation+'。'),node('p',d.reason,'task-goal'));
 }
 async function readHuman(){
  if($('read-decision').disabled||!taskData)return;
  const data=taskData,id=data.task.id,version=epoch,detail=detailSerial,serial=++humanSerial;
  humanData=null;humanBusy=true;renderHuman();controls();$('human-status').textContent='正在核对当前验收证据…';
  try{
   const reply=await request('/control/v1/tasks/'+encodeURIComponent(id)+'/workflow/decision');
   if(version!==epoch||detail!==detailSerial||serial!==humanSerial||id!==selected)return;
   humanData=humanValue(reply,data,project);humanUncertain=false;renderHuman();$('human-status').textContent=humanData.report?'已核对当前验收证据，请审阅后明确决定。':'尚无可核对的最终验收证据。';
  }catch(e){if(version===epoch&&detail===detailSerial&&serial===humanSerial&&id===selected){humanUncertain=true;humanData=null;renderHuman();$('human-status').textContent=readFailure(e)+'验收操作已停用，请重新读取任务与证据。'}}
  finally{if(version===epoch&&detail===detailSerial&&serial===humanSerial){humanBusy=false;controls()}}
 }
 async function decideHuman(action){
  if(!['accept','return'].includes(action)||$('human-'+action).disabled||!taskData||!humanData?.report)return;
  const data=taskData,id=data.task.id,version=epoch,serial=detailSerial,r=humanData.report;
  const body=Object.fromEntries(reportKeys.map(k=>[k,r[k]]));body.action=action;body.reason=$('human-reason').value;
  if(!window.confirm((action==='accept'?'接受':'退回')+'任务 '+id+' 的当前交付？\n内容 hash：'+body.tree_hash+'\n原因：'+body.reason))return;
  controlBusy=true;lock(true);controls();renderList();$('human-status').textContent='正在保存人工验收决定…';
  try{
   const reply=await request('/control/v1/tasks/'+encodeURIComponent(id)+'/workflow/decision',{method:'POST',headers:{'If-Match':data.etag},body:JSON.stringify(body)});
   if(version!==epoch||serial!==detailSerial||id!==selected)return;
   const result=humanValue(reply,data,project,body);
   await loadTask(id,true);
   if(version!==epoch||id!==selected||!taskData||!same(taskData.task,result.task))throw bad();
   humanData=result;renderHuman();$('human-status').textContent=action==='accept'?'人工接受已记录，任务已完成。':'人工退回已记录，任务需要核对；未启动返工。';refreshList();
  }catch(e){if(version===epoch&&id===selected){humanUncertain=true;humanData=null;renderHuman();$('human-status').textContent=(e.status===412?'任务或证据条件已变化。':readFailure(e))+'决定结果尚待核对，请重新读取；不自动重试。'}}
  finally{controlBusy=false;lock(!!attempt||recoveryBlocked);controls();renderList()}
 }
 async function workflowWrite(action){
  const button={attach:'attach-workflow',design:'freeze-design',approve:'approve-design'}[action];if(!taskData||controlBusy||$(button).disabled)return;
  const data=taskData,version=epoch,serial=detailSerial,id=data.task.id,v=data.workflow;
  let body;
  if(action==='attach')body={kind:$('workflow-kind').value};
  else if(action==='design'){body={run_id:$('design-run').value,document:{goal:data.task.goal}};for(const n of ['scope','constraints','interfaces','acceptance'])body.document[n]=$('design-'+n).value.split(/\r?\n/).map(s=>s.trim()).filter(Boolean)}
  else{body={design_hash:v.design.snapshot.hash,acceptance_hash:v.design.snapshot.acceptance_hash};if(!window.confirm('批准任务 '+id+' 的当前计划 '+data.task.plan_revision+'？\n设计 hash：'+body.design_hash+'\n验收标准 hash：'+body.acceptance_hash+'\n此批准不自动启动阶段。'))return}
  controlBusy=true;lock(true);controls();renderList();$('workflow-status').textContent='正在核对工作流决定…';
  try{
   const reply=await request('/control/v1/tasks/'+encodeURIComponent(id)+'/workflow'+(action==='attach'?'':'/'+action),{method:'POST',headers:{'If-Match':data.etag},body:JSON.stringify(body)});
   if(version!==epoch||serial!==detailSerial||id!==selected)return;
   const result=workflowValue(reply,data,project);if(!result||action==='attach'&&result.definition.kind!==body.kind||action==='design'&&(result.design?.run_id!==body.run_id||!same(result.design.snapshot.document,body.document))||action==='approve'&&(!result.approval||result.approval.design_hash!==body.design_hash||result.approval.acceptance_hash!==body.acceptance_hash))throw bad();
   data.workflow=result;renderWorkflow();if(result.next)$('execution-role').value=result.next;for(const opt of $('execution-role').options)opt.disabled=opt.value!==result.next;
   $('workflow-status').textContent=({attach:'流程已附加',design:'设计已冻结',approve:'当前方案已批准'})[action]+'，未启动任何阶段。';
  }catch(e){if(version===epoch&&serial===detailSerial&&id===selected){workflowUncertain=true;$('workflow-status').textContent=(e.status===412?'任务条件已变化。':readFailure(e))+'决定结果尚待核对；请重新读取任务后审阅，不自动重试或启动。'}}
  finally{controlBusy=false;lock(!!attempt||recoveryBlocked);controls();renderList()}
 }
 const submissionPath=id=>'/control/v1/projects/'+encodeURIComponent(id)+'/submission';
 function renderSubmission(){
  const root=$('submission-record');root.replaceChildren();if(!attempt)return;
  const p=attempt,record=p.journal,value=p.value;
  root.append(node('p','原项目：'+p.project+'；原请求状态：'+(record?.state||'保存尚未核对')+'。'),node('p',p.goal,'task-goal'),node('p','原提交标识：'+p.key+'；预览：'+p.id,'details'),node('p','原配置版本：'+value.configuration_revision+'；原有效期：'+new Date(p.expires).toLocaleString(),'details'),node('p','上方为当前配置；以下是原请求冻结的选择，不会重新编译。','details'));
  for(const role of value.plan.required_roles){const b=value.plan.bindings[role];root.append(node('h3',labels[role]+(b.mode==='locked'?' · 指定并锁定':' · 批准候选内自动')));for(const t of b.mode==='locked'?[b.target]:b.candidates)root.append(node('p',t.resolved_model,'task-model'),node('p','路线：'+t.route.id+'@'+t.route.revision+'；effort：'+t.effort.requested_mode+(t.effort.value?' / '+t.effort.value:'')+'；账号：'+t.account+'；计费：'+t.billing_path+'；锁定：'+t.lock_enforcement,'details'))}
  root.append(node('p','原调用上限：'+value.budget.max_calls+'；原返工上限：'+value.budget.max_reworks+'；计划 hash：'+p.hash,'details'));
  if(record?.task_id)root.append(node('p','原任务：'+record.task_id,'details'));
  if(p.task)root.append(node('p','任务回执待核对：'+p.task.id,'details'));
 }
 function forget(){attempt=null;recoveryBlocked=false;clearTimeout(expiry);expiry=null;preview=null;$('preview').hidden=true;renderSubmission();lock(false);controls()}
 function savedTask(task){forget();onNotice('任务已保存：'+task.id+' · '+stateName(task.state)+' ('+task.state+')。本次核对未启动阶段。');refreshList();loadTask(task.id)}
 function originalTask(task,p,status,expected){
  taskValue(task,p.project);if(status!==expected||task.goal!==p.goal||task.plan_revision<p.revision||p.journal?.task_id&&task.id!==p.journal.task_id)throw bad();return task;
 }
 async function terminal(p,record){
  if(record.state==='abandoned'){forget();onNotice('原请求已封存，迟到提交将被拒绝。可重新预览；未启动阶段。');return true}
  if(record.state==='acknowledged'){
   const result=await request('/agent/v1/tasks/'+encodeURIComponent(record.task_id));
   if(attempt!==p)return true;
   originalTask(result.body,p,result.status,200);taskValue(result.body,p.project,result.etag);savedTask(result.body);return true;
  }
  return false;
 }
 async function readOriginal(restoring=false){
  if(busy||!project)return;
  const id=project,version=epoch,p=attempt;busy=true;recoveryBlocked=!p;lock(true);controls();
  try{
   const result=p?await request(submissionPath(id),{method:'POST',headers:{'Idempotency-Key':p.key},body:p.body}):await request(submissionPath(id));
   if(version!==epoch||attempt!==p)return;
   if(result.status!==200)throw bad();const record=journalValue(result.body,id,p);
   if(!record){if(p)throw bad();recoveryBlocked=false;lock(false);if(!restoring)onNotice('已核对，没有未解决原请求，可重新预览。');return}
   if(!p){const original=previewValue(record.preview);attempt={...original,project:record.project_id,goal:record.goal,key:record.key,body:JSON.stringify({preview_id:original.id,plan_hash:original.hash}),unknown:true,taskSent:true,action:'',journal:record}}
   else{p.journal=record;if(p.task&&p.task.id!==record.task_id)p.task=null;if(record.state==='committed'&&p.action==='abandon')p.action=''}
   recoveryBlocked=false;onRestore?.(record);renderSubmission();
   if(await terminal(attempt,record))return;
   onNotice((restoring?'已恢复原任务请求。':'已核对原任务请求。')+(record.state==='committed'?'原任务已入库，请重试同一任务提交读取并确认回执。':'尚未确认任务保存；可重试同一任务提交或放弃未提交请求。'),false);
  }catch(e){if(version===epoch){recoveryBlocked=!attempt;lock(true);renderSubmission();onNotice('原请求核对失败，暂时不能新建任务。原信息保留，请重新核对原请求。'+readFailure(e),true)}}
  finally{if(version===epoch){busy=false;controls()}}
 }
 async function findPendingProject(ids){
  if(!Array.isArray(ids)||!ids.length||ids.length>128||ids.some(id=>!opaque(id))||new Set(ids).size!==ids.length)throw bad();
  for(const id of ids){const result=await request(submissionPath(id));if(result.status!==200)throw bad();if(journalValue(result.body,id))return id;const start=await request(pendingStartPath(id));if(start.status!==200)throw bad();if(startJournalValue(start.body,id))return id}
  return ids[0];
 }
 async function setProject(id){
  if(attempt||recoveryBlocked||controlAttempt||controlBusy||startBlocked)return;
  clearWorkflow();workflowUncertain=false;clearEvents();
  project=id;taskData=null;knownRun=null;startSerial++;$('start-record').replaceChildren();$('run-record').replaceChildren();executionNotice('');epoch++;listSerial++;detailSerial++;items=[];next='';selected='';listBusy=false;detailBusy=false;invalidate();renderList();$('task-detail').replaceChildren();$('task-detail-status').textContent='选择任务读取完整目标、冻结计划和预算。';controls();if(project){refreshList();await readOriginal(true);await readStartOriginal(true)}
 }
 function setPreview(value,context){
  if(attempt||recoveryBlocked)throw bad();const frozen=previewValue(value),expires=frozen.expires;
  if(context.project_id!==project||!same(value.plan.required_roles,context.required_roles))throw bad();
  invalidate();preview={...frozen,project:context.project_id,goal:context.goal};
  $('preview').append(node('p','计划 '+value.plan.revision+' · hash '+value.plan.hash,'details'),node('p','调用上限：'+value.budget.max_calls+'；返工上限：'+value.budget.max_reworks+'；预览有效至：'+new Date(expires).toLocaleString(),'details'));
  for(const role of value.plan.required_roles){const b=value.plan.bindings[role];for(const t of b.mode==='locked'?[b.target]:b.candidates)$('preview').append(node('p',labels[role]+' · '+t.route.id+'@'+t.route.revision+' · effort：'+t.effort.requested_mode+(t.effort.value?' / '+t.effort.value:'')+' · 账号：'+t.account+' · 计费：'+t.billing_path+' · 锁定：'+t.lock_enforcement,'details'))}
  function expire(){if(attempt)return;controls();onNotice('预览已到期，请重新预览；未提交任务。',true)}
  if(expires<=Date.now())expire();else expiry=setTimeout(expire,Math.min(expires-Date.now(),2147483647));controls();return expires>Date.now();
 }
 async function submit(){
  if(busy||recoveryBlocked||controlBusy||controlAttempt||startBlocked)return;
  if(attempt?.action==='abandon'){await abandon();return}
  if(!attempt){
   if(!preview||Date.now()>=preview.expires){onNotice('预览已到期，请重新预览；未提交任务。',true);controls();return}
   try{const bytes=crypto.getRandomValues(new Uint8Array(16));attempt={...preview,key:'task_ui_'+[...bytes].map(b=>b.toString(16).padStart(2,'0')).join(''),body:JSON.stringify({preview_id:preview.id,plan_hash:preview.hash}),unknown:false,taskSent:false,journal:null,action:''}}
   catch{onNotice('无法生成任务提交标识，未发送提交。',true);return}
  }
  const p=attempt,version=epoch;let phase=p.task?'acknowledge':'prepare',reread=false;busy=true;lock(true);renderSubmission();controls();
  try{
   if(!p.task){
    const prepared=await request(submissionPath(p.project),{method:'POST',headers:{'Idempotency-Key':p.key},body:p.body});
    if(version!==epoch||attempt!==p)return;
    const record=journalValue(prepared.body,p.project,p);if(prepared.status!==200||!record)throw bad();p.journal=record;renderSubmission();
    if(await terminal(p,record))return;
    phase='submit';p.taskSent=true;
    const saved=await request('/agent/v1/tasks',{method:'POST',headers:{'Idempotency-Key':p.key},body:p.body});
    if(version!==epoch||attempt!==p)return;
    p.task=originalTask(saved.body,p,saved.status,201);renderSubmission();
   }
   phase='acknowledge';
   const resolved=await request(submissionPath(p.project)+'/acknowledge',{method:'POST',headers:{'Idempotency-Key':p.key},body:p.body});
   if(version!==epoch||attempt!==p)return;
   const record=journalValue(resolved.body,p.project,p);if(resolved.status!==200||record?.state!=='acknowledged'||record.task_id!==p.task.id)throw bad();
   savedTask(p.task);
  }catch(e){
   if(version!==epoch||attempt!==p)return;
   if(phase==='prepare'&&!p.unknown&&!p.taskSent&&e.status>=400&&e.status<500){forget();reread=e.status===409;onNotice(e.status===409?'预览已失效或提交冲突，请重新预览。未发起任务提交。':readFailure(e),true)}
   else{p.unknown=true;renderSubmission();onNotice(phase==='acknowledge'?'已获得任务保存回执，但原记录确认未核对。请重试任务保存确认。':phase==='prepare'&&!p.taskSent?'原请求保存结果未确认，尚未发起任务提交。请重试保存原请求或重新核对原请求。':'提交结果未确认。原请求已保留，请重试同一任务提交或重新核对原请求。',true)}
  }finally{if(version===epoch){busy=false;controls()}}
  if(reread)await readOriginal(true);
 }
 async function abandon(){
  if(busy||!attempt)return;const p=attempt,version=epoch;
  if(p.action!=='abandon'&&!window.confirm('仅在原请求未提交时封存它。成功后拒绝迟到的原提交，再允许重新预览。继续？'))return;
  p.action='abandon';busy=true;lock(true);controls();let reread=false;
  try{
   const result=await request(submissionPath(p.project)+'/abandon',{method:'POST',headers:{'Idempotency-Key':p.key},body:p.body});
   if(version!==epoch||attempt!==p)return;
   const record=journalValue(result.body,p.project,p);if(result.status!==200||record?.state!=='abandoned')throw bad();
   forget();onNotice('原请求已封存，迟到提交将被拒绝。可重新预览；未启动阶段。');
  }catch(e){if(version===epoch&&attempt===p){p.unknown=true;if(e.status===409){p.action='';reread=true}onNotice(e.status===409?'原请求不能放弃，可能已提交；请核对同一原请求。':'放弃结果未确认，原请求保留。请重试放弃原请求或重新核对原请求。',true)}}
  finally{if(version===epoch){busy=false;controls()}}
  if(reread)await readOriginal();
 }
 const pendingStartPath=id=>'/control/v1/projects/'+encodeURIComponent(id)+'/start-request';
 const startPath=p=>'/control/v1/tasks/'+encodeURIComponent(p.task.id)+'/start-request';
 const startHeaders=p=>({'If-Match':p.etag,'Idempotency-Key':p.key});
 function renderStart(){
  const root=$('start-record');root.replaceChildren();const p=controlAttempt?.action==='start'?controlAttempt:null;if(!p)return;
  root.append(node('p','原阶段：'+labels[p.role]+'；原请求状态：'+(p.journal?.state||'保存尚未核对')+'。'),node('p',p.task.goal,'task-goal'),node('p','原任务：'+p.task.id+'；原条件：'+p.etag+'；原启动标识：'+p.key,'details'));
  const b=p.plan.bindings[p.role];for(const t of b.mode==='locked'?[b.target]:b.candidates)root.append(node('p','冻结模型：'+t.resolved_model+'；路线：'+t.route.id+'@'+t.route.revision+'；effort：'+t.effort.requested_mode+(t.effort.value?' / '+t.effort.value:'')+'；账号：'+t.account,'details'));
  root.append(node('p','原计划 hash：'+p.plan.hash+'；上方当前配置不会改写此原请求。','details'));
  const run=p.runId||p.journal?.run_id;if(run)root.append(node('p','原运行：'+run+'；运行状态须独立核对，确认记录不证明成功或停止。','details'));
 }
 function finishStart(message){controlAttempt=null;startBlocked=false;renderStart();renderRun();executionNotice(message);lock(!!attempt||recoveryBlocked);controls();renderList()}
 async function originalStartRun(p,record){
  const reply=await request('/agent/v1/tasks/'+encodeURIComponent(p.task.id)+'/runs/'+encodeURIComponent(record.run_id));
  const run=runValue(reply,{...p,runId:record.run_id},true,true);
  return verifiedRun(run,p);
 }
 async function absentPreparation(p){
  if(!['prepare','abandon'].includes(p.phase)||p.startSent||p.journal||p.runId)return false;
  const current=await request('/agent/v1/tasks/'+encodeURIComponent(p.task.id));taskValue(current.body,p.task.project_id,current.etag);
  if(current.status!==200||current.body.id!==p.task.id||current.body.goal!==p.task.goal||current.body.generation<p.task.generation||current.body.plan_revision<p.task.plan_revision||(current.body.generation===p.task.generation&&current.body.plan_revision===p.task.plan_revision))return false;
  // Read absence again after the monotonic Task proof: a delayed prepare may
  // have committed before the Task advanced. Absence alone cannot release it.
  try{await request(startPath(p),{headers:startHeaders(p)});return false}catch(e){if(e.status!==404||e.code!=='record_unavailable')throw e}
  return true;
 }
 async function readStartOriginal(restoring=false){
  if(controlBusy||busy||!project)return;
  const scope=project,version=epoch,serial=++startSerial,old=controlAttempt?.action==='start'?controlAttempt:null;
  controlBusy=true;startBlocked=true;lock(true);controls();
  try{
   const result=await request(old?startPath(old):pendingStartPath(scope),old?{headers:startHeaders(old)}:{});
   if(version!==epoch||serial!==startSerial)return;
   const record=startJournalValue(result.body,scope,old);if(result.status!==200||old&&!record)throw bad();
   if(!record){startBlocked=false;renderStart();return}
   const p=old||{task:record.task,plan:record.plan,etag:record.etag,key:record.key,role:record.role,body:JSON.stringify({role:record.role}),action:'start',phase:'start',unknown:true};
   p.journal=record;controlAttempt=p;startBlocked=false;renderStart();
   if(record.state==='abandoned'){finishStart('原启动请求已封存，迟到启动将被拒绝。');await loadTask(p.task.id,true);return}
   if(record.state==='acknowledged'){knownRun=await originalStartRun(p,record);finishStart('原阶段运行已核对，原启动记录已确认；不自动启动。');await loadTask(p.task.id,true);return}
   if(p.phase==='abandon'&&record.state==='committed')p.phase='start';
   await loadTask(p.task.id,true);
   executionNotice((restoring?'已恢复原阶段启动请求。':'已核对原阶段启动请求。')+(record.state==='committed'?'原运行已写入；请重试原运行请求以独立核对并确认记录。':'原请求已保存；可重试同一原启动或封存未启动请求。')+'本次回读不启动阶段。');
   if(restoring)onNotice('已恢复原阶段启动请求；当前配置不会覆盖原请求，本次仅回读。',false);
  }catch(e){
   if(version===epoch&&serial===startSerial){
    if(old&&e.status===404&&e.code==='record_unavailable'){
     try{if(await absentPreparation(old)&&version===epoch&&serial===startSerial){finishStart('原准备记录不存在，原任务条件已失效；未发送启动，迟到原请求将被拒绝。');await loadTask(old.task.id,true);return}}catch{}
    }
    startBlocked=!old;renderStart();executionNotice('原启动请求核对失败，原身份保留，暂时不能新建运行。'+readFailure(e));
   }
  }
  finally{if(version===epoch&&serial===startSerial){controlBusy=false;lock(!!attempt||recoveryBlocked);controls();renderList()}}
 }
 async function abandonStart(){
  const p=controlAttempt;if(controlBusy||busy||detailBusy||p?.action!=='start'||p.runId||p.journal?.run_id)return;
  if(p.phase!=='abandon'&&!window.confirm('封存尚未写入运行的原请求，并拒绝迟到启动。已经有运行的请求不能封存。继续？'))return;
  p.phase='abandon';controlBusy=true;lock(true);controls();let reread=false;
  try{
   if(!p.journal){const prepared=await request(startPath(p),{method:'POST',headers:startHeaders(p),body:p.body});const record=startJournalValue(prepared.body,project,p);if(prepared.status!==200||!record)throw bad();p.journal=record;renderStart()}
   const result=await request(startPath(p)+'/abandon',{method:'POST',headers:startHeaders(p),body:p.body}),record=startJournalValue(result.body,project,p);
   if(result.status!==200||record?.state!=='abandoned')throw bad();
   finishStart('原启动请求已封存，迟到启动将被拒绝。');await loadTask(p.task.id,true);
  }catch(e){if(controlAttempt===p){p.unknown=true;if(e.status===409){p.phase='start';reread=true}executionNotice('封存结果未确认，原启动请求已保留；请核对原请求或重试同一封存。');renderStart()}}
  finally{controlBusy=false;lock(!!attempt||recoveryBlocked);controls();renderList()}
  if(reread)await readStartOriginal();
 }
 function runValue(reply,p,start=false,reading=false){
  const tag=/^"p([1-9][0-9]*)-g(0|[1-9][0-9]*)-([a-z_]+)"$/.exec(reply?.taskEtag||'');
  const b=reply?.body,r=b?.run,binding=p.plan.bindings[r?.role],targets=binding?.mode==='locked'?[binding.target]:binding?.candidates;
  if(![200,202,409].includes(reply?.status)||typeof b?.created!=='boolean'||!r||!opaque(r.id)||r.task_id!==p.task.id||!p.plan.required_roles.includes(r.role)||!integer(r.attempt,1)||r.generation!==(start?p.task.generation+1:p.runGeneration??p.task.generation)||!integer(r.plan_revision,1)||r.plan_revision>p.plan.revision||!['starting','running','cancelling','succeeded','failed','cancelled','interrupted','advisory_only','unknown'].includes(r.state)||r.startup_intent!==true||typeof r.launch_confirmed!=='boolean'||!targets?.some(t=>JSON.stringify(canonical(t))===JSON.stringify(canonical(r.target)))||!tag||!integer(Number(tag[1]),1)||!integer(Number(tag[2]))||Number(tag[1])<r.plan_revision||Number(tag[2])<r.generation)throw bad();
  if(start&&(r.role!==p.role||r.plan_revision!==p.plan.revision||reply.status===202&&!b.created||reply.status===200&&b.created))throw bad();
  if(reading&&(reply.status!==200||r.id!==p.runId))throw bad();
  if(reply.status===409&&(!b.error||typeof b.error.code!=='string'||typeof b.error.message!=='string'))throw bad();
  return r;
 }
 async function verifiedRun(run,p){
  const origin={...p,runGeneration:run.generation,runId:run.id};
  const read=await request('/agent/v1/tasks/'+encodeURIComponent(p.task.id)+'/runs/'+encodeURIComponent(run.id));
  const stored=runValue(read,origin,false,true);
  if(stored.role!==run.role||stored.attempt!==run.attempt||stored.plan_revision!==run.plan_revision)throw bad();
  const current=await request('/agent/v1/tasks/'+encodeURIComponent(p.task.id));taskValue(current.body,p.task.project_id,current.etag);
  if(current.status!==200||current.body.id!==p.task.id||current.body.goal!==p.task.goal||current.body.generation<stored.generation||current.body.plan_revision<stored.plan_revision)throw bad();
  return {run:stored,origin};
 }
 function renderRun(){
  const root=$('run-record');root.replaceChildren();if(!knownRun)return;const r=knownRun.run;
  root.append(node('p','已知阶段执行：'+r.id+' · '+labels[r.role]+' · '+r.state+' · attempt '+r.attempt+' · generation '+r.generation+'。'),node('p','执行模型：'+r.target.resolved_model+'；路线：'+r.target.route.id+'@'+r.target.route.revision+'；effort：'+r.target.effort.requested_mode+(r.target.effort.value?' / '+r.target.effort.value:''),'details'),node('p','阶段结果不等于整项验收；取消不表示文件回滚或预算退款。','details'));
 }
 async function execute(action,retry=false){
  if(controlBusy||busy||detailBusy||attempt||recoveryBlocked||startBlocked)return;
  if(retry&&controlAttempt?.phase==='abandon'){await abandonStart();return}
  if(!retry){
   if(controlAttempt||!taskData)return;
   const button={start:'start-role',pause:'pause-task',continue:'continue-task',cancel:'cancel-task'}[action];if(!button||$(button).disabled)return;
   if(action==='cancel'&&!window.confirm('取消此任务并停止其当前执行。取消不回滚文件或退还预算，继续？'))return;
   const role=$('execution-role').value;if(action==='start'&&!taskData.plan.required_roles.includes(role))return;
   let key='';if(action==='start'){try{key='run_ui_'+[...crypto.getRandomValues(new Uint8Array(16))].map(n=>n.toString(16).padStart(2,'0')).join('')}catch{executionNotice('无法生成启动标识，未发送请求。');return}}
   controlAttempt={task:JSON.parse(JSON.stringify(taskData.task)),plan:JSON.parse(JSON.stringify(taskData.plan)),etag:taskData.etag,role,key,action,body:JSON.stringify(action==='start'?{role}:{}),unknown:false,phase:action==='start'?'prepare':''};
  }
  const p=controlAttempt;if(!p)return;controlBusy=true;lock(true);renderStart();controls();renderList();executionNotice('正在核对运行请求…');let reread=false;
  try{
   if(p.action==='start'){
    const saved=await request(startPath(p),p.journal?{headers:startHeaders(p)}:{method:'POST',headers:startHeaders(p),body:p.body});
    const record=startJournalValue(saved.body,p.task.project_id,p);if(saved.status!==200||!record)throw bad();p.journal=record;renderStart();
    if(record.state==='abandoned'){finishStart('原启动请求已封存，迟到启动将被拒绝。');await loadTask(p.task.id,true);return}
    if(record.run_id){knownRun=await originalStartRun(p,record)}
    else{
     p.phase='start';p.startSent=true;let result;
     try{result=await request('/control/v1/tasks/'+encodeURIComponent(p.task.id)+'/start',{method:'POST',headers:startHeaders(p),body:p.body})}catch(e){if(e.status===409&&e.reply?.body?.run)result=e.reply;else throw e}
     p.received=true;const run=runValue(result,p,true);knownRun=await verifiedRun(run,p);
    }
    p.runId=knownRun.run.id;p.phase='acknowledge';renderRun();renderStart();
    if(record.state!=='acknowledged'){
     const resolved=await request(startPath(p)+'/acknowledge',{method:'POST',headers:startHeaders(p),body:JSON.stringify({role:p.role,run_id:p.runId})});
     const acknowledged=startJournalValue(resolved.body,p.task.project_id,p);if(resolved.status!==200||acknowledged?.state!=='acknowledged'||acknowledged.run_id!==p.runId)throw bad();
    }
    finishStart('启动意图已受理，原启动记录已确认，请核对实际运行状态；不自动启动下一阶段。');await loadTask(p.task.id,true);refreshList();return;
   }
   let result;
   try{result=await request('/control/v1/tasks/'+encodeURIComponent(p.task.id)+'/'+p.action,{method:'POST',headers:{'If-Match':p.etag,...(p.key?{'Idempotency-Key':p.key}:{})},body:p.body})}catch(e){if(e.status===409&&e.reply?.body?.run)result=e.reply;else throw e}
   if(controlAttempt!==p)return;
   p.received=true;
   if(![200,202,409].includes(result.status)||typeof result.body?.changed!=='boolean'||!result.body.task||result.body.task.id!==p.task.id||result.body.task.goal!==p.task.goal||result.body.task.plan_revision<p.task.plan_revision||result.body.task.generation<p.task.generation)throw bad();
   taskValue(result.body.task,p.task.project_id,result.taskEtag);
   if(result.body.run){const run=runValue({...result,body:{...result.body,created:false}},p);knownRun=await verifiedRun(run,p)}
   controlAttempt=null;renderRun();executionNotice(result.status===409?'原运行意图已记录，执行需要核对；不自动重跑。':'控制回执已核对，请核对实际任务状态；202 只表示意图已受理。');
   await loadTask(p.task.id,true);refreshList();
  }catch(e){
   if(controlAttempt!==p)return;
   if(!p.unknown&&!p.received&&!p.journal&&!e.reply?.body?.run&&(p.action==='start'?[400,409,412,428]:[400,401,403,404,412,428]).includes(e.status)&&e.code&&e.code!=='response_unavailable'){
    controlAttempt=null;taskData=null;executionNotice(e.status===412?'任务条件已变化，原请求已拒绝；重新读取后再操作。':e.status===401||e.status===403?'运行授权已失效，请重新连接并核对。':'运行请求已拒绝，请重新读取任务核对。');
    if(p.action==='start'&&e.status===409){startBlocked=true;reread=true}
   }else{p.unknown=true;executionNotice((p.action==='start'&&p.phase==='prepare'?'原启动请求保存结果未确认，尚未发送启动。':p.phase==='acknowledge'?'原启动记录确认未核对。':'运行结果未确认。')+'原运行请求已保留，不能换 key 或条件重新启动；请核对或重试原请求。')}
  }finally{controlBusy=false;renderStart();lock(!!attempt||recoveryBlocked);controls();renderList()}
  if(reread)await readStartOriginal();
 }
 for(const action of ['attach','design','approve'])$({attach:'attach-workflow',design:'freeze-design',approve:'approve-design'}[action]).onclick=()=>workflowWrite(action);
 $('read-decision').onclick=readHuman;$('human-reason').oninput=controls;$('human-accept').onclick=()=>decideHuman('accept');$('human-return').onclick=()=>decideHuman('return');
 $('workflow-kind').onchange=controls;for(const n of ['run','scope','constraints','interfaces','acceptance'])$('design-'+n).oninput=controls;$('execution-role').onchange=controls;
 $('refresh-start').onclick=()=>readStartOriginal();$('abandon-start').onclick=abandonStart;
 $('read-events').onclick=()=>readEvents();$('reset-events').onclick=()=>readEvents(true);
 $('start-role').onclick=()=>execute('start');$('pause-task').onclick=()=>execute('pause');$('continue-task').onclick=()=>execute('continue');$('cancel-task').onclick=()=>execute('cancel');$('retry-execution').onclick=()=>execute('',true);
 $('submit-task').onclick=submit;$('retry-task').onclick=submit;$('refresh-submission').onclick=()=>readOriginal();$('abandon-submission').onclick=abandon;$('refresh-tasks').onclick=()=>refreshList();$('older-tasks').onclick=()=>refreshList(true);$('reload-task').onclick=()=>loadTask(selected);
 window.addEventListener('beforeunload',event=>{if(attempt||controlAttempt){event.preventDefault();event.returnValue=''}});
 controls();return {setProject,setPreview,invalidate,findPendingProject};
}
