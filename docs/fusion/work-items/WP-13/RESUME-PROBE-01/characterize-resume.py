#!/usr/bin/env python3
"""Characterize the pinned Native CLI; synthetic requests, no production admission.

The assertions describe observed CLI behavior, including unsafe gaps. They do
not authorize Fusion resume. The sandbox is a diagnostic profile, not the
production Supervisor profile. Temporary source/session data is deleted.
"""
import argparse, hashlib, json, os, pathlib, subprocess, tempfile, threading, uuid, http.server, shutil, platform, urllib.parse
PIN = '1ed292eb62206b1a2ec3d17dc69c9c8406a07f5ff414305f953baee5b72a4a05'
TOKEN = 'fixture-resume-no-real-credential'
A = 'FUSION_RESUME_OWNED_A'
B = 'FUSION_RESUME_DISTRACTOR_B'
NEXT = 'FUSION_RESUME_NEXT'
TOOL = 'FUSION_RESUME_TOOL'
request_read = False
parser = argparse.ArgumentParser()
parser.add_argument('--cli', required=True)
parser.add_argument('--result', required=True)
args = parser.parse_args()
assert platform.system() == 'Darwin' and platform.machine() == 'arm64', 'macOS arm64 required'
assert pathlib.Path(args.cli).is_absolute(), 'explicit absolute Native path required'
exe = pathlib.Path(args.cli).resolve(strict=True)
assert hashlib.sha256(exe.read_bytes()).hexdigest() == PIN, 'fixed executable changed'
records = []
lock = threading.Lock()

