import tempfile, pathlib, os, subprocess, json, selectors, time, hashlib, platform
exe=pathlib.Path('/Users/zhaojianzhi/.codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex').resolve()
if hashlib.sha256(exe.read_bytes()).hexdigest() != '112fae7a5a1223e673c8a1791d32338f37df8b527ff1159bb8adac6c4dbf1b4b':raise SystemExit('pinned native executable mismatch')
out=pathlib.Path('.fusion-dev/implementation/wp12-native-handshake.json')
q=lambda s:json.dumps(str(s))
with tempfile.TemporaryDirectory(prefix='fusion-codex-metadata-') as temp:
 root=pathlib.Path(temp).resolve(); worker=root/'worker'; work=root/'project'
 for p in (worker,work): p.mkdir(mode=0o700)
 for n in ('home','config','cache','data','tmp'): (worker/n).mkdir(mode=0o700)
 profile='(version 1)\n(deny default)\n(allow signal (target self))\n(allow sysctl-read (sysctl-name-prefix "hw.") (sysctl-name "kern.osrelease") (sysctl-name "kern.osversion") (sysctl-name "kern.ostype") (sysctl-name "kern.bootargs"))\n(allow file-read-metadata)\n(allow file-read* (literal "/"))\n'
 for p in ('/System/Library','/usr/lib','/Library/Apple/System/Library'): profile+=f'(allow file-read* file-map-executable (subpath {q(p)}))\n'
 for p in (worker,work): profile+=f'(allow file-read* (subpath {q(p)}))\n'
 profile+=f'(allow process-exec (literal {q(exe)}))\n(allow file-read* file-map-executable (literal {q(exe)}))\n(allow file-read* file-write* (literal "/dev/null"))\n(allow file-read* (literal "/dev/urandom"))\n'
 for n in ('home','config','cache','data','tmp'): profile+=f'(allow file-write* (subpath {q(worker/n)}))\n'
 env={'PATH':'/usr/bin:/bin','HOME':str(worker/'home'),'XDG_CONFIG_HOME':str(worker/'config'),'XDG_CACHE_HOME':str(worker/'cache'),'XDG_DATA_HOME':str(worker/'data'),'TMPDIR':str(worker/'tmp')}
 report={'diagnostic_only':True,'model_calls':0,'login_calls':0,'daily_home_inherited':False,'network_allowed':False,'fork_allowed':False,'executable_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'profile_sha256':hashlib.sha256(profile.encode()).hexdigest(),'host':platform.mac_ver()[0],'requests':[]}
 err=root/'stderr.log'
 with err.open('wb') as stderr:
  p=subprocess.Popen(['/usr/bin/sandbox-exec','-p',profile,str(exe),'app-server','--listen','stdio://','--strict-config'],cwd=work,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=stderr,bufsize=0)
  sel=selectors.DefaultSelector();sel.register(p.stdout,selectors.EVENT_READ);buf=b''
  def receive():
   global buf
   end=time.monotonic()+10
   while time.monotonic()<end:
    if b'\n' in buf:
     line,buf=buf.split(b'\n',1);return json.loads(line)
    if sel.select(max(0,end-time.monotonic())):
     b=os.read(p.stdout.fileno(),65536)
     if not b:raise RuntimeError('native stdout closed')
     buf+=b
     if len(buf)>1048576:raise RuntimeError('native frame oversized')
   raise RuntimeError('native handshake timeout')
  def call(n,m,params):
   p.stdin.write(json.dumps({'id':n,'method':m,'params':params}).encode()+b'\n');r=receive()
   if r.get('id')!=n:raise RuntimeError('unexpected native response')
   report['requests'].append(m)
   if 'error' in r: return {'rpc_error':True}
   return r.get('result')
  try:
   r=call(1,'initialize',{'clientInfo':{'name':'fusion_gateway','title':'Fusion Gateway','version':'0.1.0'},'capabilities':{'explicitGatewayOauth':True,'experimentalApi':False}})
   report['initialize']={'user_agent':r.get('userAgent'),'platform_family':r.get('platformFamily'),'platform_os':r.get('platformOs'),'private_codex_home':pathlib.Path(r.get('codexHome','')).is_relative_to(worker)}
   p.stdin.write(b'{"method":"initialized","params":{}}\n')
   r=call(2,'account/read',{'refreshToken':False})
   report['account']={'present':r.get('account') is not None,'requires_openai_auth':r.get('requiresOpenaiAuth'),'rpc_error':r.get('rpc_error',False)}
   report['metadata_handshake_passed']=True
  except Exception as e:
   report['metadata_handshake_passed']=False;report['failure_kind']=type(e).__name__
  finally:
   p.stdin.close()
   try:p.wait(timeout=3)
   except subprocess.TimeoutExpired:
    p.terminate()
    try:p.wait(timeout=3)
    except subprocess.TimeoutExpired:p.kill();p.wait(timeout=3)
   sel.close();p.stdout.close()
   report['exit_code']=p.returncode;report['os_reaped']=True
 report['stderr_sanitized']=err.read_text(errors='replace').replace(str(root),'<diagnostic-root>');report['stderr_bytes']=err.stat().st_size;report['stderr_sha256']=hashlib.sha256(err.read_bytes()).hexdigest()
 out.write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n');print(json.dumps(report,ensure_ascii=False,indent=2))
