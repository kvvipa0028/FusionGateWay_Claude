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
const stateName=s=>({ready:'待执行',running:'运行中',paused:'已暂停',pausing:'暂停中',cancelling:'取消中',cancelled:'已取消',needs_review:'需要核对',succeeded:'成功',failed:'失败'})[s]||s;
function readFailure(e){return e.status===401||e.status===403?'管理授权已失效，请重新连接。':e.status===503?'本机服务不可用或已撤销。':'读取失败，请重新读取。'}
export function createWorkbench({request,onLock,onNotice,onRestore}){
 let project='',preview=null,attempt=null,expiry=null,epoch=0,listSerial=0,detailSerial=0,listBusy=false,detailBusy=false,next='',items=[],selected='',busy=false,recoveryBlocked=false,taskData=null,controlAttempt=null,controlBusy=false,knownRun=null;
 let eventTask='',eventSerial=0,eventCursor=0,eventRows=[],eventBusy=false;
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
 function lock(value){onLock(value||controlBusy||!!controlAttempt)}
 function executionNotice(message){$('execution-status').textContent=message}
 function controls(){
  $('task-submit').hidden=!preview&&!attempt;
  $('submit-task').hidden=!!attempt;$('submit-task').disabled=busy||controlBusy||!!controlAttempt||recoveryBlocked||!preview||Date.now()>=preview.expires;
  $('retry-task').hidden=!attempt||attempt.action==='abandon';$('retry-task').disabled=busy;
  $('retry-task').textContent=attempt?.task?'重试任务保存确认':attempt&&!attempt.journal?'重试保存原请求':'重试同一任务提交';
  $('submission-recovery').hidden=!attempt&&!recoveryBlocked;
  $('refresh-submission').disabled=busy||!project;
  $('abandon-submission').hidden=!attempt||!!attempt.task||['committed','acknowledged'].includes(attempt.journal?.state);
  $('abandon-submission').disabled=busy;$('abandon-submission').textContent=attempt?.action==='abandon'?'重试放弃原请求':'放弃未提交请求';
  $('refresh-tasks').disabled=!project||listBusy;$('older-tasks').hidden=!next;$('older-tasks').disabled=listBusy;
  $('reload-task').disabled=!selected||detailBusy||controlBusy;
  $('task-events').hidden=!selected;$('read-events').disabled=eventBusy||detailBusy||!taskData||busy||controlBusy;$('reset-events').disabled=$('read-events').disabled;
  const unavailable=busy||!!attempt||recoveryBlocked||controlBusy||!!controlAttempt||detailBusy||!taskData;
  $('execution-controls').hidden=!selected;
  $('execution-role').disabled=unavailable;
  $('start-role').disabled=unavailable||taskData?.task.state!=='ready'||taskData.budget.used_calls>=taskData.budget.max_calls;
  $('pause-task').disabled=unavailable||!['ready','running','paused','pausing'].includes(taskData?.task.state);
  $('continue-task').disabled=unavailable||taskData?.task.state!=='paused';
  $('cancel-task').disabled=unavailable||!['ready','running','paused','pausing','cancelling','failed','advisory_only'].includes(taskData?.task.state);
  $('retry-execution').hidden=!controlAttempt;$('retry-execution').disabled=controlBusy||busy||detailBusy;

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
  const scope=project,version=epoch,serial=++detailSerial;if(selected!==id){knownRun=null;$('run-record').replaceChildren();executionNotice('')}selected=id;taskData=null;detailBusy=true;$('task-detail').replaceChildren();$('task-detail-status').textContent='正在读取任务详情…';controls();
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
   taskData={...data,etag:'"p'+data.task.plan_revision+'-g'+data.task.generation+'-'+data.task.state+'"'};
   const choices=$('execution-role'),picked=choices.value;choices.replaceChildren();for(const role of data.plan.required_roles){const option=node('option',labels[role]);option.value=role;choices.append(option)}if(data.plan.required_roles.includes(picked))choices.value=picked;
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
   if(controlAttempt?.action!=='start'&&controlAttempt&&taskData.etag!==controlAttempt.etag){controlAttempt=null;lock(!!attempt||recoveryBlocked);executionNotice('已读取当前任务条件；原控制条件已失效。请核对实际状态，不自动重试。');renderList()}
   if(knownRun){try{const receipt=await request('/agent/v1/tasks/'+encodeURIComponent(id)+'/runs/'+encodeURIComponent(knownRun.run.id));if(version!==epoch||serial!==detailSerial)return;knownRun.run=runValue(receipt,knownRun.origin,false,true);renderRun()}catch(e){if(version!==epoch||serial!==detailSerial)return;$('run-record').replaceChildren();executionNotice('任务条件已读取；阶段执行回读失败或版本已变化，请重新读取核对。')}}

  }catch(e){if(version===epoch&&serial===detailSerial){taskData=null;$('task-detail').replaceChildren();$('run-record').replaceChildren();$('task-detail-status').textContent=readFailure(e)}}
  finally{if(version===epoch&&serial===detailSerial){detailBusy=false;controls()}}
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
  for(const id of ids){const result=await request(submissionPath(id));if(result.status!==200)throw bad();if(journalValue(result.body,id))return id}
  return ids[0];
 }
 async function setProject(id){
  if(attempt||recoveryBlocked||controlAttempt||controlBusy)return;
  clearEvents();
  project=id;taskData=null;knownRun=null;$('run-record').replaceChildren();executionNotice('');epoch++;listSerial++;detailSerial++;items=[];next='';selected='';listBusy=false;detailBusy=false;invalidate();renderList();$('task-detail').replaceChildren();$('task-detail-status').textContent='选择任务读取完整目标、冻结计划和预算。';controls();if(project){refreshList();await readOriginal(true)}
 }
 function setPreview(value,context){
  if(attempt||recoveryBlocked)throw bad();const frozen=previewValue(value),expires=frozen.expires;
  if(context.project_id!==project||value.plan.required_roles.length!==1||value.plan.required_roles[0]!==context.required_roles[0])throw bad();
  invalidate();preview={...frozen,project:context.project_id,goal:context.goal};
  $('preview').append(node('p','计划 '+value.plan.revision+' · hash '+value.plan.hash,'details'),node('p','调用上限：'+value.budget.max_calls+'；返工上限：'+value.budget.max_reworks+'；预览有效至：'+new Date(expires).toLocaleString(),'details'));
  for(const role of value.plan.required_roles){const b=value.plan.bindings[role];for(const t of b.mode==='locked'?[b.target]:b.candidates)$('preview').append(node('p',labels[role]+' · '+t.route.id+'@'+t.route.revision+' · effort：'+t.effort.requested_mode+(t.effort.value?' / '+t.effort.value:'')+' · 账号：'+t.account+' · 计费：'+t.billing_path+' · 锁定：'+t.lock_enforcement,'details'))}
  function expire(){if(attempt)return;controls();onNotice('预览已到期，请重新预览；未提交任务。',true)}
  if(expires<=Date.now())expire();else expiry=setTimeout(expire,Math.min(expires-Date.now(),2147483647));controls();return expires>Date.now();
 }
 async function submit(){
  if(busy||recoveryBlocked||controlBusy||controlAttempt)return;
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
  if(controlBusy||busy||detailBusy||attempt||recoveryBlocked)return;
  if(!retry){
   if(controlAttempt||!taskData)return;
   const button={start:'start-role',pause:'pause-task',continue:'continue-task',cancel:'cancel-task'}[action];if(!button||$(button).disabled)return;
   if(action==='cancel'&&!window.confirm('取消此任务并停止其当前执行。取消不回滚文件或退还预算，继续？'))return;
   const role=$('execution-role').value;if(action==='start'&&!taskData.plan.required_roles.includes(role))return;
   let key='';if(action==='start'){try{key='run_ui_'+[...crypto.getRandomValues(new Uint8Array(16))].map(n=>n.toString(16).padStart(2,'0')).join('')}catch{executionNotice('无法生成启动标识，未发送请求。');return}}
   controlAttempt={task:JSON.parse(JSON.stringify(taskData.task)),plan:JSON.parse(JSON.stringify(taskData.plan)),etag:taskData.etag,role,key,action,body:JSON.stringify(action==='start'?{role}:{}),unknown:false};
  }
  const p=controlAttempt;if(!p)return;controlBusy=true;lock(true);controls();renderList();executionNotice('正在核对运行请求…');
  try{
   let result;
   try{result=await request('/control/v1/tasks/'+encodeURIComponent(p.task.id)+'/'+p.action,{method:'POST',headers:{'If-Match':p.etag,...(p.key?{'Idempotency-Key':p.key}:{})},body:p.body})}catch(e){if(e.status===409&&e.reply?.body?.run)result=e.reply;else throw e}
   if(controlAttempt!==p)return;
   p.received=true;
   if(p.action==='start'){
    const run=runValue(result,p,true);knownRun=await verifiedRun(run,p);
   }else{
    if(![200,202,409].includes(result.status)||typeof result.body?.changed!=='boolean'||!result.body.task||result.body.task.id!==p.task.id||result.body.task.goal!==p.task.goal||result.body.task.plan_revision<p.task.plan_revision||result.body.task.generation<p.task.generation)throw bad();
    taskValue(result.body.task,p.task.project_id,result.taskEtag);
    if(result.body.run){const run=runValue({...result,body:{...result.body,created:false}},p);knownRun=await verifiedRun(run,p)}
   }
   controlAttempt=null;renderRun();executionNotice(result.status===409?'原运行意图已记录，执行需要核对；不自动重跑。':p.action==='start'?'启动意图已受理，请核对实际运行状态。':'控制回执已核对，请核对实际任务状态；202 只表示意图已受理。');
   await loadTask(p.task.id,true);refreshList();
  }catch(e){
   if(controlAttempt!==p)return;
   if(!p.unknown&&!p.received&&!e.reply?.body?.run&&[400,401,403,404,412,428].includes(e.status)&&e.code&&e.code!=='response_unavailable'){
    controlAttempt=null;taskData=null;executionNotice(e.status===412?'任务条件已变化，原请求已拒绝；重新读取后再操作。':e.status===401||e.status===403?'运行授权已失效，请重新连接并核对。':'运行请求已拒绝，请重新读取任务核对。');
   }else{p.unknown=true;executionNotice('运行结果未确认。原运行请求已保留，不能换 key 或条件重新启动；请重试原运行请求或重新读取任务。')}
  }finally{controlBusy=false;lock(!!attempt||recoveryBlocked);controls();renderList()}
 }
 $('read-events').onclick=()=>readEvents();$('reset-events').onclick=()=>readEvents(true);
 $('start-role').onclick=()=>execute('start');$('pause-task').onclick=()=>execute('pause');$('continue-task').onclick=()=>execute('continue');$('cancel-task').onclick=()=>execute('cancel');$('retry-execution').onclick=()=>execute('',true);
 $('submit-task').onclick=submit;$('retry-task').onclick=submit;$('refresh-submission').onclick=()=>readOriginal();$('abandon-submission').onclick=abandon;$('refresh-tasks').onclick=()=>refreshList();$('older-tasks').onclick=()=>refreshList(true);$('reload-task').onclick=()=>loadTask(selected);
 window.addEventListener('beforeunload',event=>{if(attempt||controlAttempt){event.preventDefault();event.returnValue=''}});
 controls();return {setProject,setPreview,invalidate,findPendingProject};
}
