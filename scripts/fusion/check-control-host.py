#!/usr/bin/env python3
"""Verify the fusion-control CLI with private temporary state and no real credentials."""
import argparse
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import selectors
import shutil
import signal
import stat
import subprocess
import tempfile
import sys
import urllib.error
import urllib.request


def main():
    repo = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=repo / '.fusion-dev/fusion-gateway-cli')
    parser.add_argument('--go', default=shutil.which('go'))
    args = parser.parse_args()
    spec = importlib.util.spec_from_file_location('fusion_run_dev', Path(__file__).with_name('run-dev.py'))
    launcher = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(launcher)
    process = None
    def stop_process():
        nonlocal process
        if process is not None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate(timeout=5)
            process = None
    try:
        if not args.go or not launcher.fusion_binary(args.binary, args.go):
            raise ValueError('fusion binary required')
        with contextlib.ExitStack() as cleanup:
            temp = cleanup.enter_context(tempfile.TemporaryDirectory(prefix='fusion-control-smoke-'))
            cleanup.callback(stop_process)
            base = Path(temp).resolve()
            workspace = base / 'workspace'
            workspace.mkdir(mode=0o700)
            private = base / 'private'
            private.mkdir(mode=0o700)
            config = private / 'config'
            config.mkdir(mode=0o700)
            source = config / 'projects.json'
            document = {'schema_version': 1, 'revision': 1, 'global': {}, 'routes': [],
                        'projects': [{'id': 'synthetic-project', 'name': '合成项目', 'path': str(workspace),
                                      'read': True, 'write': False, 'routes': [], 'layer': {}}]}
            source.write_text(json.dumps(document, ensure_ascii=False))
            source.chmod(0o600)
            root = base / 'state'
            env = launcher.environment(root)
            env.update({'GOENV': 'off', 'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off'})
            process = subprocess.Popen([sys.executable, str(Path(__file__).with_name('run-dev.py')),
                                        '--root', str(root), '--binary', str(args.binary.resolve()),
                                        '--go', args.go, '--', 'fusion-control', '--projects', str(source)],
                                       env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, text=True, start_new_session=True)
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                if not selector.select(15):
                    raise ValueError('startup deadline')
                announcement = process.stdout.readline()
            if not announcement:
                raise ValueError("startup produced no announcement: exit=" + str(process.poll()))
            started = json.loads(announcement)
            if started.get('execution_enabled') is not False or started.get('jev') != 'off':
                raise ValueError('execution opened')
            address = started['control_address']
            token_file = root / 'data/fusion-gateway/control/management.token'
            token = token_file.read_text().strip()
            info = token_file.stat()
            if stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1 or token in announcement:
                raise ValueError('private capability boundary')
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

            def request(path, secret, body=None):
                headers = {'Authorization': 'Bearer ' + secret}
                if body is not None:
                    headers['Content-Type'] = 'application/json'
                req = urllib.request.Request(address + path, data=body, headers=headers)
                try:
                    with opener.open(req, timeout=3) as response:
                        return response.status, response.read()
                except urllib.error.HTTPError as response:
                    return response.code, response.read()

            path = '/control/v1/projects/synthetic-project/configuration'
            if request(path, 'wrong')[0] != 401:
                raise ValueError('authentication')
            code, data = request(path, token)
            if code != 200 or str(workspace).encode() in data or token.encode() in data:
                raise ValueError('configuration boundary')
            body = json.dumps({'project_id': 'synthetic-project', 'goal': 'synthetic goal',
                               'required_roles': ['design']}).encode()
            if request('/control/v1/tasks/preview', token, body)[0] != 422:
                raise ValueError('unselected model generated a plan')
            if request('/v1/messages', token)[0] != 404:
                raise ValueError('legacy exit')
            document['revision'] = 2
            source.write_text(json.dumps(document, ensure_ascii=False))
            if request(path, token)[0] != 503:
                raise ValueError('stale registration')
            process.send_signal(signal.SIGTERM)
            output, error = process.communicate(timeout=5)
            if process.returncode != 0 or token in output or token in error:
                raise ValueError('shutdown or secret output')
            process = None
        print(json.dumps({'actual_cli': True, 'authenticated_configuration': True,
                          'unselected_preview_rejected': True, 'legacy_exit_closed': True,
                          'stale_source_rejected': True, 'sigterm_exit': 0,
                          'private_token_not_output': True, 'real_model_calls': 0,
                          'real_quota_queries': 0, 'jev': 'off'}, indent=2))
        return 0
    except (OSError, ValueError, KeyError, subprocess.TimeoutExpired, json.JSONDecodeError) as error:
        reason = str(error) if type(error) is ValueError else type(error).__name__
        print('Fusion control CLI verification failed: ' + reason, file=os.sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
