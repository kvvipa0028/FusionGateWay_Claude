#!/usr/bin/env python3
"""Explicit single live CN quota read through the product CLI; no model calls."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import tempfile
import urllib.request


def probe(binary, go, key_file):
    repo = Path(__file__).resolve().parents[2]
    spec = importlib.util.spec_from_file_location("fusion_private_launcher", repo / "scripts/fusion/run-dev.py")
    launcher = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(launcher)
    if not launcher.fusion_binary(binary, go):
        raise ValueError("explicit fusion-tag product binary required")
    with tempfile.TemporaryDirectory(prefix="fusion-glm-quota-host-") as temporary:
        root = Path(temporary).resolve()
        os.chmod(root, 0o700)
        config, project = root / "config", root / "project"
        config.mkdir(mode=0o700)
        project.mkdir(mode=0o700)
        (project / "README.md").write_text("Synthetic private query-only pilot.\n")
        source = config / "projects.json"
        document = {"schema_version": 1, "revision": 1, "global": {}, "routes": [
            {"id": "live-glm-query-only", "revision": 1, "native_route": "glm-cn-claude",
             "model": "query-only-declaration", "account": "local-account-unverified",
             "workspace": "local-workspace-unverified", "credential_identity": "explicit-query-file",
             "runtime_version": "query-only-declaration", "no_effort": True}], "projects": [
            {"id": "private-query-pilot", "name": "Quota-only synthetic pilot", "path": str(project),
             "read": True, "write": False, "routes": [{"id": "live-glm-query-only", "revision": 1}],
             "layer": {}}]}
        source.write_text(json.dumps(document))
        os.chmod(source, 0o600)
        state = root / "state"
        environment = launcher.environment(state)
        process = subprocess.Popen([str(binary), "fusion-control", "--projects", str(source),
            "--glm-quota-project", "private-query-pilot", "--glm-quota-route", "live-glm-query-only",
            "--glm-quota-key", str(key_file)], env=environment, stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        result = None
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                if not selector.select(10):
                    raise ValueError("owned product startup not confirmed")
                line = process.stdout.readline(8193)
            if len(line) > 8192:
                raise ValueError("product startup record unavailable")
            announcement = json.loads(line)
            address = announcement.get("control_address", "")
            if not address.startswith("http://127.0.0.1:") or announcement.get("execution_enabled") is not False or announcement.get("quota_query_enabled") is not True or announcement.get("jev") != "off":
                raise ValueError("product query-only mode not confirmed")
            token = (state / "data/fusion-gateway/control/management.token").read_text().strip()
            client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            base = "/control/v1/projects/private-query-pilot/quota"

            def request(method, path):
                req = urllib.request.Request(address + path, data=b"{}" if method == "POST" else None,
                    method=method, headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
                with client.open(req, timeout=15) as response:
                    raw = response.read((128 << 10) + 1)
                    if len(raw) > 128 << 10:
                        raise ValueError("product reply too large")
                    return json.loads(raw)

            before = request("GET", base)
            if len(before.get("routes", [])) != 1 or before["routes"][0].get("status") != "unknown" or before["routes"][0].get("snapshot") is not None:
                raise ValueError("initial quota cache unexpectedly populated")
            refreshed = request("POST", base + "/live-glm-query-only/refresh")  # exactly one
            row = refreshed["routes"][0]
            snapshot = row.get("snapshot")
            if row.get("error") or not snapshot or row.get("status") != "unverified" or snapshot.get("complete") is not False or snapshot.get("pool", {}).get("verified") is not False:
                raise ValueError("live quota query failed or promoted admission")
            after = request("GET", base)
            if after["routes"][0].get("snapshot") != snapshot:
                raise ValueError("cache GET changed source observation")
            configuration = request("GET", "/control/v1/projects/private-query-pilot/configuration")
            if any(route.get("admitted") or route.get("billing_known") for route in configuration["configuration"]["routes"]):
                raise ValueError("quota query granted generation")
            windows = [{"kind": w.get("kind"), "used_percent": w.get("used_percent")}
                       for w in snapshot.get("windows", []) if w.get("kind") == "coding_plan"]
            if not windows:
                raise ValueError("live model quota windows not observed")
            result = {"component": "WP-15-GLM-QUOTA-HOST-01", "product_cli": "fusion-control",
                "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                "query_endpoint": "https://open.bigmodel.cn/api/monitor/usage/quota/limit",
                "explicit_refresh_requests": 1, "real_model_calls": 0, "execution_enabled": False,
                "quota_status": row["status"], "pool_verified": False, "complete": False,
                "observed_at": snapshot.get("observed_at"), "received_at": snapshot.get("received_at"),
                "model_windows": windows, "cache_GET_kept_original_snapshot": True,
                "route_admitted": False, "billing_known": False, "jev": "off"}
        finally:
            if process.poll() is None:
                process.send_signal(signal.SIGTERM)
            try:
                process.communicate(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.communicate(timeout=5)
                raise ValueError("owned product did not exit after SIGTERM") from None
        if process.returncode != 0:
            raise ValueError("owned product shutdown failed")
        result["owned_exit_code"] = process.returncode
        result["private_fixture_cleanup"] = True
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--go", required=True)
    parser.add_argument("--key-file", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    try:
        # Refuse an existing report before any credential registration/query.
        if args.report.exists() or args.report.is_symlink() or not args.report.parent.is_dir():
            raise ValueError("fresh report destination required")
        report = probe(args.binary.resolve(), args.go, args.key_file)  # preserve symlink refusal
        with args.report.open("x") as output:
            output.write(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
        print("single live product quota read verified; no model calls; owned host exited and private fixture removed")
        return 0
    except Exception:
        # Never disclose HTTP bodies, private paths, token, key or exception text.
        print("product quota probe unconfirmed; no automatic retry")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
