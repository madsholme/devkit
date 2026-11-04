#!/usr/bin/env python3
"""Exercise real dashboard -> CLI -> Git using disposable local origins only."""
import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


def main():
    binary = os.environ.get('DEVKIT_GIT_BINARY', str(Path(__file__).resolve().parents[1] / 'bin/devkit'))
    with tempfile.TemporaryDirectory(prefix='devkit-git-http-') as tmp:
        parent = Path(tmp)
        projects = parent / 'projects'
        projects.mkdir()
        repo = projects / 'app'
        repo.mkdir()
        config = parent / 'config'
        config.mkdir()
        fakebin = parent / 'bin'
        fakebin.mkdir()
        # The web server's background housekeeping must never see real Docker state.
        for name in ['docker', 'virsh', 'tailscale']:
            tool = fakebin / name
            tool.write_text('#!/bin/sh\nexit 0\n')
            tool.chmod(0o755)
        env = dict(os.environ, DEVKIT_CONFIG_DIR=str(config), DEVKIT_STATE_DIR=str(parent/'state'),
                   GIT_CONFIG_GLOBAL='/dev/null', GIT_CONFIG_NOSYSTEM='1', PATH=str(fakebin)+':'+os.environ['PATH'])
        def git(root, *args):
            return subprocess.check_output(['git', '-C', str(root), *args], env=env, text=True, stderr=subprocess.PIPE).strip()
        def identity(root):
            git(root, 'config', 'user.name', 'Devkit fixture')
            git(root, 'config', 'user.email', 'fixture@example.invalid')
            git(root, 'config', 'commit.gpgsign', 'false')
        def commit(root, name):
            (root/name).write_text(name)
            git(root, 'add', name)
            git(root, 'commit', '-m', name)
            return git(root, 'rev-parse', 'HEAD')
        git(repo, 'init', '-b', 'main')
        identity(repo)
        initial = commit(repo, 'initial')
        remote = parent / 'origin.git'
        git(parent, 'clone', '--bare', str(repo), str(remote))
        git(repo, 'remote', 'add', 'origin', str(remote))
        git(repo, 'fetch', 'origin')
        git(repo, 'branch', '--set-upstream-to=origin/main')
        peer = parent / 'peer'
        git(parent, 'clone', str(remote), str(peer))
        identity(peer)
        target = commit(peer, 'remote-change')
        git(peer, 'push', 'origin', 'main')
        password = 'temporary-fixture-password'
        salt = os.urandom(16)
        enc = lambda b: base64.urlsafe_b64encode(b).decode().rstrip('=')
        key = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 600000)
        (config/'config.json').write_text(json.dumps({'projects_dir': str(projects), 'prune': {'daily': False},
            'web': {'password_hash': 'pbkdf2-sha256$600000$'+enc(salt)+'$'+enc(key)}}))
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        server = subprocess.Popen([binary, 'web', '--host', '127.0.0.1', '--port', str(port)],
                                  cwd=parent, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        origin = f'http://127.0.0.1:{port}'
        client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        def request(path, payload=None):
            req = urllib.request.Request(origin+path, data=None if payload is None else json.dumps(payload).encode(),
                                         headers={'Origin': origin, 'Content-Type': 'application/json'})
            with client.open(req, timeout=10) as response:
                return json.load(response)
        def action(name, ok=True):
            job = request('/api/action', {'action': 'git-'+name, 'worktree': str(repo)})
            for _ in range(100):
                result = request('/api/job?id='+job['id'])
                if result['done']:
                    assert result['ok'] == ok, result
                    return result
                time.sleep(.05)
            raise AssertionError('Git job did not finish')
        try:
            for _ in range(100):
                try:
                    request('/api/session')
                    break
                except urllib.error.URLError:
                    time.sleep(.05)
            controls = request('/api/projects')['projects'][0]['main']['git_controls']
            assert controls['actions'] == ['fetch', 'sync', 'push'], controls
            try:
                request('/api/action', {'action': 'git-sync', 'worktree': str(repo)})
                raise AssertionError('Anonymous Git action accepted')
            except urllib.error.HTTPError as error:
                assert error.code == 401
            request('/api/login', {'password': password})
            action('fetch')
            assert git(repo, 'rev-parse', 'HEAD') == initial
            action('sync')
            assert git(repo, 'rev-parse', 'HEAD') == target
            target = commit(repo, 'local-change')
            action('push')
            assert git(remote, 'rev-parse', 'main') == target
            (repo/'local-edits').write_text('preserve me')
            result = action('sync', ok=False)
            assert 'local changes' in result['output']
            assert (repo/'local-edits').read_text() == 'preserve me'
            assert git(repo, 'rev-parse', 'HEAD') == target
            events = [json.loads(line) for line in (parent/'state/history.jsonl').read_text().splitlines()]
            assert any(e['action']=='git-push' and e['ok'] and e['worktree']==str(repo) for e in events)
            assert any(e['action']=='git-sync' and not e['ok'] for e in events)
            print('PASS: authenticated HTTP jobs -> CLI -> real local Git fetch/sync/push, dirty refusal, history, anonymous denial')
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()


if __name__ == '__main__':
    main()
