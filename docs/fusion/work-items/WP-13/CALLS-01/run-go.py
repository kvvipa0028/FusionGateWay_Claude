import os,sys,subprocess,tempfile,shutil
from pathlib import Path
go=shutil.which('go');sdk=subprocess.check_output(['/usr/bin/xcrun','--show-sdk-path'],text=True).strip()
with tempfile.TemporaryDirectory(prefix='fusion-check-') as temp:
 env={'PATH':str(Path(go).parent)+':/usr/bin:/bin','HOME':temp,'XDG_CONFIG_HOME':temp+'/config','XDG_CACHE_HOME':temp+'/cache','XDG_DATA_HOME':temp+'/data','TMPDIR':temp,'GOENV':'off','GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GOMODCACHE':str(Path.home()/'go/pkg/mod'),'GOCACHE':str(Path.home()/'Library/Caches/go-build'),'CGO_ENABLED':'1','CC':'/usr/bin/clang','CXX':'/usr/bin/clang++','SDKROOT':sdk,'CGO_CFLAGS':'-O2 -g -isysroot '+sdk,'CGO_CXXFLAGS':'-O2 -g -isysroot '+sdk,'CGO_LDFLAGS':'-isysroot '+sdk,'MAGPIE_NO_STATS':'1','DO_NOT_TRACK':'1'}
 with open(sys.argv[1],'w') as out:r=subprocess.run([go,*sys.argv[2:]],env=env,stdout=out,stderr=subprocess.STDOUT)
 print('exit',r.returncode);print(Path(sys.argv[1]).read_text()[-3500:]);sys.exit(r.returncode)
