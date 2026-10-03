#!/usr/bin/env python3
"""Launch a fusion-tag binary with private state and an isolated client HOME."""

import argparse
import importlib.util
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys


spec = importlib.util.spec_from_file_location("private_glm_key", Path(__file__).with_name("private-glm-key.py"))
credentials = importlib.util.module_from_spec(spec)
spec.loader.exec_module(credentials)


def environment(root):
    root = credentials.check_location(root)
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    credentials.check_directory(root)
    env = {"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "LANG": "en_US.UTF-8",
           "FUSION_STATE_ROOT": str(root), "MAGPIE_NO_STATS": "1", "DO_NOT_TRACK": "1"}
    for key, folder in {"HOME":"runtime-home", "XDG_CONFIG_HOME":"config", "XDG_CACHE_HOME":"cache",
                        "XDG_DATA_HOME":"data", "TMPDIR":"tmp"}.items():
        path = root / folder
        path.mkdir(mode=0o700, exist_ok=True)
        credentials.check_directory(path)
        env[key] = str(path)
    env["USERPROFILE"] = env["HOME"]
    for name in ("config", "cache", "data"):
        path = root / name / "fusion-gateway"
        path.mkdir(mode=0o700, exist_ok=True)
        credentials.check_directory(path)
    return env


def fusion_binary(binary, go):
    result = subprocess.run([str(go), "version", "-m", str(binary)], stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=5)
    if result.returncode:
        return False
    matches = re.findall(r"^\s*build\s+-tags=(\S+)\s*$", result.stdout, re.MULTILINE)
    return any("fusion" in tags.strip('"').split(",") for tags in matches)


def main():
    repo = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=credentials.default_root())
    parser.add_argument("--binary", type=Path, default=repo / ".fusion-dev/fusion-gateway-cli")
    parser.add_argument("--go", default=shutil.which("go"))
    parser.add_argument("arguments", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    try:
        if not args.go or not args.binary.is_file() or not fusion_binary(args.binary, args.go):
            raise ValueError("Expected an explicit fusion-tag binary")
        env = environment(args.root)
        arguments = args.arguments[1:] if args.arguments[:1] == ["--"] else args.arguments
        process = subprocess.Popen([str(args.binary), *arguments], env=env)
        previous = {}
        def forward(signum, frame):
            if process.poll() is None:
                try:
                    process.send_signal(signum)
                except ProcessLookupError:
                    pass
        try:
            for signum in (signal.SIGINT, signal.SIGTERM):
                previous[signum] = signal.signal(signum, forward)
            return process.wait()
        finally:
            for signum, handler in previous.items():
                signal.signal(signum, handler)
    except (credentials.KeyError, OSError, ValueError, subprocess.TimeoutExpired):
        print("Fusion launch refused; check fusion build tag and private state permissions", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
