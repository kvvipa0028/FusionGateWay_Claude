#!/usr/bin/env python3
"""Verify pinned macOS Runtime publishers without login, install or model calls."""
import argparse
from dataclasses import dataclass
from datetime import datetime, timezone
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import ssl
import stat
import subprocess
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request

MAX_BINARY = 512 << 20
MAX_DOWNLOAD = 160 << 20
SIGNING_FINGERPRINT = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"


@dataclass(frozen=True)
class Pin:
    version: str
    relative_path: str
    digest: str
    identifier: str
    team: str
    source: str
    asset: str = ""


PINS = {
    "codex": Pin("0.160.0", ".codex/packages/standalone/releases/0.160.0-aarch64-apple-darwin/bin/codex",
        "112fae7a5a1223e673c8a1791d32338f37df8b527ff1159bb8adac6c4dbf1b4b", "codex", "2DC432GLL2",
        "https://api.github.com/repos/openai/codex/releases/tags/rust-v0.160.0",
        "https://github.com/openai/codex/releases/download/rust-v0.160.0/codex-aarch64-apple-darwin.tar.gz"),
    "grok": Pin("1.0.48", ".grok/downloads/grok-1.0.48-macos-aarch64",
        "1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05", "xai-grok-pager", "5Y6N3AJ54S",
        "https://x.ai/cli/grok-1.0.48-macos-aarch64.gz"),
    "claude": Pin("2.1.287", ".local/share/claude/versions/2.1.287",
        "6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea", "com.anthropic.claude-code", "Q6L2SF6YDW",
        "https://downloads.claude.ai/claude-code-releases/2.1.287/manifest.json"),
}


def digest_stream(stream, maximum):
    digest, total = hashlib.sha256(), 0
    while data := stream.read(min(1 << 20, maximum - total + 1)):
        total += len(data)
        if total > maximum:
            raise ValueError("size limit exceeded")
        digest.update(data)
    return digest.hexdigest(), total


