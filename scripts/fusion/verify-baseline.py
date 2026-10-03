#!/usr/bin/env python3
"""Check current untagged fork source without overwriting historical baseline evidence."""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time


def main():
    root = Path(__file__).resolve().parents[2]
    evidence = root / ".fusion-dev/baseline-validation"
    evidence.mkdir(parents=True, exist_ok=True)
    output = evidence / "bin"
    output.mkdir(parents=True, exist_ok=True)
    go = shutil.which("go")
    if not go:
        raise SystemExit("Go 1.26.3 is required on PATH")
    version = subprocess.check_output([go, "version"], text=True).strip()
    if not version.startswith("go version go1.26.3 "):
        raise SystemExit(f"Expected Go 1.26.3, found: {version}")
    lock = json.loads((root / "docs/fusion/upstream-lock.json").read_text())
    for name in ("go.mod", "go.sum"):
        actual = hashlib.sha256((root / name).read_bytes()).hexdigest()
        if actual != lock["files_sha256"][name]:
            raise SystemExit(f"Locked dependency file differs: {name}")
    previous = {}
    saved = evidence / "verification-results.json"
    if "--resume" in sys.argv and saved.exists():
        old = json.loads(saved.read_text())
        if old["upstream_commit"] == lock["commit"] and old["go_version"] == version:
            previous = {check["check"]: check for check in old["checks"]
                        if check["exit_code"] == 0 and
                        check["check"] in ("dependencies", "module-integrity")}
    sdk = subprocess.check_output(["/usr/bin/xcrun", "--show-sdk-path"],
                                  text=True).strip()
    results = []
    with tempfile.TemporaryDirectory(prefix="fusion-baseline-home-") as temp:
        home = Path(temp)
        for name in ("config", "cache", "data", "tmp"):
            (home / name).mkdir()
        # Public dependency/compiler caches are shared; authentication and
        # native client configuration are not inherited.
        env = {
            "PATH": str(Path(go).parent) + os.pathsep + os.environ["PATH"],
            "HOME": str(home),
            "USERPROFILE": str(home),
            "XDG_CONFIG_HOME": str(home / "config"),
            "XDG_CACHE_HOME": str(home / "cache"),
            "XDG_DATA_HOME": str(home / "data"),
            "TMPDIR": str(home / "tmp"),
            "GOENV": "off",
            "GOTOOLCHAIN": "local",
            "GOPROXY": "https://proxy.golang.org,direct",
            "GOSUMDB": "sum.golang.org",
            "GOMODCACHE": str(Path.home() / "go/pkg/mod"),
            "GOCACHE": str(Path.home() / "Library/Caches/go-build"),
            "MAGPIE_NO_STATS": "1",
            "DO_NOT_TRACK": "1",
            "LANG": "en_US.UTF-8",
            "CC": "/usr/bin/clang",
            "CXX": "/usr/bin/clang++",
            "SDKROOT": sdk,
            "CGO_CFLAGS": f"-O2 -g -isysroot {sdk}",
            "CGO_CXXFLAGS": f"-O2 -g -isysroot {sdk}",
            "CGO_LDFLAGS": f"-isysroot {sdk}",
        }
        for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"):
            if name in os.environ:
                env[name] = os.environ[name]
        commands = [
            ("dependencies", ["mod", "download"]),
            ("module-integrity", ["mod", "verify"]),
            ("build-cli", ["build", "-mod=readonly", "-tags", "nogui", "-o",
                           str(output / "untagged-cli"), "."]),
            ("build-gui", ["build", "-mod=readonly", "-o",
                           str(output / "untagged-gui"), "."]),
            ("vet", ["vet", "-mod=readonly", "-tags", "nogui", "./..."]),
            ("targeted-tests", ["test", "-mod=readonly", "-tags", "nogui",
                                "-count=1", "./internal/appdir",
                                "./internal/access", "./internal/provider",
                                "./internal/gateway", "./internal/qoder"]),
        ]
        for name, args in commands:
            if name in previous:
                results.append(previous[name])
                print(f"REUSE {name}: previous exit=0", flush=True)
                continue
            env["CGO_ENABLED"] = "1" if name == "build-gui" else "0"
            command = [go, *args]
            print(f"START {name}", flush=True)
            began = time.monotonic()
            log = evidence / f"{name}.log"
            with log.open("w") as stream:
                completed = subprocess.run(command, cwd=root, env=env,
                                           stdout=stream, stderr=subprocess.STDOUT)
            results.append({
                "check": name, "command": ["go", *args],
                "exit_code": completed.returncode,
                "duration_seconds": round(time.monotonic() - began, 2),
                "log": str(log.relative_to(root)),
                "log_sha256": hashlib.sha256(log.read_bytes()).hexdigest(),
                "cgo_enabled": env["CGO_ENABLED"],
            })
            print(f"END {name}: exit={completed.returncode}", flush=True)
            if completed.returncode:
                print(log.read_text()[-6000:], flush=True)
                break
    report = {
        "source_scope": "current fork source without fusion tag; not the original upstream snapshot or Fusion product",
        "git_head": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
        "upstream_commit": lock["commit"], "go_version": version,
        "go_executable": go, "credential_environment": "allowlist and temporary HOME/XDG",
        "macos_sdk": sdk, "compiler": "/usr/bin/clang",
        "checks": results,
        "not_verified": ["complete upstream test suite", "GUI interaction",
                         "real subscription routes", "Fusion T01-T60", "deployment"],
    }
    (evidence / "verification-results.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    return 0 if len(results) == len(commands) and all(
        result["exit_code"] == 0 for result in results) else 1


if __name__ == "__main__":
    sys.exit(main())
