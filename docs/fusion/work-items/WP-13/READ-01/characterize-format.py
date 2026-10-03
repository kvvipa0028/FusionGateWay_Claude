#!/usr/bin/env python3
"""Pinned Native tool diagnosis against owned synthetic files only; no admission."""
import argparse,contextlib,hashlib,json,os,signal,subprocess,tempfile,threading
from pathlib import Path
from http.server import ThreadingHTTPServer,BaseHTTPRequestHandler
pin=Path('/Users/zhaojianzhi/.grok/downloads/grok-1.0.48-macos-aarch64')
assert hashlib.sha256(pin.read_bytes()).hexdigest()=='1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05'
parser=argparse.ArgumentParser()
parser.add_argument('--mode',choices=['read','outside','private-config'],required=True)
parser.add_argument('--output',required=True,type=Path)
opt=parser.parse_args();opt.output.mkdir(parents=True,exist_ok=True)
records=[];main_calls=0
with contextlib.ExitStack() as cleanup:
 root=Path(cleanup.enter_context(tempfile.TemporaryDirectory(prefix='fusion-grok-tool-'))).resolve()
 outside=Path(cleanup.enter_context(tempfile.TemporaryDirectory(prefix='fusion-grok-denied-'))).resolve()
 for n in ['grok','workspace','config','cache','data','tmp']:(root/n).mkdir(mode=0o700)
 sentinel='FUSION_OWNED_READ_MARKER'
 denied_marker='FUSION_OWNED_DENIED_MARKER'
 private_marker='FUSION_SYNTHETIC_PRIVATE_CONFIG_MARKER'
 (root/'workspace/fixture.txt').write_text(sentinel+'\n'+''.join('line'+str(i)+'\n' for i in range(2,14)))
 (outside/'fixture.txt').write_text(denied_marker+'\n')
 target=root/'workspace/fixture.txt' if opt.mode=='read' else (outside/'fixture.txt' if opt.mode=='outside' else root/'grok/config.toml')
 class Handler(BaseHTTPRequestHandler):
  def log_message(self,*a):pass
  def do_POST(self):
   global main_calls
   length=int(self.headers.get('Content-Length','0'))
   if length<1 or length>1<<20:self.send_error(413);return
   body=json.loads(self.rfile.read(length))
   names=[x.get('function',{}).get('name') for x in body.get('tools',[])]
   title='session_title' in names
   tool_messages=[m for m in body.get('messages',[]) if m.get('role')=='tool']
   records.append({'path':self.path,'model':body.get('model'),'tools':names,'purpose':'title' if title else 'agent','message_roles':[m.get('role') for m in body.get('messages',[])],'tool_messages':tool_messages,'assistant_tool_calls':[m.get('tool_calls') for m in body.get('messages',[]) if m.get('tool_calls')],'private_marker_in_body':private_marker in json.dumps(body),'read_marker_in_body':sentinel in json.dumps(body),'denied_marker_in_body':denied_marker in json.dumps(body)})
   if not title:main_calls+=1
   header={'id':'chatcmpl-tools-fixture','object':'chat.completion.chunk','created':1780000000,'model':body.get('model')}
   if not title and main_calls==1:
    args=json.dumps({'target_file':str(target)})
    delta={'role':'assistant','content':None,'tool_calls':[{'index':0,'id':'call_fixture_read','type':'function','function':{'name':'read_file','arguments':args}}]}
    reason='tool_calls'
   else:delta={'role':'assistant','content':'Fixture complete.'};reason='stop'
   chunks=[dict(header,choices=[{'index':0,'delta':delta,'finish_reason':None}]),dict(header,choices=[{'index':0,'delta':{},'finish_reason':reason}],usage={'prompt_tokens':3,'completion_tokens':2,'total_tokens':5})]
   data=(''.join('data: '+json.dumps(c)+'\n\n' for c in chunks)+'data: [DONE]\n\n').encode()
   self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 server=ThreadingHTTPServer(('127.0.0.1',0),Handler);threading.Thread(target=server.serve_forever,daemon=True).start()
 cleanup.callback(server.server_close);cleanup.callback(server.shutdown)
 config='[model.fixture]\nmodel="fixture-model"\nbase_url="http://127.0.0.1:'+str(server.server_port)+'/v1"\napi_key="'+private_marker+'"\n[models]\ndefault="fixture"\nsession_summary="fixture"\n[features]\nturn_summary=false\n[cli]\nauto_update=false\n'
 (root/'grok/config.toml').write_text(config);(root/'grok/config.toml').chmod(0o600)
 q=lambda p:json.dumps(str(p))
 profile='(version 1)\n(deny default)\n(allow signal (target self))\n(allow sysctl-read)\n(allow mach-lookup)\n(deny mach-lookup (global-name "com.apple.securityd"))\n(allow file-read-metadata)\n(allow file-read* (literal "/"))\n(allow file-read* file-map-executable (subpath "/System") (subpath "/usr/lib") (subpath "/usr/share") (subpath "/Library/Apple") (subpath "/private/var/db/timezone") (literal "/dev/null") (literal "/dev/urandom") (literal '+q(pin)+') (subpath '+q(root)+'))\n(allow process-exec (literal '+q(pin)+'))\n(allow file-write* (subpath '+q(root)+') (literal "/dev/null"))\n(allow network-outbound (remote tcp "localhost:'+str(server.server_port)+'"))\n'
 env={'PATH':'/usr/bin:/bin','HOME':str(root),'GROK_HOME':str(root/'grok'),'XDG_CONFIG_HOME':str(root/'config'),'XDG_CACHE_HOME':str(root/'cache'),'XDG_DATA_HOME':str(root/'data'),'TMPDIR':str(root/'tmp'),'LANG':'en_US.UTF-8','DO_NOT_TRACK':'1','RUST_LOG':'error'}
 args=['/usr/bin/sandbox-exec','-p',profile,str(pin),'--single','Read the specified fixture, then reply Fixture complete.','--model','fixture','--output-format','streaming-json','--permission-mode','dontAsk','--no-subagents','--max-turns','3','--tools','Read','--disallowed-tools','search_tool,use_tool','--disable-web-search','--no-auto-update','--cwd',str(root/'workspace'),'--session-id','22222222-2222-4222-8222-222222222222']
 p=subprocess.Popen(args,env=env,cwd=root/'workspace',stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,start_new_session=True)
 def stop():
  try:os.killpg(p.pid,signal.SIGKILL)
  except ProcessLookupError:pass
  p.communicate(timeout=3)
 cleanup.callback(stop)
 timedout=False
 try:out,err=p.communicate(timeout=20)
 except subprocess.TimeoutExpired:
  timedout=True;os.killpg(p.pid,signal.SIGKILL);out,err=p.communicate(timeout=3)
 norm=lambda s:s.replace(str(root),'<fixture-root>').replace(str(outside),'<denied-root>')
 frames=[json.loads(x) for x in out.splitlines()]
 normalized=json.loads(norm(json.dumps(records)))
 result={'mode':opt.mode,'native_version':'1.0.48/b94d5072c95f','executable_sha256':'1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05','exit':p.returncode,'timeout':timedout,'http':normalized,'frame_types':[f.get('type') for f in frames],'real_model_calls':0,'real_credentials_used':False,'production_isolation_verified':False,'route_admitted':False}
 (opt.output/'native-stream.jsonl').write_text(norm(out));(opt.output/'native-stderr.log').write_text(norm(err));(opt.output/'characterization.json').write_text(json.dumps(result,indent=2)+'\n')
 print(json.dumps(result,indent=2))
 assert not timedout and p.returncode==0
 assert len(records)==3 and main_calls==2
 assert all(r['model']=='fixture-model' for r in records)
 assert [r['tools'] for r in records if r['purpose']=='agent']==[['read_file'],['read_file']]
 assert records[-1]['tool_messages']
 if opt.mode=='read':assert records[-1]['read_marker_in_body']
 else:assert not records[-1]['read_marker_in_body'] and not records[-1]['denied_marker_in_body'] and not records[-1]['private_marker_in_body']
 assert any(f.get('type')=='end' for f in frames)