class Handler(http.server.BaseHTTPRequestHandler):

    def log_message(self, *args):
        pass

    def do_POST(self):
        n = int(self.headers.get('Content-Length', '0'))
        if self.path != '/v1/chat/completions' or not 0 < n <= 1 << 20 or self.headers.get('Authorization') != 'Bearer ' + TOKEN:
            self.send_error(400)
            return
        raw = self.rfile.read(n)
        req = json.loads(raw)
        text = json.dumps(req)
        tools = [x.get('function', {}).get('name') for x in req.get('tools', [])]
        with lock:
            records.append({'model': req.get('model'), 'main': 'read_file' in tools, 'marker_a': A in text, 'marker_b': B in text, 'marker_next': NEXT in text, 'tool_history': any((x.get('role') == 'tool' for x in req.get('messages', []))), 'cwd_original': str(root / 'project') in text, 'cwd_other': str(root / 'other-project') in text, 'assistant_history': any((x.get('role') == 'assistant' for x in req.get('messages', []))), 'message_roles': [x.get('role') for x in req.get('messages', [])], 'request_bytes': len(raw)})
        global request_read
        model = req['model']
        events = [{'id': 'fixture-response', 'object': 'chat.completion.chunk', 'created': 1780000000, 'model': model, 'choices': [{'index': 0, 'delta': {'role': 'assistant', 'content': 'Fixture ready.'}, 'finish_reason': None}]}, {'id': 'fixture-response', 'object': 'chat.completion.chunk', 'created': 1780000000, 'model': model, 'choices': [{'index': 0, 'delta': {}, 'finish_reason': 'stop'}], 'usage': {'prompt_tokens': 3, 'completion_tokens': 2, 'total_tokens': 5}}]
        if request_read and 'read_file' in tools:
            request_read = False
            events[0]['choices'][0]['delta'] = {'role': 'assistant', 'content': None, 'tool_calls': [{'index': 0, 'id': 'call_resume_read', 'type': 'function', 'function': {'name': 'read_file', 'arguments': json.dumps({'target_file': str(root / 'project/fixture.txt')})}}]}
            events[1]['choices'][0]['finish_reason'] = 'tool_calls'
        body = (''.join(('data: ' + json.dumps(x) + '\n\n' for x in events)) + 'data: [DONE]\n\n').encode()
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
report = {'version': '1.0.48', 'executable_sha256': PIN, 'real_model_calls': 0, 'real_quota_calls': 0, 'fixture_only': True, 'production_resume_enabled': False, 'checks': {}, 'umask': '0077', 'profile': 'isolated diagnostic; root writable and broad Mach lookup except securityd; not production Supervisor', 'actual_wait': True, 'stop_proof_verified': False, 'model_binding_verified': False, 'account_binding_verified': False, 'cwd_binding_verified': False}
try:
    with tempfile.TemporaryDirectory(prefix='fusion-grok-resume-') as temp:
        root = pathlib.Path(temp).resolve()
        os.chmod(root, 448)
        for name in ('grok', 'project', 'other-project', 'config', 'cache', 'data', 'tmp'):
            (root / name).mkdir(mode=448)
        config = ''
        for (name, model) in [('fixture', 'fixture-model'), ('other', 'fixture-other-model')]:
            config += f'[model.{name}]\nmodel = {json.dumps(model)}\nbase_url = "http://127.0.0.1:{server.server_port}/v1"\napi_key = "{TOKEN}"\nmax_retries = 0\n'
        config += '[models]\ndefault = "fixture"\nsession_summary = "fixture"\n[features]\nturn_summary = false\n[cli]\nauto_update = false\n'
        cfg = root / 'grok/config.toml'
        cfg.write_text(config)
        os.chmod(cfg, 384)
        q = lambda p: json.dumps(str(p))
        profile = '(version 1)\n(deny default)\n(allow signal (target self))\n(allow sysctl-read)\n(allow mach-lookup)\n(deny mach-lookup (global-name "com.apple.securityd"))\n(allow file-read-metadata)\n(allow file-read* (literal "/"))\n'
        for p in ['/System', '/usr/lib', '/usr/share', '/Library/Apple', '/private/var/db/timezone', root]:
            profile += f'(allow file-read* file-map-executable (subpath {q(p)}))\n'
        profile += f'(allow process-exec (literal {q(exe)}))\n(allow file-read* file-map-executable (literal {q(exe)}))\n(allow file-write* (subpath {q(root)}))\n(allow file-read* file-write* (literal "/dev/null"))\n(allow file-read* (literal "/dev/urandom"))\n(allow network-outbound (remote tcp "localhost:{server.server_port}"))\n'
        env = {'PATH': '/usr/bin:/bin', 'HOME': str(root), 'GROK_HOME': str(root / 'grok'), 'XDG_CONFIG_HOME': str(root / 'config'), 'XDG_CACHE_HOME': str(root / 'cache'), 'XDG_DATA_HOME': str(root / 'data'), 'TMPDIR': str(root / 'tmp'), 'LANG': 'en_US.UTF-8', 'DO_NOT_TRACK': '1', 'RUST_LOG': 'error'}
        sid_a = str(uuid.uuid4())
        sid_b = str(uuid.uuid4())
        sid_c = str(uuid.uuid4())
        owned = root / 'project/fixture.txt'
        owned.write_text('FUSION_RESUME_OWNED_READ\nline2\n')
        os.chmod(owned, 384)
        source_before = hashlib.sha256(owned.read_bytes()).hexdigest()
        def project_snapshot():
            return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() and not p.is_symlink() else 'directory' if p.is_dir() and not p.is_symlink() else 'unsupported' for name in ('project', 'other-project') for p in (root / name).rglob('*')}
        project_before = project_snapshot()

        def run(name, extra, prompt, cwd='project', model='fixture', home='grok', turns='1'):
            before = len(records)
            cmd = ['/usr/bin/sandbox-exec', '-p', profile, str(exe), '--single', prompt, '--model', model, '--output-format', 'streaming-json', '--permission-mode', 'dontAsk', '--no-subagents', '--max-turns', turns, '--tools', 'Read', '--disallowed-tools', 'search_tool,use_tool', '--disable-web-search', '--no-auto-update', '--cwd', str(root / cwd), *extra]
            runenv = dict(env)
            runenv['GROK_HOME'] = str(root / home)
            proc = subprocess.Popen(cmd, cwd=root / cwd, env=runenv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True, umask=63)
            try:
                (out, err) = proc.communicate(timeout=20)
            except subprocess.TimeoutExpired:
                import signal
                os.killpg(proc.pid, signal.SIGKILL)
                proc.communicate()
                raise RuntimeError('isolated Native deadline')
            assert len(out) <= 64 << 10 and len(err) <= 64 << 10 and (TOKEN.encode() not in out + err), 'output bound/reflection'
            frames = []
            for line in out.splitlines():
                try:
                    frames.append(json.loads(line))
                except ValueError:
                    pass
            end = [x for x in frames if x.get('type') == 'end']
            result = {'exit_code': proc.returncode, 'frame_types': [x.get('type') for x in frames], 'end_count': len(end), 'same_session_a': any((x.get('sessionId') == sid_a for x in end)), 'same_session_b': any((x.get('sessionId') == sid_b for x in end)), 'same_session_c': any((x.get('sessionId') == sid_c for x in end)), 'stdout_bytes': len(out), 'stderr_bytes': len(err), 'http': records[before:]}
            if end:
                result['end_fields'] = sorted(end[-1])
                result['model_usage'] = list(end[-1].get('modelUsage', {}))
                result['end_num_turns'] = end[-1].get('num_turns')
                result['end_usage'] = end[-1].get('usage')
                result['end_model_usage'] = end[-1].get('modelUsage')
            report['checks'][name] = result
            print(name, json.dumps(result), flush=True)
        run('new_a', ['--session-id', sid_a], A)
        files = []
        for path in sorted((root / 'grok').rglob('*')):
            if path.is_file():
                entry = {'path': str(path.relative_to(root)).replace(sid_a, '<session-a>').replace(urllib.parse.quote(str(root / 'project'), safe=''), '<encoded-cwd>'), 'bytes': path.stat().st_size, 'mode': oct(path.stat().st_mode & 511)}
                if path.suffix == '.json':
                    obj = json.loads(path.read_text())
                    entry['keys'] = sorted(obj) if isinstance(obj, dict) else 'array'
                if '/sessions/' in '/' + entry['path']:
                    files.append(entry)
        report['initial_disk'] = files
        report['summary_info_keys'] = sorted(json.loads(next((root / 'grok/sessions').glob('*/' + sid_a + '/summary.json')).read_text())['info'])
        print('disk', json.dumps(files), flush=True)
        run('new_b', ['--session-id', sid_b], B)
        run('latest_without_id', ['--resume'], NEXT)
        run('exact_a', ['--resume', sid_a], NEXT)
        session_a = next((root / 'grok/sessions').glob('*/' + sid_a))

        def archive(name):
            home = root / name
            home.mkdir(mode=448)
            copy = home / 'sessions' / session_a.parent.name / sid_a
            shutil.copytree(session_a, copy)
            cfg = home / 'config.toml'
            cfg.write_text(config)
            os.chmod(cfg, 384)
            return copy
        archive('archive-grok')
        run('copied_session_only', ['--resume', sid_a], NEXT, home='archive-grok')
        incomplete = archive('incomplete-grok')
        (incomplete / 'chat_history.jsonl').unlink()
        run('missing_chat_history', ['--resume', sid_a], NEXT, home='incomplete-grok')
        incomplete_pair = archive('incomplete-pair-grok')
        for name in ('chat_history.jsonl', 'updates.jsonl'):
            (incomplete_pair / name).unlink()
        run('missing_transcript_pair', ['--resume', sid_a], NEXT, home='incomplete-pair-grok')
        broken = archive('broken-summary-grok')
        (broken / 'summary.json').write_text('{}')
        run('broken_summary', ['--resume', sid_a], NEXT, home='broken-summary-grok')
        run('missing_uuid', ['--resume', str(uuid.uuid4())], NEXT)
        run('duplicate_new_uuid', ['--session-id', sid_a], NEXT)
        run('resume_plus_session_id', ['--resume', sid_a, '--session-id', str(uuid.uuid4())], NEXT)
        run('wrong_cwd', ['--resume', sid_a], NEXT, cwd='other-project')
        run('model_override', ['--resume', sid_a], NEXT, model='other')
        request_read = True
        run('new_read_session', ['--session-id', sid_c], TOOL, turns='3')
        run('resume_read_history', ['--resume', sid_c], NEXT, turns='3')
        report['readonly_source_unchanged'] = hashlib.sha256(owned.read_bytes()).hexdigest() == source_before and project_snapshot() == project_before
        report['private_config_mode'] = oct((root / 'grok/config.toml').stat().st_mode & 511)
        assert report['readonly_source_unchanged']
