#!/usr/bin/env python3
"""Opt-in real ACPI/QMP and individual Docker shutdown, using disposable resources only."""
import base64
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


def main():
    if os.environ.get('DEVKIT_SHUTDOWN_RUNTIME_TEST') != '1':
        raise SystemExit('Set DEVKIT_SHUTDOWN_RUNTIME_TEST=1 and DEVKIT_TEST_IMAGE to an existing image with /bin/sh.')
    binary = str(Path(os.environ.get('DEVKIT_VM_BINARY', 'bin/devkit')).resolve())
    image = os.environ['DEVKIT_TEST_IMAGE']
    docker = shutil.which('docker')
    token = 'devkit-shutdown-' + uuid.uuid4().hex[:10]
    cid = None
    qemu = server = None
    volume = token + '-data'
    def dock(*args):
        return subprocess.check_output([docker, *args], text=True, timeout=30).strip()
    with tempfile.TemporaryDirectory(prefix='devkit-shutdown-') as tmp:
        root = Path(tmp)
        try:
            dock('volume', 'create', '--label', 'devkit.acceptance=' + token, volume)
            cid = dock('run', '-d', '--pull', 'never', '--network', 'none', '--name', token,
                       '--user', '0', '--mount', f'type=volume,src={volume},dst=/data', '--entrypoint', '/bin/sh',
                       image, '-c', 'echo retained > /data/proof; trap "exit 0" TERM; while :; do sleep 1; done')
            # SeaBIOS/PIIX4 boot guest: enable the power-button event (PM1_EN
            # 0x602), enter ACPI mode (SMI_CMD 0xb2), poll PWRBTN_STS (0x600),
            # then acknowledge the user's power button by entering S5 (0x604).
            # This guest exits itself; the host never sends QMP quit or SIGKILL.
            code = bytes.fromhex('ba0206b80001efbab200b0f1eeba0006eda9000174faba0406b80020efebfe')
            disk = root / 'boot.img'
            disk.write_bytes(code + bytes(510-len(code)) + b'\x55\xaa' + bytes(1474560-512))
            disk_hash = hashlib.sha256(disk.read_bytes()).hexdigest()
            spice, vnc = port(), port()
            qemu = subprocess.Popen(['qemu-system-x86_64', '-machine', 'pc', '-accel', 'tcg', '-m', '32',
                '-name', token, '-boot', 'a', '-display', 'none', '-serial', 'none', '-monitor', 'none',
                '-drive', f'file={disk},format=raw,if=floppy,readonly=on',
                '-qmp', f'unix:{root}/qmp,server=on,wait=off', '-spice', f'addr=127.0.0.1,port={spice},disable-ticketing=on',
                '-vnc', f'127.0.0.1:{vnc-5900}'], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            password = uuid.uuid4().hex
            salt = os.urandom(16)
            enc = lambda b: base64.urlsafe_b64encode(b).decode().rstrip('=')
            key = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 600000)
            config = {'domain': 'devkit.test', 'tailscale_ip': '100.64.0.1', 'prune': {'daily': False}, 'projects_dir': str(root/'projects'),
                      'web': {'password_hash': 'pbkdf2-sha256$600000$'+enc(salt)+'$'+enc(key)}}
            project = root/'projects'/'Demo'
            project.mkdir(parents=True)
            recipe = project/'devkit.json'
            recipe.write_text(json.dumps({'project': 'shutdown-demo', 'vms': [{'name': token}]}))
            recipe_hash = hashlib.sha256(recipe.read_bytes()).hexdigest()
            (root/'config.json').write_text(json.dumps(config))
            fakebin = root/'bin'
            fakebin.mkdir()
            # Only this test's exact container is visible/mutable. Housekeeping
            # cannot reach any real project, volume or container outside the fixture.
            (fakebin/'docker').write_text('#!/usr/bin/python3\nimport os,sys\na=sys.argv[1:]\n'
                + f'cid={cid!r}\nreal={docker!r}\n'
                + 'if a and a[0]=="ps": print(cid)\n'
                + 'elif a and a[0]=="inspect" and a[1:]==[cid]: os.execv(real,[real,*a])\n'
                + 'elif a==["stop","--timeout","-1",cid]: os.execv(real,[real,*a])\n'
                + 'else: sys.exit(0)\n')
            for tool in ['virsh', 'tailscale']:
                (fakebin/tool).write_text('#!/bin/sh\nexit 0\n')
            for p in fakebin.iterdir(): p.chmod(0o755)
            env = dict(os.environ, DEVKIT_CONFIG_DIR=tmp, DEVKIT_STATE_DIR=tmp, PATH=str(fakebin)+':'+os.environ['PATH'])
            webport = port()
            origin = f'http://127.0.0.1:{webport}'
            server = subprocess.Popen([binary, 'web', '--host', '127.0.0.1', '--port', str(webport)], cwd=root,
                                      env=env, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            jar = http.cookiejar.CookieJar()
            client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
            def request(path, body=None, anonymous=False):
                req = urllib.request.Request(origin+path, data=None if body is None else json.dumps(body).encode(),
                                             headers={'Origin': origin, 'Content-Type': 'application/json'})
                with (urllib.request.urlopen(req, timeout=15) if anonymous else client.open(req, timeout=15)) as r:
                    return json.load(r)
            for _ in range(80):
                try:
                    rows = request('/api/vms')['vms']
                    vm = next(v for v in rows if v['name']==token)
                    break
                except (OSError, StopIteration): time.sleep(.1)
            else: raise AssertionError('VM/server did not start')
            assert qemu.poll() is None
            assert vm['display']=='vnc+spice' and vm['actions']==['stop'], vm
            commands = {c['kind']:c['command'] for c in vm['connections']}
            assert commands['spice']==f'remote-viewer spice://127.0.0.1:{spice}'
            assert commands['vnc']==f'remote-viewer vnc://127.0.0.1:{vnc}'
            assert str(root) not in json.dumps(vm), 'private path leaked'
            cr = request('/api/containers')['containers'][0]
            assert cr['id']==cid and cr['actions']==['stop'] and not cr['managed'], cr
            requests = [{'action':'vm-stop','vm':vm['control_id'],'confirm':vm['control_id']},
                        {'action':'container-stop','container':cid,'confirm':cid}]
            for body in requests:
                try: request('/api/action',body,anonymous=True); raise AssertionError('anonymous mutation')
                except urllib.error.HTTPError as e: assert e.code==401
            request('/api/login',{'password':password})
            for body in requests:
                try: request('/api/action',dict(body,confirm='')); raise AssertionError('missing confirmation')
                except urllib.error.HTTPError as e: assert e.code==400
            # Optional browser checkpoint: inspect both protocols, confirm/cancel,
            # then turn off these same disposable resources through the actual UI.
            ready = os.environ.get('DEVKIT_SHUTDOWN_UI_READY')
            if ready:
                descriptor = Path(ready)
                descriptor.write_text(json.dumps({'url':origin,'password':password,'vm':token,'container':token}))
                descriptor.chmod(0o600)
                print('UI ready:',ready,flush=True)
                until = time.monotonic()+600
                while not Path(ready+'.continue').exists():
                    if time.monotonic()>until: raise TimeoutError('browser checkpoint expired')
                    time.sleep(.2)
            for body in requests:
                running = qemu.poll() is None if 'vm' in body else json.loads(dock('inspect',cid))[0]['State']['Running']
                if running:
                    job=request('/api/action',body)
                    for _ in range(80):
                        result=request('/api/job?id='+job['id'])
                        if result['done']:
                            assert result['ok'], result
                            break
                        time.sleep(.1)
                    else: raise AssertionError('shutdown job did not complete')
            assert qemu.wait(timeout=5)==0, qemu.stderr.read().decode()
            assert not json.loads(dock('inspect',cid))[0]['State']['Running']
            assert hashlib.sha256(disk.read_bytes()).hexdigest()==disk_hash
            assert hashlib.sha256(recipe.read_bytes()).hexdigest()==recipe_hash
            assert dock('volume','inspect',volume), 'volume removed'
            dock('cp',cid+':/data/proof',str(root/'proof'))
            assert (root/'proof').read_text().strip()=='retained'
            assert not any(v['name']==token for v in request('/api/vms')['vms'])
            assert request('/api/containers')['containers'][0]['status']=='exited'
            history=[json.loads(line) for line in (root/'history.jsonl').read_text().splitlines()]
            for action in ['vm-stop','container-stop']:
                assert any(e['action']==action and e['ok'] for e in history), history
            print('PASS: dual VNC/SPICE, authenticated CLI-backed UI/API shutdown, real guest ACPI exit, exact container stop, retained disk/recipe/volume/data, inventory refresh and history')
        finally:
            for p in [server,qemu]:
                if p and p.poll() is None: p.terminate()
                if p:
                    try: p.wait(timeout=5)
                    except subprocess.TimeoutExpired: p.kill(); p.wait()
                    p.stderr.close()
            if cid: dock('rm','-f',cid)
            dock('volume','rm',volume)


if __name__=='__main__': main()
