#!/usr/bin/env python3
"""Opt-in read-only inventory check with two disposable, paused QEMU processes."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import uuid
import urllib.request
import urllib.error


def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


def main():
    if os.environ.get('DEVKIT_VM_RUNTIME_TEST') != '1':
        raise SystemExit('Set DEVKIT_VM_RUNTIME_TEST=1 to create temporary paused QEMU processes.')
    binary = os.environ.get('DEVKIT_VM_BINARY', str(Path(__file__).resolve().parents[1]/'bin/devkit'))
    processes = []
    with tempfile.TemporaryDirectory(prefix='devkit-vm-test-') as tmp:
        root = Path(tmp)
        env = dict(os.environ, DEVKIT_CONFIG_DIR=tmp, DEVKIT_STATE_DIR=tmp)
        (root/'config.json').write_text(json.dumps({'tailscale_ip':'100.64.0.1','prune':{'daily':False}}))
        disk = root/'test.qcow2'
        subprocess.run(['qemu-img','create','-f','qcow2',str(disk),'32M'], check=True, capture_output=True)
        token = 'devkit-test-'+uuid.uuid4().hex[:8]
        spice, ssh = port(), port()
        common = ['qemu-system-x86_64','-accel','tcg','-S','-nodefaults','-display','none','-m','64','-smp','2']
        try:
            for name, args in [('display', ['-spice',f'addr=127.0.0.1,port={spice},disable-ticketing=on','-device','qxl-vga','-drive',f'if=none,format=qcow2,file={disk}']),
                               ('headless',['-netdev',f'user,id=n,hostfwd=tcp:127.0.0.1:{ssh}-:22'])]:
                processes.append(subprocess.Popen([*common,'-name',token+'-'+name,*args], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE))
            for _ in range(40):
                if any(p.poll() is not None for p in processes):
                    raise AssertionError([p.stderr.read().decode() for p in processes if p.poll() is not None])
                try:
                    with socket.create_connection(('127.0.0.1',spice), timeout=.1): pass
                    with socket.create_connection(('127.0.0.1',ssh), timeout=.1): pass
                    break
                except OSError: time.sleep(.05)
            else: raise AssertionError('temporary QEMU listeners did not start')
            output = subprocess.check_output([binary,'vms','--json'], env=env, text=True, timeout=12)
            data = json.loads(output)
            rows = {v['name'].removeprefix(token+'-'):v for v in data['vms'] if v['name'].startswith(token+'-')}
            assert set(rows)=={'headless','display'}, rows
            for vm in rows.values():
                assert vm['vcpus']==2 and vm['memory_bytes']==64*1024*1024 and vm['rss_bytes']>0, vm
                assert len(vm['connections'])==1, vm
                assert 'ExitOnForwardFailure=yes' in vm['connections'][0]['tunnel'], vm
            assert rows['headless']['display']=='headless'
            assert rows['headless']['connections'][0]['kind']=='ssh'
            assert rows['display']['display']=='spice' and rows['display']['graphics']==['qxl-vga']
            assert rows['display']['disk_bytes']==32*1024*1024
            assert rows['display']['connections'][0]['command']==f'remote-viewer spice://127.0.0.1:{spice}'
            assert str(root) not in json.dumps(rows)
            # Stub Docker in this test server so background share cleanup cannot see real containers.
            fakebin = root/'bin'
            fakebin.mkdir()
            (fakebin/'docker').write_text('#!/bin/sh\nexit 0\n')
            (fakebin/'docker').chmod(0o755)
            webport = port()
            webenv = dict(env, PATH=str(fakebin)+':'+env['PATH'])
            server = subprocess.Popen([binary,'web','--host','127.0.0.1','--port',str(webport)], env=webenv, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            processes.append(server)
            url = f'http://127.0.0.1:{webport}/api/vms'
            for _ in range(40):
                try:
                    with urllib.request.urlopen(url, timeout=8) as response:
                        first = json.load(response)
                    break
                except urllib.error.URLError: time.sleep(.05)
            else: raise AssertionError('test web server did not start')
            firstrows = [v for v in first['vms'] if v['name'].startswith(token+'-')]
            assert len(firstrows)==2 and all(v['cpu_percent'] is None for v in firstrows)
            time.sleep(5.1)
            with urllib.request.urlopen(url, timeout=8) as response:
                second = json.load(response)
            secondrows = [v for v in second['vms'] if v['name'].startswith(token+'-')]
            assert len(secondrows)==2 and all(v['cpu_percent'] is not None for v in secondrows)
            try:
                urllib.request.urlopen(urllib.request.Request(url, data=b'{}'), timeout=2)
                raise AssertionError('VM endpoint accepted POST')
            except urllib.error.HTTPError as exc:
                assert exc.code==404
            print('PASS: live headless/SSH and SPICE discovery, sizing, RSS, private commands, metadata privacy and read-only API CPU sampling')
        finally:
            for p in processes:
                if p.poll() is None: p.terminate()
            for p in processes:
                try: p.wait(timeout=5)
                except subprocess.TimeoutExpired: p.kill(); p.wait()
                p.stderr.close()

if __name__ == '__main__':
    main()
