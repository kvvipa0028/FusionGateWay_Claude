#!/usr/bin/env python3
"""Enter a GLM key locally; never print it or store it in a checkout."""

import argparse
import getpass
import json
import os
from pathlib import Path
import stat
import subprocess
import sys


class KeyError(RuntimeError):
    pass


def default_root():
    base = os.environ.get("XDG_CONFIG_HOME")
    return (Path(base) if base else Path.home() / ".config") / "fusion-gateway"


def check_directory(path):
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise KeyError("Private directory must be owned by you and have mode 0700")


def check_location(root):
    root = Path(root).absolute()
    if any((parent / ".git").exists() or (parent / ".git").is_symlink() for parent in [root, *root.parents]):
        raise KeyError("Credential storage must be outside a Git checkout")
    if root.exists() or root.is_symlink():
        check_directory(root)
    return root


def validate_key(key):
    if not key or len(key) > 8192 or key.startswith("REPLACE_"):
        raise KeyError("A real, nonempty API key is required")
    if any(ord(char) < 33 or ord(char) > 126 for char in key):
        raise KeyError("API key must contain printable ASCII without whitespace")


def store_key(root, key):
    validate_key(key)
    root = check_location(root)
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    check_directory(root)
    directory = root / "credentials"
    directory.mkdir(mode=0o700, exist_ok=True)
    check_directory(directory)
    path = directory / "glm-coding-plan.key"
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError:
        raise KeyError("Credential file already exists; existing key was preserved") from None
    try:
        with os.fdopen(fd, "w") as stream:
            stream.write(key + "\n")
            stream.flush()
            os.fsync(stream.fileno())
    except BaseException:
        path.unlink(missing_ok=True)
        raise
    return path


def read_key(root):
    root = check_location(root)
    check_directory(root)
    directory = root / "credentials"
    check_directory(directory)
    path = directory / "glm-coding-plan.key"
    before = path.lstat()
    if not stat.S_ISREG(before.st_mode) or before.st_uid != os.getuid() or before.st_mode & 0o077:
        raise KeyError("Credential must be a private regular file owned by you")
    fd = os.open(path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    with os.fdopen(fd, "r") as stream:
        after = os.fstat(stream.fileno())
        if not os.path.samestat(before, after):
            raise KeyError("Credential file changed while opening")
        key = stream.read(8194).strip()
    validate_key(key)
    return key


def status(root):
    root = Path(root).absolute()
    path = root / "credentials/glm-coding-plan.key"
    if not path.exists() and not path.is_symlink():
        return {"configured": False, "credential_file": str(path)}
    read_key(root)
    return {"configured": True, "credential_file": str(path), "live_authentication_verified": False}


def dialog_key():
    if sys.platform != "darwin":
        raise KeyError("Native hidden entry is available on macOS; use terminal entry")
    script = '''set answer to display dialog "请输入中国大陆 GLM Coding Plan API key。仅保存到本机私有文件，不发送到聊天。" default answer "" with hidden answer buttons {"取消", "保存"} default button "保存" cancel button "取消" with title "Fusion · GLM 私有凭据"
return text returned of answer'''
    # Captured in memory only: never forward this stdout to logs or tool output.
    completed = subprocess.run(["/usr/bin/osascript", "-e", script], stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, text=True)
    if completed.returncode:
        raise KeyError("Private key entry was cancelled or unavailable")
    return completed.stdout.rstrip("\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["init", "status"])
    parser.add_argument("--root", type=Path, default=default_root())
    parser.add_argument("--dialog", action="store_true", help="macOS masked dialog")
    args = parser.parse_args(argv)
    try:
        if args.command == "init":
            # Validate the destination before asking for sensitive input.
            root = check_location(args.root)
            if (root / "credentials/glm-coding-plan.key").exists():
                raise KeyError("Credential already exists; use status to check it")
            key = dialog_key() if args.dialog else getpass.getpass("GLM Coding Plan API key (hidden): ")
            path = store_key(root, key)
            print(json.dumps({"saved": True, "credential_file": str(path)}))
        else:
            print(json.dumps(status(args.root)))
        return 0
    except (KeyError, OSError, UnicodeError):
        # Keep even OS/parser exception details out of credential-entry output.
        print("Private credential operation refused; check path, permissions and input", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