def local_identity(path, pin):
    if path.resolve() != path.absolute():
        raise ValueError("canonical executable required")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as file:
        before = os.fstat(file.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_uid != os.getuid() or before.st_nlink != 1 or before.st_mode & 0o022 or not before.st_mode & 0o100:
            raise ValueError("owned immutable executable required")
        if file.read(4) != b"\xcf\xfa\xed\xfe":
            raise ValueError("arm64 Mach-O required")
        file.seek(0)
        digest, size = digest_stream(file, MAX_BINARY)
        after = os.fstat(file.fileno())
        current = path.lstat()
        if before != after or after != current or digest != pin.digest:
            raise ValueError("executable changed or wrong pin")
        return (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns, digest)


def signature_command(path, pin):
    requirement = (f'=anchor apple generic and identifier "{pin.identifier}" '
        f'and certificate leaf[subject.OU] = "{pin.team}" '
        'and certificate leaf[field.1.2.840.113635.100.6.1.13] exists')
    return ["/usr/bin/codesign", "--verify", "--strict", "-R", requirement, str(path)]


def strict_json(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate manifest field")
            result[key] = value
        return result
    return json.loads(raw.decode("utf-8"), object_pairs_hook=unique)


def claude_checksum(raw, pin, size):
    doc = strict_json(raw)
    entry = doc.get("platforms", {}).get("darwin-arm64", {})
    if doc.get("version") != pin.version or entry.get("binary") != "claude" or type(entry.get("size")) is not int or entry["size"] != size or entry.get("checksum") != pin.digest:
        raise ValueError("manifest does not match installed version")
    return entry["checksum"]


def codex_asset(raw):
    doc = strict_json(raw)
    assets = [a for a in doc.get("assets", []) if a.get("name") == "codex-aarch64-apple-darwin.tar.gz"]
    if doc.get("tag_name") != "rust-v0.160.0" or doc.get("draft") is not False or doc.get("prerelease") is not False or len(assets) != 1:
        raise ValueError("exact stable release required")
    asset = assets[0]
    digest, size = asset.get("digest"), asset.get("size")
    if asset.get("browser_download_url") != PINS["codex"].asset or not isinstance(digest, str) or not digest.startswith("sha256:") or len(digest) != 71 or any(c not in "0123456789abcdef" for c in digest[7:]) or type(size) is not int or not 0 < size <= MAX_DOWNLOAD:
        raise ValueError("owned release asset checksum required")
    return digest[7:], size


def codex_binary_hash(stream):
    matches, digest, expanded = 0, None, 0
    with tarfile.open(fileobj=stream, mode="r|gz") as archive:
        for count, member in enumerate(archive, start=1):
            path = PurePosixPath(member.name)
            expanded += member.size
            if count > 256 or expanded > MAX_BINARY or path.is_absolute() or ".." in path.parts or not member.isfile():
                raise ValueError("unsafe release archive")
            if member.name == "codex-aarch64-apple-darwin":
                matches += 1
                with archive.extractfile(member) as binary:
                    digest, _ = digest_stream(binary, MAX_BINARY)
    if matches != 1:
        raise ValueError("one exact archive binary required")
    return digest


def grok_binary_hash(stream):
    with gzip.GzipFile(fileobj=stream) as binary:
        digest, _ = digest_stream(binary, MAX_BINARY)
    return digest


def check_url(url, runtime):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.username or parsed.password or parsed.port not in (None, 443) or parsed.fragment:
        raise ValueError("official HTTPS required")
    host, path = parsed.hostname, parsed.path
    allowed = False
    if runtime == "claude":
        allowed = host == "downloads.claude.ai" and path in (
            "/keys/claude-code.asc", "/claude-code-releases/2.1.287/manifest.json",
            "/claude-code-releases/2.1.287/manifest.json.sig") and not parsed.query
    elif runtime == "grok":
        allowed = ((host == "x.ai" and path == "/cli/grok-1.0.48-macos-aarch64.gz") or
            (host == "storage.googleapis.com" and path == "/grok-build-public-artifacts/cli/grok-1.0.48-macos-aarch64.gz")) and not parsed.query
    elif runtime == "codex":
        allowed = (url in (PINS["codex"].source, PINS["codex"].asset) or
            (host == "release-assets.githubusercontent.com" and path.startswith("/github-production-release-asset/")))
    if not allowed:
        raise ValueError("foreign release destination")


class PublicReleaseClient:
    def __init__(self, runtime):
        self.runtime, self.deadline, self.observations = runtime, time.monotonic() + 300, []
        outer = self

        class Redirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, request, response, code, message, headers, newurl):
                check_url(newurl, outer.runtime)
                return super().redirect_request(request, response, code, message, headers, newurl)

        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), Redirect(),
            urllib.request.HTTPSHandler(context=ssl.create_default_context()))

    def fetch(self, url, maximum):
        check_url(url, self.runtime)
        request = urllib.request.Request(url, headers={"User-Agent": "FusionGateway-publisher-verification/1"})
        output, digest, total = tempfile.TemporaryFile(), hashlib.sha256(), 0
        try:
            remaining = self.deadline - time.monotonic()
            if remaining <= 0:
                raise ValueError("verification deadline exceeded")
            with self.opener.open(request, timeout=min(30, remaining)) as response:
                check_url(response.url, self.runtime)
                if response.status != 200:
                    raise ValueError("complete public artifact required")
                length = response.headers.get("Content-Length")
                if length is not None and (not length.isdecimal() or int(length) > maximum):
                    raise ValueError("invalid public artifact size")
                while data := response.read(min(1 << 20, maximum - total + 1)):
                    total += len(data)
                    if total > maximum or time.monotonic() > self.deadline:
                        raise ValueError("public artifact limit exceeded")
                    output.write(data);digest.update(data)
                if length is not None and total != int(length):
                    raise ValueError("incomplete public artifact")
                self.observations.append({"source": url, "final_host": urllib.parse.urlsplit(response.url).hostname,
                    "bytes": total, "sha256": digest.hexdigest()})
            output.seek(0)
            return output
        except Exception:
            output.close()
            raise


def run_check(argv, root):
    env = {"HOME": str(root), "GNUPGHOME": str(root), "PATH": "/usr/bin:/bin", "LANG": "C", "TZ": "UTC"}
    result = subprocess.run(argv, env=env, stdin=subprocess.DEVNULL,
        capture_output=True, timeout=30)
    if result.returncode != 0 or len(result.stdout) + len(result.stderr) > 65536:
        raise ValueError("publisher signature unconfirmed")
    return result.stdout.decode("utf-8", errors="strict")


