#!/usr/bin/env python3
"""Prepare or run isolated official device login, without generation admission."""
import argparse
from dataclasses import dataclass
import importlib.util
import json
import os
from pathlib import Path
import pwd
import signal
import stat
import subprocess
import sys

spec = importlib.util.spec_from_file_location("login_publisher_checks", Path(__file__).with_name("verify-runtime-publishers.py"))
publishers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publishers)

SEEDS = {
    "codex": b'cli_auth_credentials_store = "file"\nforced_login_method = "chatgpt"\n',
    "grok": b'auto_update = false\n[grok_com_config]\ndisable_api_key_auth = true\n',
}
SYSTEM_CA = Path("/private/etc/ssl/cert.pem")


@dataclass(frozen=True)
class Layout:
    runtime: str
    root: Path
    home: Path
    config: Path
    credential: Path
    inode: tuple


def user_home():
    return Path(pwd.getpwuid(os.getuid()).pw_dir).resolve(strict=True)


def default_root(runtime):
    return user_home() / ".local/share/fusion-gateway/auth" / runtime


def private_directory(path):
    value = path.lstat()
    if not stat.S_ISDIR(value.st_mode) or value.st_uid != os.getuid() or value.st_mode & 0o077:
        raise ValueError("owned private directory required")
    return (value.st_dev, value.st_ino)


def outside_git(root):
    if root.resolve() != root.absolute():
        raise ValueError("canonical private path required")
    for parent in (root, *root.parents):
        if (parent / ".git").exists() or (parent / ".git").is_symlink():
            raise ValueError("authentication must be outside Git")


def private_file(path, maximum=65536):
    value = path.lstat()
    if not stat.S_ISREG(value.st_mode) or value.st_uid != os.getuid() or value.st_nlink != 1 or value.st_mode & 0o077 or not 0 < value.st_size <= maximum:
        raise ValueError("private regular file required")
    return value


