#!/usr/bin/env python3
"""Run a tool-free GLM connection diagnostic through isolated Claude Code."""

import argparse
from datetime import datetime, timezone
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import tempfile


spec = importlib.util.spec_from_file_location("private_glm_key", Path(__file__).with_name("private-glm-key.py"))
credentials = importlib.util.module_from_spec(spec)
spec.loader.exec_module(credentials)

ENDPOINT = "https://open.bigmodel.cn/api/anthropic"


def probe(root, model, executable, timeout=60):
    if not re.fullmatch(r"glm-[a-z0-9]+(?:[.-][a-z0-9]+)*", model) or not 0 < timeout <= 120:
        raise ValueError("Explicit GLM model ID and bounded timeout are required")
    key = credentials.read_key(root)
    report = {"checked_at": datetime.now(timezone.utc).isoformat(), "endpoint": ENDPOINT,
              "requested_model": model, "connection_verified": False, "billing_verified": False,
              "plan_tier": "not_verified", "quota": "unknown", "upstream_reported_model": "unknown",
              "strict_locked_verified": False, "diagnostic_only": True}
    with tempfile.TemporaryDirectory(prefix="fusion-glm-probe-") as temp:
        home = Path(temp)
        project = home / "project"
        project.mkdir(mode=0o700)
        env = {"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "HOME": temp, "USERPROFILE": temp,
               "XDG_CONFIG_HOME": str(home / "xdg-config"), "XDG_CACHE_HOME": str(home / "xdg-cache"),
               "XDG_DATA_HOME": str(home / "xdg-data"), "TMPDIR": temp, "LANG": "en_US.UTF-8",
               "CLAUDE_CONFIG_DIR": str(home / "claude"), "ANTHROPIC_BASE_URL": ENDPOINT,
               "ANTHROPIC_AUTH_TOKEN": key, "ANTHROPIC_API_KEY": key, "ANTHROPIC_MODEL": model,
               "DISABLE_UPDATES": "1", "DISABLE_TELEMETRY": "1", "DISABLE_ERROR_REPORTING": "1",
               "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "DISABLE_COMPACT": "1",
               "DO_NOT_TRACK": "1", "API_TIMEOUT_MS": "45000"}
        # v2.1.287 --bare requires API-key auth. The same GLM key is supplied
        # in both supported auth variables; local native characterization
        # confirms Bearer and X-Api-Key are sent to the selected endpoint.
        command = [str(executable), "--bare", "--restricted", "--strict-mcp-config",
                   "--setting-sources", "", "--tools", "", "--disable-slash-commands", "--no-chrome",
                   "--no-session-persistence", "--permission-mode", "dontAsk", "--model", model,
                   "--system-prompt", "You are running a connection diagnostic. Follow the user instruction.",
                   "-p", "--output-format", "json", "Reply with exactly FUSION_GLM_OK. Do not use tools."]
        child = subprocess.Popen(command, cwd=project, env=env, stdin=subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                 start_new_session=True)
        try:
            stdout, _ = child.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            # Terminate the process group, including any native descendants.
            try:
                os.killpg(child.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                child.communicate(timeout=2)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(child.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                child.communicate()
            return {**report, "status": "timeout"}
        report["exit_code"] = child.returncode
        try:
            result = json.loads(stdout)
        except (json.JSONDecodeError, UnicodeError):
            return {**report, "status": "invalid_native_result"}
        if not isinstance(result, dict):
            return {**report, "status": "invalid_native_result"}
        status = result.get("api_error_status")
        if type(status) is int and 100 <= status <= 599:
            report["api_error_status"] = status
        if child.returncode or result.get("is_error") is not False or result.get("subtype") != "success":
            return {**report, "status": "native_error"}
        reply = result.get("result")
        if result.get("type") != "result" or result.get("num_turns") != 1 or not isinstance(reply, str) or reply.strip() != "FUSION_GLM_OK":
            return {**report, "status": "unexpected_reply"}
        models = result.get("modelUsage")
        if not isinstance(models, dict) or set(models) != {model}:
            return {**report, "status": "native_model_mismatch"}
        report["native_reported_models"] = [model]
        usage = models[model]
        if isinstance(usage, dict):
            report["native_usage"] = {name: usage[name] for name in ("inputTokens", "outputTokens")
                                      if type(usage.get(name)) is int and 0 <= usage[name] <= 10**9}
        # Native costUSD/provider labels are estimates, not Coding Plan
        # entitlement, remaining quota or upstream billing evidence.
        return {**report, "status": "pass", "connection_verified": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", required=True, help="Explicit model ID for this diagnostic only")
    parser.add_argument("--root", type=Path, default=credentials.default_root())
    parser.add_argument("--claude", type=Path, default=Path.home() / ".local/bin/claude")
    parser.add_argument("--timeout", type=float, default=60)
    args = parser.parse_args()
    try:
        result = probe(args.root, args.model, args.claude, args.timeout)
    except (credentials.KeyError, OSError, ValueError, UnicodeError):
        # Do not echo exception strings: providers or local errors may contain credentials.
        result = {"status": "local_preflight_refused", "connection_verified": False}
    print(json.dumps(result, ensure_ascii=False))
    return 0 if result["connection_verified"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
