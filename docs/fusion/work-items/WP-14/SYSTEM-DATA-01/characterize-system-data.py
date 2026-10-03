#!/usr/bin/env python3
"""Pinned Native startup ablation with synthetic credentials and loopback only.

This diagnostic does not launch through Supervisor or admit a production route.
It never reads the user's API key, account configuration, or project files.
"""

import hashlib
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading

PIN = "6eab8333fe2121553100d8f40bfada384a3e989b94f947e18ba6677a6fcb41ea"
KEY = "fixture-only.private-glm-key"
EXE = Path.home() / ".local/share/claude/versions/2.1.287"


def digest(value):
    return hashlib.sha256(value).hexdigest()


def run_case(name, data_paths):
    calls = []

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_POST(self):
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size < 1048576:
                self.send_error(400)
                return
            body = json.loads(self.rfile.read(size))
            call = {"path": self.path, "model": body.get("model"),
                    "tool_count": len(body.get("tools", [])),
                    "bearer_fixture": self.headers.get("Authorization") == "Bearer " + KEY,
                    "api_key_fixture": self.headers.get("X-Api-Key") == KEY}
            calls.append(call)
            if (call["path"] != "/api/anthropic/v1/messages?beta=true"
                    or call["model"] != "glm-5.3" or call["tool_count"] != 0
                    or not call["bearer_fixture"] or not call["api_key_fixture"]):
                self.send_error(400)
                return
            message = {"id": "fixture-message", "type": "message", "role": "assistant",
                       "model": "glm-5.3", "content": [], "stop_reason": None,
                       "stop_sequence": None, "usage": {"input_tokens": 8, "output_tokens": 0}}
            events = [
                ("message_start", {"type": "message_start", "message": message}),
                ("content_block_start", {"type": "content_block_start", "index": 0,
                                         "content_block": {"type": "text", "text": ""}}),
                ("content_block_delta", {"type": "content_block_delta", "index": 0,
                                         "delta": {"type": "text_delta", "text": "FUSION_FIXTURE_OK"}}),
                ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                ("message_delta", {"type": "message_delta", "delta": {"stop_reason": "end_turn",
                                    "stop_sequence": None}, "usage": {"output_tokens": 8}}),
                ("message_stop", {"type": "message_stop"}),
            ]
            payload = "".join("event: " + kind + "\ndata: " + json.dumps(value) + "\n\n"
                              for kind, value in events).encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with tempfile.TemporaryDirectory(prefix="fusion-system-data-") as temp:
            root = Path(temp).resolve()
            worker, project = root / "worker", root / "project"
            for path in (worker, project):
                path.mkdir(mode=0o700)
            for part in ("home", "config", "cache", "data", "tmp"):
                (worker / part).mkdir(mode=0o700)
            quote = lambda path: json.dumps(str(path))
            profile = ('(version 1)\n(deny default)\n(allow signal (target self))\n'
                       '(allow sysctl-read (sysctl-name-prefix "hw.") (sysctl-name "kern.osrelease") '
                       '(sysctl-name "kern.osversion") (sysctl-name "kern.ostype") '
                       '(sysctl-name "kern.bootargs"))\n'
                       '(allow file-read-metadata)\n(allow file-read* (literal "/"))\n')
            for path in ("/System/Library", "/usr/lib", "/Library/Apple/System/Library"):
                profile += f"(allow file-read* file-map-executable (subpath {quote(path)}))\n"
            for path in data_paths:
                profile += f"(allow file-read* (subpath {quote(path)}))\n"
            for path in (worker, project):
                profile += f"(allow file-read* (subpath {quote(path)}))\n"
            profile += (f"(allow process-exec (literal {quote(EXE)}))\n"
                        f"(allow file-read* file-map-executable (literal {quote(EXE)}))\n"
                        '(allow file-read* file-write* (literal "/dev/null"))\n'
                        '(allow file-read* (literal "/dev/urandom"))\n')
            for part in ("home", "config", "cache", "data", "tmp"):
                profile += f"(allow file-write* (subpath {quote(worker / part)}))\n"
            profile += f'(allow network-outbound (remote tcp "localhost:{server.server_port}"))\n'
            env = {"PATH": "/usr/bin:/bin", "HOME": str(worker / "home"),
                   "USERPROFILE": str(worker / "home"), "XDG_CONFIG_HOME": str(worker / "config"),
                   "XDG_CACHE_HOME": str(worker / "cache"), "XDG_DATA_HOME": str(worker / "data"),
                   "TMPDIR": str(worker / "tmp"), "CLAUDE_CONFIG_DIR": str(worker / "config/claude"),
                   "CLAUDE_CODE_TMPDIR": str(worker / "tmp"), "ANTHROPIC_API_KEY": KEY,
                   "ANTHROPIC_AUTH_TOKEN": KEY,
                   "ANTHROPIC_BASE_URL": f"http://127.0.0.1:{server.server_port}/api/anthropic",
                   "ANTHROPIC_MODEL": "glm-5.3", "DISABLE_UPDATES": "1", "DISABLE_TELEMETRY": "1",
                   "DISABLE_ERROR_REPORTING": "1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
                   "DISABLE_COMPACT": "1", "DO_NOT_TRACK": "1", "API_TIMEOUT_MS": "5000"}
            args = ["--bare", "--restricted", "--strict-mcp-config", "--setting-sources", "",
                    "--tools", "", "--disable-slash-commands", "--no-chrome", "--no-session-persistence",
                    "--permission-mode", "dontAsk", "--model", "glm-5.3", "--system-prompt",
                    "You are a tool-free synthetic connection fixture.", "-p", "--output-format",
                    "stream-json", "--verbose", "--include-partial-messages", "Reply FUSION_FIXTURE_OK."]
            process = subprocess.Popen(["/usr/bin/sandbox-exec", "-p", profile, str(EXE), *args],
                                       cwd=project, env=env, stdin=subprocess.DEVNULL,
                                       stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            timed_out = False
            try:
                stdout, stderr = process.communicate(timeout=12)
            except subprocess.TimeoutExpired:
                timed_out = True
                process.terminate()
                try:
                    stdout, stderr = process.communicate(timeout=2)
                except subprocess.TimeoutExpired:
                    process.kill()
                    stdout, stderr = process.communicate(timeout=2)
            terminal = [json.loads(line) for line in stdout.splitlines()]
            results = [frame for frame in terminal if frame.get("type") == "result"]
            success = (process.returncode == 0 and len(results) == 1
                       and results[0].get("subtype") == "success"
                       and results[0].get("result") == "FUSION_FIXTURE_OK" and len(calls) == 1)
            return {"case": name, "data_read_paths": data_paths, "timeout": timed_out,
                    "exit_code": process.returncode, "native_result_verified": success,
                    "parent_wait_completed": True, "supervisor_stop_proof": False,
                    "fork_allowed": False, "network_scope": "synthetic localhost port only",
                    "profile_sha256": digest(profile.encode()), "calls": calls,
                    "stdout_bytes": len(stdout), "stderr_bytes": len(stderr),
                    "stdout_sha256": digest(stdout), "stderr_sha256": digest(stderr),
                    "event_types": [{"type": frame.get("type"), "subtype": frame.get("subtype")}
                                    for frame in terminal]}
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def main():
    if not EXE.is_file() or digest(EXE.read_bytes()) != PIN:
        raise SystemExit("Pinned Claude Code 2.1.287 executable unavailable or changed")
    cases = [("baseline", []), ("icu_only", ["/usr/share/icu"]),
             ("timezone_only", ["/private/var/db/timezone"]),
             ("both", ["/usr/share/icu", "/private/var/db/timezone"])]
    reports = [run_case(name, paths) for name, paths in cases]
    verified = all(case["timeout"] and not case["calls"] for case in reports[:3]) and reports[3]["native_result_verified"]
    report = {"diagnostic_only": True, "fixture_only": True, "actual_account_used": False,
              "real_model_calls": 0, "strict_lock_verified": False, "billing_verified": False,
              "quota_verified": False, "runtime_version": "2.1.287", "executable_sha256": PIN,
              "ablation_verified": verified, "cases": reports}
    output = Path(__file__).with_name("native-system-data.json")
    output.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps({"report": str(output), "ablation_verified": verified, "real_model_calls": 0}))
    return 0 if verified else 1


if __name__ == "__main__":
    raise SystemExit(main())
