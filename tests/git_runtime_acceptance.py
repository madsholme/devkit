#!/usr/bin/env python3
"""Exercise real dashboard -> CLI -> Git creation and sync using disposable projects only."""
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
import urllib.parse
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
        global_config = parent / 'gitconfig'
        global_config.write_text('[user]\n name = Devkit fixture\n email = fixture@example.invalid\n')
        env = dict(os.environ, DEVKIT_CONFIG_DIR=str(config), DEVKIT_STATE_DIR=str(parent/'state'),
                   GIT_CONFIG_GLOBAL=str(global_config), GIT_CONFIG_NOSYSTEM='1', PATH=str(fakebin)+':'+os.environ['PATH'])
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
        def action(name, ok=True, payload=None):
            job = request('/api/action', payload or {'action': 'git-'+name, 'worktree': str(repo)})
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
            notes = 'Build a small Go app.\nLiteral: $(touch NEVER) <script>text</script>'
            payload = {'action': 'project-create', 'name': 'new-app',
                       'remote': 'git@example.invalid:team/new-app.git', 'agent_notes': notes}
            result = action('create', payload=payload)
            created = projects / 'new-app'
            assert notes not in result['output'], result
            assert notes in (created/'AGENTS.md').read_text()
            assert (created/'CLAUDE.md').read_text().strip() == '@AGENTS.md'
            assert (created/'.dockerignore').exists() and (created/'.gitignore').exists()
            assert not (created/'NEVER').exists() and not (created/'embed.go').exists()
            assert json.loads((created/'devkit.json').read_text())['project'] == 'new-app'
            assert git(created, 'branch', '--show-current') == 'main'
            assert git(created, 'rev-list', '--count', 'HEAD') == '1'
            assert git(created, 'status', '--porcelain') == ''
            assert git(created, 'remote', 'get-url', 'origin') == payload['remote']
            assert any(p['path'] == str(created) for p in request('/api/projects')['projects'])
            before = git(created, 'rev-parse', 'HEAD')
            try:
                request('/api/action', payload)
                raise AssertionError('Duplicate project accepted')
            except urllib.error.HTTPError as error:
                assert error.code == 400
            assert git(created, 'rev-parse', 'HEAD') == before
            history = (parent/'state/history.jsonl').read_text()
            assert notes not in history
            assert any(e['action'] == 'project-create' and e['ok'] for e in map(json.loads, history.splitlines()))
            # Exercise bounded Markdown writes over the real CLI stdin transport.
            doc_payload = {'action': 'document-save', 'worktree': str(created),
                           'path': 'docs/plans/first.md', 'expected': 'new',
                           'content': '# Project plan\n\n' + ('Detailed requirement.\n' * 4000)}
            result = action('document', payload=doc_payload)
            assert doc_payload['content'] not in result['output']
            query = urllib.parse.urlencode({'worktree': str(created), 'path': doc_payload['path']})
            doc = request('/api/document?' + query)
            assert doc['content'] == doc_payload['content'] and doc['editable']
            listing = request('/api/documents?' + urllib.parse.urlencode({'worktree': str(created)}))
            assert any(f['path'] == doc_payload['path'] for f in listing['files'])
            agent = request('/api/document?' + urllib.parse.urlencode({'worktree': str(created), 'path': 'AGENTS.md'}))
            assert not agent['editable']
            doc_payload.update(expected=doc['version'], content='# Revised plan\n\nUpdated from the dashboard.')
            action('document', payload=doc_payload)
            assert (created/'docs/plans/first.md').read_text() == doc_payload['content']
            try:
                request('/api/action', doc_payload)
                raise AssertionError('Stale document overwrite accepted')
            except urllib.error.HTTPError as error:
                assert error.code == 400
            assert git(created, 'rev-parse', 'HEAD') == before, 'Document save committed changes'
            assert git(created, 'status', '--porcelain'), 'Document changes were not visible to Git'
            history = (parent/'state/history.jsonl').read_text()
            assert doc_payload['content'] not in history
            assert any(e['action'] == 'document-save' and e['ok'] for e in map(json.loads, history.splitlines()))
            print('PASS: authenticated HTTP jobs -> CLI -> real local Git fetch/sync/push, project creation with stdin notes/initial commit/origin, document create/read/edit and stale refusal, history, anonymous denial')
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()


if __name__ == '__main__':
    main()
