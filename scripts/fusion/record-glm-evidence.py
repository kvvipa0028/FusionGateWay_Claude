#!/usr/bin/env python3
"""Record the private GLM generation-admission evidence file.

Runs three real checks against one declared GLM Coding Plan route:
1. the tool-free native probe (one minimal generation request, exact reply);
2. the pinned Claude Code publisher verification (offline file/signature);
3. one read-only CN quota GET proving a billed Coding Plan window exists.

The output file is a private 0600 certificate consumed by
bootstrap.LoadGLMEvidence. It never contains the key, cookies or account
secrets. A failed or rate-limited step refuses to write anything.
"""
import argparse
from datetime import datetime, timezone
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import socket
import stat
import sys
import urllib.parse
import urllib.request

HERE = Path(__file__).resolve().parent


def load_module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


probe_module = load_module("glm_probe", "glm-claude-probe.py")
credentials = load_module("private_glm_key", "private-glm-key.py")
publishers = load_module("publisher_checks", "verify-runtime-publishers.py")

KIND = "fusion-glm-generation-evidence"
TRANSPORT = "glm-cn-claude-coding-plan"
QUOTA_ENDPOINT = "https://open.bigmodel.cn/api/monitor/usage/quota/limit"


def publisher_verified(claude_path):
    pin = publishers.PINS["claude"]
    path = Path(claude_path).resolve(strict=True)
    if path != (Path.home() / pin.relative_path).resolve(strict=True):
        return False, pin.version
    before = publishers.local_identity(path, pin)
    publishers.run_check(publishers.signature_command(path, pin), Path.home() / ".cache")
    after = publishers.local_identity(path, pin)
    return before is not None and before == after, pin.version


def checked_quota_url():
    """Assert the one fixed public CN endpoint and public resolved addresses."""
    parsed = urllib.parse.urlparse(QUOTA_ENDPOINT)
    if parsed.scheme != "https" or parsed.hostname != "open.bigmodel.cn" or parsed.query or parsed.fragment or parsed.username or parsed.password:
        raise ValueError("quota endpoint mismatch")
    for _, _, _, _, sockaddr in socket.getaddrinfo(parsed.hostname, 443):
        address = ipaddress.ip_address(sockaddr[0])
        if address.is_private or address.is_loopback or address.is_link_local or address.is_unspecified or address.is_reserved:
            raise ValueError("quota endpoint resolves to a non-public address")
    return QUOTA_ENDPOINT


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def quota_windows(key):
    endpoint = checked_quota_url()
    opener = urllib.request.build_opener(NoRedirect)
    request = urllib.request.Request(endpoint, method="GET",
        headers={"Authorization": key, "Accept": "application/json", "Cache-Control": "no-cache"})
    with opener.open(request, timeout=30) as response:
        if response.status != 200 or response.geturl() != endpoint:
            raise ValueError("quota read failed")
        raw = response.read(65536 + 1)
        if len(raw) > 65536 or bytes(key.encode()) in raw:
            raise ValueError("quota read malformed")
        doc = json.loads(raw)
    if doc.get("success") is not True or not isinstance(doc.get("data"), dict):
        raise ValueError("quota envelope malformed")
    limits = doc["data"].get("limits")
    if not isinstance(limits, list) or not limits:
        raise ValueError("no quota windows")
    windows = []
    for limit in limits:
        if not isinstance(limit, dict):
            continue
        kind = limit.get("type")
        if not isinstance(kind, str):
            continue
        if kind.upper() == "TOKENS_LIMIT":
            used = limit.get("percentage")
            if not isinstance(used, (int, float)) or not 0 <= used <= 100:
                used = None
            windows.append({"kind": "coding_plan", "unit": "percent",
                            "used_percent": float(used) if used is not None else None})
    return windows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, help="exact declared GLM model ID")
    parser.add_argument("--claude", default=str(Path.home() / publishers.PINS["claude"].relative_path))
    parser.add_argument("--output", type=Path,
                        default=credentials.default_root() / "evidence" / "glm-generation-evidence.json")
    parser.add_argument("--timeout", type=float, default=120)
    parser.add_argument("--replace", action="store_true", help="overwrite an existing evidence file")
    args = parser.parse_args()

    output = args.output
    if output.exists() and not args.replace:
        print(json.dumps({"status": "refused_existing_evidence"}))
        return 1
    report = probe_module.probe(credentials.default_root(), args.model, args.claude, args.timeout)
    if report.get("status") != "pass" or not report.get("connection_verified"):
        print(json.dumps({"status": "probe_" + str(report.get("status")), "recorded": False}))
        return 1
    publisher_ok, version = publisher_verified(args.claude)
    if not publisher_ok:
        print(json.dumps({"status": "publisher_unverified", "recorded": False}))
        return 1
    key = credentials.read_key(credentials.default_root())
    try:
        windows = quota_windows(key)
    except Exception:
        # The quota step supplies the billing evidence; nothing is recorded.
        print(json.dumps({"status": "quota_read_failed", "recorded": False}))
        return 1
    if not any(w["used_percent"] is not None for w in windows):
        print(json.dumps({"status": "quota_windows_incomplete", "recorded": False}))
        return 1
    evidence = {
        "schema_version": 1,
        "kind": KIND,
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "endpoint": probe_module.ENDPOINT,
        "requested_model": args.model,
        "probe_status": "pass",
        "connection_verified": True,
        "native_reported_models": report.get("native_reported_models", []),
        "publisher_runtime_version": version,
        "publisher_verified": True,
        "quota_windows": windows,
        "transport_id": TRANSPORT,
    }
    output.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if output.parent.stat().st_mode & 0o077:
        print(json.dumps({"status": "evidence_dir_not_private", "recorded": False}))
        return 1
    temp = output.with_name(".evidence.tmp")
    if temp.exists():
        temp.unlink()
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "w") as handle:
        handle.write(json.dumps(evidence, ensure_ascii=False, indent=1) + "\n")
        handle.flush()
        os.fsync(handle.fileno())
    os.replace(temp, output)
    os.chmod(output, 0o600)
    if not stat.S_ISREG(output.lstat().st_mode) or output.lstat().st_mode & 0o077:
        print(json.dumps({"status": "evidence_file_not_private", "recorded": False}))
        return 1
    print(json.dumps({"status": "recorded", "path": str(output),
                      "model": args.model, "windows": len(windows),
                      "publisher_version": version}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