def verify_manifest_signature(manifest, signature, key, root):
    gpg = Path("/opt/homebrew/bin/gpg").resolve(strict=True)
    args = [str(gpg), "--no-options", "--batch", "--no-autostart", "--homedir", str(root)]
    for name, content in (("manifest.json", manifest), ("manifest.json.sig", signature), ("key.asc", key)):
        (root / name).write_bytes(content)
    listing = run_check(args + ["--with-colons", "--import-options", "show-only", "--import", str(root / "key.asc")], root)
    fingerprints = [line.split(":")[9] for line in listing.splitlines() if line.startswith("fpr:")]
    if not fingerprints or fingerprints[0] != SIGNING_FINGERPRINT:
        raise ValueError("official signing fingerprint mismatch")
    run_check(args + ["--import", str(root / "key.asc")], root)
    status = run_check(args + ["--no-auto-key-retrieve", "--status-fd", "1", "--verify",
        str(root / "manifest.json.sig"), str(root / "manifest.json")], root)
    valid = [line.split() for line in status.splitlines() if line.startswith("[GNUPG:] VALIDSIG ")]
    if len(valid) != 1 or SIGNING_FINGERPRINT not in (valid[0][2], valid[0][-1]):
        raise ValueError("official manifest signature missing")


def verify_runtime(runtime):
    pin = PINS[runtime]
    path = Path.home() / pin.relative_path
    original = local_identity(path, pin)
    client = PublicReleaseClient(runtime)
    with tempfile.TemporaryDirectory(prefix="fusion-publisher-") as temporary:
        root = Path(temporary).resolve()
        root.chmod(0o700)
        run_check(signature_command(path, pin), root)
        with client.fetch(pin.source, (2 << 20) if runtime != "grok" else MAX_DOWNLOAD) as source:
            if runtime == "claude":
                manifest = source.read()
                claude_checksum(manifest, pin, original[2])
                with client.fetch(pin.source + ".sig", 16384) as sig, client.fetch("https://downloads.claude.ai/keys/claude-code.asc", 65536) as key:
                    verify_manifest_signature(manifest, sig.read(), key.read(), root)
            elif runtime == "codex":
                expected, size = codex_asset(source.read())
                with client.fetch(pin.asset, MAX_DOWNLOAD) as archive:
                    digest, actual_size = digest_stream(archive, MAX_DOWNLOAD)
                    if digest != expected or actual_size != size:
                        raise ValueError("release archive checksum mismatch")
                    archive.seek(0)
                    if codex_binary_hash(archive) != pin.digest:
                        raise ValueError("installed binary differs from official archive")
            elif grok_binary_hash(source) != pin.digest:
                raise ValueError("installed binary differs from official download")
        if local_identity(path, pin) != original:
            raise ValueError("installed binary replaced during proof")
        run_check(signature_command(path, pin), root)
    return {"runtime": runtime, "version": pin.version, "platform": "darwin-arm64",
        "executable_sha256": pin.digest, "identifier": pin.identifier, "team_identifier": pin.team,
        "publisher_verified": True, "apple_developer_id_requirement_verified": True,
        "official_release_bytes_verified": True, "manifest_signing_fingerprint": SIGNING_FINGERPRINT if runtime == "claude" else None,
        "observed_at": datetime.now(timezone.utc).isoformat(), "public_sources": client.observations,
        "authentication_inspected": False, "generation_admitted": False, "billing_verified": False,
        "quota_verified": False, "real_model_calls": 0, "temporary_files_removed": True}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runtime", choices=[*PINS, "all"], required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        if args.report.exists() or args.report.is_symlink() or not args.report.parent.is_dir():
            raise ValueError("fresh report destination required")
        if platform.system() != "Darwin" or platform.machine() != "arm64":
            raise ValueError("unsupported verification platform")
        results = [verify_runtime(name) for name in (PINS if args.runtime == "all" else [args.runtime])]
        with args.report.open("x", encoding="utf-8") as output:
            output.write(json.dumps({"schema_version": 1, "component": "WP-17-PUBLISHER-01",
                "observation_only": True, "registry_admission_granted": False, "runtimes": results}, indent=2) + "\n")
        print("pinned publisher proof verified; no authentication, installation or model calls")
        return 0
    except Exception:
        print("publisher proof unconfirmed; no automatic retry or admission")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
