#!/usr/bin/env python3
"""Opt-in lifecycle check: isolated launcher, paused 64 MiB QEMU, no guest OS."""
import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import shlex
import socket
import subprocess
import tempfile
import time
import uuid
import urllib.request
import urllib.error


def main():
    if os.environ.get('DEVKIT_VM_RUNTIME_TEST') != '1':
        raise SystemExit('Set DEVKIT_VM_RUNTIME_TEST=1 to test a disposable QEMU guest.')
    binary = os.environ.get('DEVKIT_VM_BINARY', str(Path(__file__).resolve().parents[1] / 'bin/devkit'))
    with tempfile.TemporaryDirectory(prefix='devkit-vm-control-') as tmp:
        root = Path(tmp)
        env = dict(os.environ, DEVKIT_CONFIG_DIR=tmp, DEVKIT_STATE_DIR=tmp)
        name = 'devkit-control-' + uuid.uuid4().hex[:8]
        disk = root / 'guest.qcow2'
        subprocess.run(['qemu-img', 'create', '-f', 'qcow2', str(disk), '32M'], check=True, capture_output=True)
        original = hashlib.sha256(disk.read_bytes()).hexdigest()
        # A local QMP adapter for this paused test guest. Real project adapters should
        # request guest shutdown; this fixture has no OS and uses quit to end QEMU.
        stop = root / 'stop.py'
        stop.write_text('''import json,socket
with socket.socket(socket.AF_UNIX) as s:
 s.settimeout(3)
 s.connect('monitor.sock')
 f=s.makefile('rwb',buffering=0)
 json.loads(f.readline())
 for cmd in ['qmp_capabilities','quit']:
  f.write((json.dumps({'execute':cmd})+'\\n').encode())
  while True:
   line=f.readline()
   if not line: break
   reply=json.loads(line)
   if 'return' in reply: break
   if 'error' in reply: raise RuntimeError(reply)
''')
        start = ['qemu-system-x86_64', '-accel', 'tcg', '-S', '-nodefaults', '-display', 'none',
                 '-m', '64', '-smp', '2', '-name', name, '-daemonize', '-pidfile', 'guest.pid',
                 '-qmp', 'unix:monitor.sock,server=on,wait=off',
                 '-drive', 'if=none,format=qcow2,file=guest.qcow2']
        recipe = {'project': 'vm-lifecycle-test', 'vms': [{'name': name, 'control': {
            'start': shlex.join(start), 'stop': 'python3 stop.py', 'timeout_seconds': 10}}]}
        (root / 'devkit.json').write_text(json.dumps(recipe))
        (root / 'config.json').write_text(json.dumps({'vm_projects': [tmp], 'prune': {'daily': False}}))
        # Bound optional external probes and never expose real Docker state.
        fakebin = root / 'bin'
        fakebin.mkdir()
        for tool in ['docker', 'virsh', 'tailscale']:
            (fakebin / tool).write_text('#!/bin/sh\nexit 0\n')
            (fakebin / tool).chmod(0o755)
        env['PATH'] = str(fakebin) + ':' + env['PATH']
        def run(*args):
            return subprocess.check_output([binary, *args], cwd=tmp, env=env, text=True, timeout=15)
        def vm():
            rows = [v for v in json.loads(run('vms', '--json'))['vms'] if v['name'] == name]
            assert len(rows) == 1, rows
            return rows[0]
        server = None
        try:
            first = vm()
            assert first['state'] == 'stopped' and first['actions'] == ['start'], first
            run('up', '--vm', name)
            started = vm()
            assert started['state'] == 'running' and started['display'] == 'headless', started
            pid = started['pid']
            run('start', '--vm', name)
            assert vm()['pid'] == pid, 'idempotent start duplicated guest'
            run('restart', '--vm', started['control_id'])
            restarted = vm()
            assert restarted['pid'] != pid and restarted['control_id'] == first['control_id'], restarted
            # Fail shutdown and verify restart leaves the same running guest alone.
            recipe['vms'][0]['control']['stop'] = 'exit 7'
            (root / 'devkit.json').write_text(json.dumps(recipe))
            failure = subprocess.run([binary, 'restart', '--vm', name], cwd=tmp, env=env, capture_output=True, timeout=15)
            assert failure.returncode != 0 and vm()['pid'] == restarted['pid']
            recipe['vms'][0]['control']['stop'] = 'python3 stop.py'
            (root / 'devkit.json').write_text(json.dumps(recipe))
            run('stop', '--vm', name)
            run('stop', '--vm', name)
            stopped = vm()
            assert stopped['state'] == 'stopped' and not stopped.get('pid') and stopped['connections'] == [], stopped
            assert hashlib.sha256(disk.read_bytes()).hexdigest() == original, 'disk changed'
            assert str(root) not in json.dumps(stopped) and 'stop.py' not in json.dumps(stopped)
            events = [json.loads(line) for line in (root / 'history.jsonl').read_text().splitlines()]
            assert any(e['action'] == 'vm-restart' and not e['ok'] for e in events), events
            assert any(e['action'] == 'vm-stop' and e['ok'] for e in events), events
            # Exercise the real authenticated HTTP job -> CLI -> QEMU path, with
            # temporary credentials/config and stubbed Docker cleanup only.
            password = uuid.uuid4().hex
            salt = os.urandom(16)
            enc = lambda b: base64.urlsafe_b64encode(b).decode().rstrip('=')
            key = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 600000)
            config = json.loads((root / 'config.json').read_text())
            config['web'] = {'password_hash': 'pbkdf2-sha256$600000$' + enc(salt) + '$' + enc(key)}
            (root / 'config.json').write_text(json.dumps(config))
            with socket.socket() as listener:
                listener.bind(('127.0.0.1', 0))
                webport = listener.getsockname()[1]
            server = subprocess.Popen([binary, 'web', '--host', '127.0.0.1', '--port', str(webport)],
                                      cwd=tmp, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            origin = f'http://127.0.0.1:{webport}'
            client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            def request(path, payload=None):
                body = None if payload is None else json.dumps(payload).encode()
                req = urllib.request.Request(origin + path, data=body,
                    headers={'Content-Type': 'application/json', 'Origin': origin})
                with client.open(req, timeout=12) as response:
                    return json.load(response)
            for _ in range(40):
                try:
                    request('/api/session')
                    break
                except urllib.error.URLError:
                    time.sleep(.05)
            else:
                raise AssertionError('test HTTP server did not start')
            payload = {'action': 'vm-start', 'vm': stopped['control_id']}
            try:
                request('/api/action', payload)
                raise AssertionError('anonymous VM mutation accepted')
            except urllib.error.HTTPError as exc:
                assert exc.code == 401
            request('/api/login', {'password': password})
            for action, expected in [('start', 'running'), ('restart', 'running'), ('stop', 'stopped')]:
                # Prime cache before mutation to catch stale post-job inventory.
                request('/api/vms')
                job = request('/api/action', {'action': 'vm-' + action, 'vm': stopped['control_id']})
                for _ in range(100):
                    result = request('/api/job?id=' + job['id'])
                    if result['done']:
                        assert result['ok'], result
                        break
                    time.sleep(.05)
                else:
                    raise AssertionError('VM HTTP job did not finish')
                rows = [v for v in request('/api/vms')['vms'] if v['name'] == name]
                assert len(rows) == 1 and rows[0]['state'] == expected, rows
            assert hashlib.sha256(disk.read_bytes()).hexdigest() == original
            print('PASS: stopped inventory, real QEMU launch/idempotence/restart/stop, failed-stop guard, retained disk, stable ID, private metadata, history and authenticated HTTP jobs/cache refresh')
        finally:
            if server is not None:
                server.terminate()
                try: server.wait(timeout=5)
                except subprocess.TimeoutExpired: server.kill(); server.wait()
            # Clean only this fixture's monitor, even if an assertion failed.
            if (root / 'monitor.sock').exists():
                subprocess.run(['python3', str(stop)], cwd=tmp, timeout=5, capture_output=True)

if __name__ == '__main__':
    main()
