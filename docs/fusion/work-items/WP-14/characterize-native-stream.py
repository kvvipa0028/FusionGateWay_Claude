from pathlib import Path
import json,hashlib,threading,tempfile,subprocess,http.server,os,time
exe=Path('/Users/zhaojianzhi/.local/share/claude/versions/2.1.287')
assert hashlib.sha256(exe.read_bytes()).hexdigest()=='6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea'
key='fixture-only.private-glm-key';calls=[]
class Handler(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  size=int(self.headers.get('Content-Length','0'))
  if not 0<size<1048576:self.send_error(400);return
  body=json.loads(self.rfile.read(size))
  calls.append({'path':self.path,'model':body.get('model'),'tool_count':len(body.get('tools',[])),'bearer_fixture':self.headers.get('Authorization')=='Bearer '+key,'api_key_fixture':self.headers.get('X-Api-Key')==key})
  if body.get('model')!='glm-5.3' or body.get('tools'):
   self.send_error(400);return
  msg={'id':'fixture-message','type':'message','role':'assistant','model':'glm-5.3','content':[],'stop_reason':None,'stop_sequence':None,'usage':{'input_tokens':8,'output_tokens':0}}
  events=[('message_start',{'type':'message_start','message':msg}),('content_block_start',{'type':'content_block_start','index':0,'content_block':{'type':'text','text':''}}),('content_block_delta',{'type':'content_block_delta','index':0,'delta':{'type':'text_delta','text':'FUSION_FIXTURE_OK'}}),('content_block_stop',{'type':'content_block_stop','index':0}),('message_delta',{'type':'message_delta','delta':{'stop_reason':'end_turn','stop_sequence':None},'usage':{'output_tokens':8}}),('message_stop',{'type':'message_stop'})]
  payload=''.join('event: '+n+'\ndata: '+json.dumps(e)+'\n\n' for n,e in events).encode()
  self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Content-Length',str(len(payload)));self.end_headers();self.wfile.write(payload)
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
q=lambda s:json.dumps(str(s))
report={'fixture_only':True,'actual_account_used':False,'runtime_version':'2.1.287','real_model_calls':0,'strict_lock_verified':False}
try:
 with tempfile.TemporaryDirectory(prefix='fusion-claude-stream-') as t:
  root=Path(t).resolve();worker=root/'worker';project=root/'project'
  for p in (worker,project):p.mkdir(mode=0o700)
  for n in ('home','config','cache','data','tmp'):(worker/n).mkdir(mode=0o700)
  profile=f'(version 1)\n(allow default)\n(deny network-outbound (require-not (remote tcp "localhost:{server.server_port}")))\n'
  report['worker_isolation_verified']=False
  report['probe_context']='trusted tool-free synthetic characterization; not a production worker'
  env={'PATH':'/usr/bin:/bin','HOME':str(worker/'home'),'USERPROFILE':str(worker/'home'),'XDG_CONFIG_HOME':str(worker/'config'),'XDG_CACHE_HOME':str(worker/'cache'),'XDG_DATA_HOME':str(worker/'data'),'TMPDIR':str(worker/'tmp'),'CLAUDE_CONFIG_DIR':str(worker/'config'/'claude'),'CLAUDE_CODE_TMPDIR':str(worker/'tmp'),'ANTHROPIC_API_KEY':key,'ANTHROPIC_AUTH_TOKEN':key,'ANTHROPIC_BASE_URL':f'http://127.0.0.1:{server.server_port}/api/anthropic','ANTHROPIC_MODEL':'glm-5.3','DISABLE_UPDATES':'1','DISABLE_TELEMETRY':'1','DISABLE_ERROR_REPORTING':'1','CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC':'1','DISABLE_COMPACT':'1','DO_NOT_TRACK':'1','API_TIMEOUT_MS':'5000'}
  args=['--bare','--restricted','--strict-mcp-config','--setting-sources','','--tools','','--disable-slash-commands','--no-chrome','--no-session-persistence','--permission-mode','dontAsk','--model','glm-5.3','--system-prompt','You are a tool-free synthetic connection fixture.','-p','--output-format','stream-json','--verbose','--include-partial-messages','Reply FUSION_FIXTURE_OK.']
  p=subprocess.Popen(['/usr/bin/sandbox-exec','-p',profile,str(exe),*args],cwd=project,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  try:stdout,stderr=p.communicate(timeout=20)
  except subprocess.TimeoutExpired:
   p.terminate()
   try:stdout,stderr=p.communicate(timeout=2)
   except subprocess.TimeoutExpired:p.kill();stdout,stderr=p.communicate(timeout=2)
   report['timeout']=True
  report.update({'exit_code':p.returncode,'os_reaped':True,'profile_sha256':hashlib.sha256(profile.encode()).hexdigest(),'executable_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'fork_allowed':True,'network_scope':'synthetic localhost port only','stdout_bytes':len(stdout),'stderr_bytes':len(stderr),'stdout_sha256':hashlib.sha256(stdout).hexdigest(),'stderr_sha256':hashlib.sha256(stderr).hexdigest(),'calls':calls})
  safe=stdout.decode(errors='replace').replace(str(root),'<fixture-root>').replace(key,'<synthetic-credential>')
  Path('.fusion-dev/implementation/wp14-native-stream.jsonl').write_text(safe)
  report['event_types']=[]
  for line in safe.splitlines():
   try:
    d=json.loads(line);report['event_types'].append({'type':d.get('type'),'subtype':d.get('subtype')})
   except json.JSONDecodeError:pass
  if p.returncode:report['stderr_sanitized']=stderr.decode(errors='replace').replace(str(root),'<fixture-root>').replace(key,'<synthetic-credential>')[:1500]
finally:server.shutdown();server.server_close();thread.join(timeout=2)
Path('.fusion-dev/implementation/wp14-native-stream-report.json').write_text(json.dumps(report,indent=2)+'\n')
print(json.dumps(report,indent=2))