def check_seed(layout):
    outside_git(layout.root)
    if private_directory(layout.root) != layout.inode:
        raise ValueError("authentication root replaced")
    for path in (layout.home, layout.home / ".codex", layout.home / ".grok", *(layout.root / n for n in ("config", "cache", "data", "tmp", "cwd"))):
        private_directory(path)
    before = private_file(layout.config)
    with os.fdopen(os.open(layout.config, os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
        if not os.path.samestat(before, os.fstat(source.fileno())) or source.read(65537) != SEEDS[layout.runtime]:
            raise ValueError("controlled login configuration changed")
    if not os.path.samestat(before, layout.config.lstat()):
        raise ValueError("login configuration replaced")


def prepare(root, runtime):
    if runtime not in SEEDS:
        raise ValueError("unsupported login Runtime")
    root = Path(root).absolute()
    outside_git(root)
    missing = []
    parent = root
    while not parent.exists():
        missing.append(parent);parent = parent.parent
    for path in reversed(missing):
        path.mkdir(mode=0o700)
    identity = private_directory(root)
    home = root / "home"
    for path in (home, *(root / n for n in ("config", "cache", "data", "tmp", "cwd"))):
        path.mkdir(mode=0o700, exist_ok=True);private_directory(path)
    for name in (".codex", ".grok"):
        (home / name).mkdir(mode=0o700, exist_ok=True);private_directory(home / name)
    native_home = home / (".codex" if runtime == "codex" else ".grok")
    layout = Layout(runtime, root, home, native_home / "config.toml", native_home / "auth.json", identity)
    try:
        fd = os.open(layout.config, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    except FileExistsError:
        pass
    else:
        with os.fdopen(fd, "wb") as destination:
            destination.write(SEEDS[runtime]);destination.flush();os.fsync(destination.fileno())
    check_seed(layout)
    return layout


def status(layout):
    check_seed(layout)
    present = layout.credential.exists() or layout.credential.is_symlink()
    if present:
        private_file(layout.credential, 256 << 10)  # Metadata only; never parse tokens/JWT.
    return {"runtime": layout.runtime, "runtime_version": publishers.PINS[layout.runtime].version,
        "private_home_prepared": True, "credential_file_present": present,
        "authenticated": False, "identity_verified": False, "generation_admitted": False,
        "billing_verified": False, "quota_verified": False}


def system_ca_identity(path=SYSTEM_CA):
    if path.resolve(strict=True) != path.absolute():
        raise ValueError("canonical system CA required")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
        before = os.fstat(source.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_nlink != 1 or before.st_mode & 0o022 or not 0 < before.st_size <= 1 << 20:
            raise ValueError("root owned public system CA required")
        header = source.read(4096)
        if b"-----BEGIN CERTIFICATE-----" not in header:
            raise ValueError("public PEM certificate bundle required")
        source.seek(0)
        digest, _ = publishers.digest_stream(source, 1 << 20)
        if before != os.fstat(source.fileno()) or before != path.lstat():
            raise ValueError("system CA changed")
        return (before.st_dev, before.st_ino, before.st_mtime_ns, before.st_ctime_ns, digest)


def environment(layout):
    check_seed(layout)
    env = {"PATH": "/usr/bin:/bin", "LANG": "C", "TZ": "UTC", "TERM": "xterm-256color",
        "HOME": str(layout.home), "CFFIXED_USER_HOME": str(layout.home), "USERPROFILE": str(layout.home),
        "CODEX_HOME": str(layout.home / ".codex"), "GROK_HOME": str(layout.home / ".grok"),
        "DO_NOT_TRACK": "1", "NO_PROXY": "*", "no_proxy": "*"}
    if layout.runtime == "codex":
        system_ca_identity()
        env["SSL_CERT_FILE"] = str(SYSTEM_CA)
    for variable, name in (("XDG_CONFIG_HOME", "config"), ("XDG_CACHE_HOME", "cache"),
            ("XDG_DATA_HOME", "data"), ("TMPDIR", "tmp")):
        env[variable] = str(layout.root / name)
    return env


def native_command(layout, mode):
    if mode not in ("login", "help"):
        raise ValueError("only official login or offline help allowed")
    binary = user_home() / publishers.PINS[layout.runtime].relative_path
    command = [str(binary)]
    if layout.runtime == "codex":
        command += ["-c", 'cli_auth_credentials_store="file"', "-c", 'forced_login_method="chatgpt"']
    command += ["login"]
    if mode == "help":
        command += ["--help"]
    else:
        command += ["--device-auth"]
    return command


def profile(layout, executable, online):
    check_seed(layout)
    q = lambda p: json.dumps(str(p), ensure_ascii=False)
    rules = ['(version 1)', '(deny default)', '(allow signal (target self))',
        '(allow file-read-metadata)', '(allow file-read* (literal "/"))',
        '(allow sysctl-read (sysctl-name-prefix "hw.") (sysctl-name "kern.osrelease") (sysctl-name "kern.osversion") (sysctl-name "kern.ostype") (sysctl-name "kern.bootargs"))',
        f'(allow process-exec (literal {q(executable)}))',
        f'(allow file-read* file-map-executable (literal {q(executable)}))',
        f'(allow file-read* (subpath {q(layout.root)}))',
        '(allow file-read* file-write* (literal "/dev/null") (literal "/dev/tty"))',
        '(allow file-read* (literal "/dev/urandom") (literal "/dev/random"))']
    for path in (layout.home, *(layout.root / name for name in ("config", "cache", "data", "tmp"))):
        rules += [f'(allow file-write* (subpath {q(path)}))']
    for path in ("/System/Library", "/usr/lib", "/Library/Apple/System/Library"):
        rules += [f'(allow file-read* file-map-executable (subpath {q(path)}))']
    for path in ("/usr/share/icu", "/private/var/db/timezone", "/private/etc/ssl"):
        rules += [f'(allow file-read* (subpath {q(path)}))']
    if layout.runtime == "codex":
        rules += ['(allow mach-lookup (global-name "com.apple.cfprefsd.agent") (global-name "com.apple.cfprefsd.daemon") (local-name "com.apple.cfprefsd.agent"))',
            '(allow user-preference-read (preference-domain "com.openai.codex"))',
            '(allow ipc-posix-shm-read* (ipc-posix-name-prefix "apple.cfprefs."))']
    if online:
        rules += ['(allow network-outbound (remote tcp "*:443") (remote udp "*:53") (remote tcp "*:53"))',
            '(allow network-outbound (literal "/private/var/run/mDNSResponder"))',
            '(allow file-read* (literal "/private/etc/resolv.conf") (literal "/private/etc/hosts"))',
            '(allow mach-lookup (global-name "com.apple.SystemConfiguration.configd") (global-name "com.apple.mDNSResponder"))']
    return "\n".join(rules) + "\n"


def check_runtime(layout):
    command = native_command(layout, "help")
    pin = publishers.PINS[layout.runtime]
    before = publishers.local_identity(Path(command[0]), pin)
    publishers.run_check(publishers.signature_command(Path(command[0]), pin), layout.root / "tmp")
    if any(Path(p).exists() for p in ("/etc/grok", "/etc/codex", "/Library/Application Support/Codex")):
        raise ValueError("system policy requires separate isolation review")
    return before


def send_owned_signal(child, number):
    if child.poll() is not None:
        return
    try:
        os.killpg(child.pid, number)
    except ProcessLookupError:
        pass
    except PermissionError:
        # macOS can deny a process-group signal during exit. The profile denies
        # fork, so this precise owned Popen handle is also the whole Worker.
        child.send_signal(number)


def run_native(layout, mode):
    online = mode == "login"
    if online and (not all(stream.isatty() for stream in (sys.stdin, sys.stdout, sys.stderr)) or status(layout)["credential_file_present"]):
        raise ValueError("fresh private login requires attached local terminal")
    before = check_runtime(layout)
    ca_before = system_ca_identity() if layout.runtime == "codex" else None
    argv = native_command(layout, mode)
    child = subprocess.Popen(["/usr/bin/sandbox-exec", "-p", profile(layout, argv[0], online), *argv],
        cwd=layout.root / "cwd", env=environment(layout), start_new_session=True, umask=0o077,
        stdin=None if online else subprocess.DEVNULL,
        stdout=None if online else subprocess.PIPE, stderr=None if online else subprocess.PIPE)
    previous = {}
    def forward(signum, frame):
        send_owned_signal(child, signum)
    try:
        for number in (signal.SIGINT, signal.SIGTERM):
            previous[number] = signal.signal(number, forward)
        output, error = child.communicate(timeout=900 if online else 15)
    finally:
        if child.poll() is None:
            send_owned_signal(child, signal.SIGKILL);child.wait(timeout=5)
        for number, handler in previous.items(): signal.signal(number, handler)
    if child.returncode != 0 or publishers.local_identity(Path(argv[0]), publishers.PINS[layout.runtime]) != before:
        raise ValueError("isolated Native login operation unconfirmed")
    check_seed(layout)
    if ca_before is not None and system_ca_identity() != ca_before:
        raise ValueError("system CA changed during login")
    if online:
        if not status(layout)["credential_file_present"]:
            raise ValueError("private cache not confirmed after login")
        return {"official_login_process_succeeded": True, "identity_verified": False, "generation_admitted": False}
    if len(output) + len(error) > 65536 or b"--device-auth" not in output:
        raise ValueError("pinned offline login help unconfirmed")
    return {"offline_login_help_verified": True, "owned_exit_code": child.returncode,
        "runtime_version": publishers.PINS[layout.runtime].version, "real_model_calls": 0}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["prepare", "status", "check", "login"])
    parser.add_argument("--runtime", choices=["codex", "grok"], required=True)
    args = parser.parse_args(argv)
    try:
        if args.command == "login" and not all(stream.isatty() for stream in (sys.stdin, sys.stdout, sys.stderr)):
            raise ValueError("attached local terminal required")
        root = default_root(args.runtime)
        if args.command != "prepare" and not root.exists():
            raise ValueError("private home not prepared")
        layout = prepare(root, args.runtime)
        result = run_native(layout, "help" if args.command == "check" else "login") if args.command in ("check", "login") else status(layout)
        print(json.dumps(result))
        return 0
    except Exception:
        print("private login operation unconfirmed; existing authentication preserved; no automatic fallback")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