finally:
    server.shutdown()
    server.server_close()
    thread.join()
checks = report['checks']

def success(name, session, calls, model='fixture-model'):
    c = checks[name]
    assert c['exit_code'] == 0 and c['end_count'] == 1 and c['same_session_' + session], name + ' terminal mismatch'
    assert len(c['http']) == calls and c['model_usage'] == [model], name + ' model/calls mismatch'
for (name, session, calls) in [('new_a', 'a', 2), ('new_b', 'b', 2), ('latest_without_id', 'b', 1), ('exact_a', 'a', 1), ('copied_session_only', 'a', 1), ('missing_chat_history', 'a', 1), ('missing_transcript_pair', 'a', 1), ('wrong_cwd', 'a', 1), ('new_read_session', 'c', 3), ('resume_read_history', 'c', 1)]:
    success(name, session, calls)
success('model_override', 'a', 1, 'fixture-other-model')
for name in ['broken_summary', 'missing_uuid', 'duplicate_new_uuid', 'resume_plus_session_id']:
    c = checks[name]
    assert c['exit_code'] != 0 and (not c['http']) and (not c['end_count']), name + ' failed-open'
for name in ['exact_a', 'copied_session_only', 'missing_chat_history', 'wrong_cwd']:
    c = checks[name]['http'][0]
    assert c['main'] and c['marker_a'] and (not c['marker_b']) and c['assistant_history'], name + ' history mismatch'
