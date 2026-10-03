#!/usr/bin/env python3
"""Build isolated Fusion binaries without inheriting authentication or SDK overrides."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def main():
    repo = Path(__file__).resolve().parents[2]
    go = shutil.which("go")
    if not go or not subprocess.check_output([go,"version"],text=True).startswith("go version go1.26.3 "):
        raise SystemExit("Go 1.26.3 is required")
    sdk = subprocess.check_output(["/usr/bin/xcrun","--show-sdk-path"],text=True).strip()
    output = repo / ".fusion-dev"
    logs = output / "build-dev"
    logs.mkdir(parents=True,exist_ok=True)
    checks = []
    with tempfile.TemporaryDirectory(prefix="fusion-dev-build-") as temp:
        env = {"PATH":str(Path(go).parent)+":/usr/bin:/bin:/usr/sbin:/sbin", "HOME":temp,
               "XDG_CONFIG_HOME":temp+"/config", "XDG_CACHE_HOME":temp+"/cache", "XDG_DATA_HOME":temp+"/data",
               "TMPDIR":temp, "GOENV":"off", "GOTOOLCHAIN":"local", "GOPROXY":"https://proxy.golang.org,direct",
               "GOSUMDB":"sum.golang.org", "GOMODCACHE":str(Path.home()/"go/pkg/mod"),
               "GOCACHE":str(Path.home()/"Library/Caches/go-build"), "CC":"/usr/bin/clang", "CXX":"/usr/bin/clang++",
               "SDKROOT":sdk, "CGO_CFLAGS":"-O2 -g -isysroot "+sdk, "CGO_CXXFLAGS":"-O2 -g -isysroot "+sdk,
               "CGO_LDFLAGS":"-isysroot "+sdk, "MAGPIE_NO_STATS":"1", "DO_NOT_TRACK":"1"}
        for name,args,cgo in [
            ("cli",["build","-mod=readonly","-tags","fusion,nogui","-o",str(output/"fusion-gateway-cli"),"."],"0"),
            ("gui",["build","-mod=readonly","-tags","fusion","-o",str(output/"fusion-gateway-gui"),"."],"1"),
            ("vet",["vet","-mod=readonly","-tags","fusion,nogui","./..."],"0"),
        ]:
            env["CGO_ENABLED"] = cgo
            log = logs / (name+".log")
            with log.open("w") as stream:
                result = subprocess.run([go,*args],cwd=repo,env=env,stdout=stream,stderr=subprocess.STDOUT)
            checks.append({"check":name,"command":["go",*args],"exit_code":result.returncode,
                           "log":str(log.relative_to(repo)),"log_sha256":hashlib.sha256(log.read_bytes()).hexdigest()})
            print(f"{name}: exit={result.returncode}",flush=True)
            if result.returncode:
                break
    report = {"go_version":"1.26.3", "platform":"macOS/arm64 pilot", "credential_environment":"allowlist and temporary HOME/XDG",
              "checks":checks, "artifacts_sha256":{p.name:hashlib.sha256(p.read_bytes()).hexdigest()
                for p in [output/"fusion-gateway-cli",output/"fusion-gateway-gui"] if p.is_file()}}
    (logs/"results.json").write_text(json.dumps(report,indent=2)+"\n")
    return 0 if len(checks)==3 and all(c["exit_code"]==0 for c in checks) else 1


if __name__ == "__main__":
    raise SystemExit(main())
