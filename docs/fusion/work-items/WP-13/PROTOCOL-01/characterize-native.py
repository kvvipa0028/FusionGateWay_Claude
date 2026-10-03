#!/usr/bin/env python3
"""Synthetic pinned-native diagnosis only; not a managed Worker or route admission."""
import argparse,contextlib,hashlib,json,os,signal,subprocess,tempfile,threading
from pathlib import Path
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
pin=Path('/Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64')
assert hashlib.sha256(pin.read_bytes()).hexdigest()=='1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05'
parser=argparse.ArgumentParser()
parser.add_argument('--mode',choices=['baseline','read','restricted','invalid-model'],default='restricted')
parser.add_argument('--output',required=True,type=Path)
opt=parser.parse_args()
opt.output.mkdir(parents=True,exist_ok=True)
records=[]
class Handler(BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_POST(self):
  raw=self.rfile.read(int(self.headers.get('Content-Length','0')));body=json.loads(raw)
  records.append({'path':self.path,'model':body.get('model'),'stream':body.get('stream'),'tools_count':len(body.get('tools',[])), 'tools':[x.get('function',{}).get('name') for x in body.get('tools',[])], 'purpose': 'title' if any(x.get('function',{}).get('name')=='session_title' for x in body.get('tools',[])) else ('dashboard-summary' if any('dashboard line' in str(x.get('content','')) for x in body.get('messages',[])) else 'agent')})
  if body.get('stream'):
   chunks=[{'id':'chatcmpl-fixture','object':'chat.completion.chunk','created':1780000000,'model':body.get('model'),'choices':[{'index':0,'delta':{'role':'assistant','content':'Fixture ready.'},'finish_reason':None}]},{'id':'chatcmpl-fixture','object':'chat.completion.chunk','created':1780000000,'model':body.get('model'),'choices':[{'index':0,'delta':{},'finish_reason':'stop'}],'usage':{'prompt_tokens':3,'completion_tokens':2,'total_tokens':5}}]
   data=''.join('data: '+json.dumps(c)+'\n\n'for c in chunks)+'data: [DONE]\n\n'
   self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Content-Length',str(len(data.encode())));self.end_headers();self.wfile.write(data.encode())
  else:
   data=json.dumps({'id':'chatcmpl-fixture','object':'chat.completion','created':1780000000,'model':body.get('model'),'choices':[{'index':0,'message':{'role':'assistant','content':'Fixture ready.'},'finish_reason':'stop'}],'usage':{'prompt_tokens':3,'completion_tokens':2,'total_tokens':5}}).encode()
   self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
with contextlib.ExitStack() as cleanup:
 temp=cleanup.enter_context(tempfile.TemporaryDirectory(prefix='fusion-grok-characterize-'));root=Path(temp).resolve()
 server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
 cleanup.callback(server.server_close);cleanup.callback(server.shutdown)
 gh=root/'grok';gh.mkdir(mode=0o700);work=root/'workspace';work.mkdir(mode=0o700)
 config='[model.fixture]\nmodel = "fixture-model"\nbase_url = "http://127.0.0.1:'+str(server.server_port)+'/v1"\napi_key = "fixture-key"\n[models]\ndefault = "fixture"\nsession_summary = "fixture"\n[features]\nturn_summary = false\n[cli]\nauto_update = false\n'
 if opt.mode!='restricted':config=config.replace('session_summary = \"fixture\"\n[features]\nturn_summary = false\n','')
 (gh/'config.toml').write_text(config)
 (gh/'config.toml').chmod(0o600)
 profile=root/'profile.sb'
 profile.write_text('(version 1)\n(deny default)\n(allow process-exec (literal '+json.dumps(str(pin))+'))\n(allow signal (target self))\n(allow sysctl-read)\n(allow mach-lookup)\n(deny mach-lookup (global-name "com.apple.securityd"))\n(allow file-read-metadata)\n(allow file-read* (literal "/"))\n(allow file-read* file-map-executable (subpath "/System") (subpath "/usr/lib") (subpath "/usr/share") (subpath "/Library/Apple") (subpath "/private/var/db/timezone") (literal "/dev/null") (literal "/dev/urandom") (literal '+json.dumps(str(pin))+') (subpath '+json.dumps(str(root))+'))\n(allow file-write* (subpath '+json.dumps(str(root))+') (literal "/dev/null"))\n(allow network-outbound (remote tcp "localhost:'+str(server.server_port)+'"))\n')
 env={'PATH':'/usr/bin:/bin','HOME':str(root),'GROK_HOME':str(gh),'XDG_CONFIG_HOME':str(root/'config'),'XDG_CACHE_HOME':str(root/'cache'),'XDG_DATA_HOME':str(root/'data'),'TMPDIR':str(root),'LANG':'en_US.UTF-8','DO_NOT_TRACK':'1','RUST_LOG':'error'}
 args=[str(pin),'--single','Reply Fixture ready.','--model','fixture','--output-format','streaming-json','--permission-mode','dontAsk','--no-subagents','--max-turns','1','--tools','Read','--disable-web-search','--cwd',str(work),'--session-id','11111111-1111-4111-8111-111111111111']
 if opt.mode=='baseline':args[args.index('--tools')+1]=''
 if opt.mode=='invalid-model':args[args.index('--model')+1]='invalid-fixture-model'
 args+=['--no-auto-update']
 if opt.mode=='restricted':args+=['--disallowed-tools','search_tool,use_tool']
 p=subprocess.Popen(['/usr/bin/sandbox-exec','-f',str(profile),*args],env=env,cwd=work,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,start_new_session=True)
 def stop():
  try:os.killpg(p.pid,signal.SIGKILL)
  except ProcessLookupError:pass
  p.communicate(timeout=3)
 cleanup.callback(stop)
 timedout=False
 try:out,err=p.communicate(timeout=20)
 except subprocess.TimeoutExpired:
  timedout=True;os.killpg(p.pid,signal.SIGKILL);out,err=p.communicate(timeout=3)
 normalized=lambda s:s.replace(str(root),'<fixture-root>')
 (opt.output/'native-stream.jsonl').write_text(normalized(out))
 (opt.output/'native-stderr.log').write_text(normalized(err))
 result={'mode':opt.mode,'native_version':'grok1.0.48/b94d5072c95f','executable_sha256':'1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05','pid':p.pid,'exit':p.returncode,'timeout':timedout,'http':records,'real_model_calls':0,'real_credentials_used':False,'worker_isolation_verified':False,'stdout':normalized(out),'stderr':normalized(err)}
 (opt.output/'characterization.json').write_text(json.dumps(result,indent=2))
 print(json.dumps({k:v for k,v in result.items() if k not in ('stdout','stderr')},indent=2))
 assert not timedout
 assert p.returncode==(1 if opt.mode=='invalid-model' else 0)
 if opt.mode=='invalid-model':assert not records
 elif opt.mode=='restricted':
  assert len(records)==2 and {r['model'] for r in records}=={'fixture-model'}
  assert [r['tools'] for r in records if r['purpose']=='agent']==[['read_file']]
  assert any(json.loads(l).get('type')=='end' for l in out.splitlines())
 else:assert len(records)==3 and any(r['model']=='grok-4.6' for r in records)
