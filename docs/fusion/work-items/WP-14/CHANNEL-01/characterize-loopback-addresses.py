from pathlib import Path
import tempfile, subprocess, socket, threading, json, hashlib

SOURCE = '''#include <sys/socket.h>
#include <arpa/inet.h>
#include <unistd.h>
#include <stdlib.h>
int main(int argc,char**argv){if(argc!=3)return 2;int family=argv[1][0]==':'?AF_INET6:AF_INET;int fd=socket(family,SOCK_STREAM,0);int result=-1;if(family==AF_INET){struct sockaddr_in a={0};a.sin_family=family;a.sin_port=htons(atoi(argv[2]));inet_pton(family,argv[1],&a.sin_addr);result=connect(fd,(struct sockaddr*)&a,sizeof(a));}else{struct sockaddr_in6 a={0};a.sin6_family=family;a.sin6_port=htons(atoi(argv[2]));inet_pton(family,argv[1],&a.sin6_addr);result=connect(fd,(struct sockaddr*)&a,sizeof(a));}close(fd);return result==0?0:1;}
'''
sockets=[];threads=[]
def listen(host,port):
 family=socket.AF_INET6 if ':' in host else socket.AF_INET
 s=socket.socket(family,socket.SOCK_STREAM)
 if family==socket.AF_INET6:s.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
 try:s.bind((host,port))
 except Exception:s.close();raise
 s.listen();s.settimeout(.1);sockets.append(s)
 def accept():
  while True:
   try:c,_=s.accept();c.close()
   except socket.timeout:continue
   except OSError:return
 th=threading.Thread(target=accept,daemon=True);th.start();threads.append(th)
 return s.getsockname()[1]
try:
 port=listen('127.0.0.1',0);listen('::1',port);other=listen('127.0.0.1',0)
 alias=socket.socket(socket.AF_INET,socket.SOCK_STREAM)
 try:
  alias.bind(('127.0.0.2',0));alias_status='available on this host; not tested or counted as a sandbox negative'
 except OSError as error:
  alias_status='unavailable on this host: bind '+__import__('errno').errorcode.get(error.errno,'unknown')+', not counted as a sandbox negative'
 finally:alias.close()
 cases=[('127.0.0.1',port),('::1',port),('127.0.0.1',other)]
 with tempfile.TemporaryDirectory(prefix='fusion-loopback-addresses-') as tmp:
  p=Path(tmp);(p/'probe.c').write_text(SOURCE);exe=p/'probe'
  sdk=subprocess.check_output(['/usr/bin/xcrun','--show-sdk-path'],text=True).strip()
  env={'PATH':'/usr/bin:/bin','HOME':tmp,'TMPDIR':tmp,'SDKROOT':sdk}
  subprocess.run(['/usr/bin/clang','-isysroot',sdk,'-Wall','-Wextra','-Werror',str(p/'probe.c'),'-o',str(exe)],env=env,check=True,capture_output=True)
  profile=f'(version 1)(allow default)(deny network-outbound)(allow network-outbound (remote tcp "localhost:{port}"))'
  reports=[]
  for host,value in cases:
   control=subprocess.run([str(exe),host,str(value)],env={},capture_output=True,timeout=3)
   tested=subprocess.run(['/usr/bin/sandbox-exec','-p',profile,str(exe),host,str(value)],env={},capture_output=True,timeout=3)
   reports.append({'address':host,'approved_port':value==port,'positive_control_exit':control.returncode,'sandbox_exit':tested.returncode})
  report={'diagnostic_only':True,'real_model_calls':0,'profile':'allow default; deny network-outbound; only remote tcp localhost at one owned port','cases':reports,'compiler_exit':0,'probe_sha256':hashlib.sha256(exe.read_bytes()).hexdigest(),'alias_127_0_0_2':alias_status}
  output=Path('.fusion-dev/implementation/wp14-loopback-addresses.json');output.parent.mkdir(parents=True,exist_ok=True)
  output.write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,indent=2))
  if any(r['positive_control_exit']!=0 or r['sandbox_exit']!=(0 if r['approved_port'] else 1) for r in reports):raise SystemExit('loopback matching differs from admitted host')
finally:
 for s in sockets:s.close()
 for t in threads:t.join(timeout=.3)