c = checks['missing_transcript_pair']['http'][0]
assert c['main'] and (not c['marker_a']) and (not c['assistant_history']), 'missing transcript was not characterized'
c = checks['latest_without_id']['http'][0]
assert c['marker_b'] and (not c['marker_a']), 'implicit selection mismatch'
c = checks['wrong_cwd']['http'][0]
assert c['cwd_original'] and (not c['cwd_other']), 'restored cwd metadata mismatch'
c = checks['model_override']['http'][0]
assert c['model'] == 'fixture-other-model', 'override was not characterized'
assert checks['resume_read_history']['http'][0]['tool_history'], 'historical tool result absent'
assert 'tool_call' not in checks['resume_read_history']['frame_types'], 'historical tool replay changed'
assert report['readonly_source_unchanged'] and report['private_config_mode'] == '0o600'
assert all((x['mode'] == '0o600' for x in report['initial_disk'])), 'private fixture umask boundary'
report['observed_behavior_checks'] = 'pass'
report['native_scenarios'] = len(checks)
report['synthetic_http_calls'] = sum((len(c['http']) for c in checks.values()))
assert TOKEN not in json.dumps(report), 'report contains fixture credential'
result = pathlib.Path(args.result)
with result.open('x') as out:
    json.dump(report, out, indent=2)
    out.write('\n')
print('observed behavior checks pass; synthetic HTTP', report['synthetic_http_calls'], 'Native scenarios', report['native_scenarios'], 'production Resume disabled', flush=True)
