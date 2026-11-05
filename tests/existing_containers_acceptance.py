#!/usr/bin/env python3
"""Opt-in existing-stack registration checks using only task-owned containers."""
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


def main():
    if os.environ.get('DEVKIT_EXISTING_RUNTIME_TEST') != '1':
        raise SystemExit('Set DEVKIT_EXISTING_RUNTIME_TEST=1 and DEVKIT_TEST_IMAGE to an existing image with /bin/sh.')
    binary = str(Path(os.environ.get('DEVKIT_VM_BINARY', 'bin/devkit')).resolve())
    image = os.environ['DEVKIT_TEST_IMAGE']
    docker = shutil.which('docker')
    token = 'devkit-registration-' + uuid.uuid4().hex[:10]
    cid = server = None
    volume = token + '-data'
    def dock(*args):
        return subprocess.check_output([docker, *args], text=True, timeout=30).strip()
    with tempfile.TemporaryDirectory(prefix=token) as tmp:
        root = Path(tmp)
        try:
            project = root/'projects'/'Example'
            working = project/'runtime'
            working.mkdir(parents=True)
            recipe = project/'devkit.json'
            registration = {'project': 'example', 'existing_compose': [{'project':token, 'working_dir':'runtime'}]}
            recipe.write_text(json.dumps(registration))
            dock('volume', 'create', '--label', 'devkit.acceptance='+token, volume)
            cid = dock('run', '-d', '--pull', 'never', '--network', 'none', '--name', token,
                       '--label', 'com.docker.compose.project='+token,
                       '--label', 'com.docker.compose.project.working_dir='+str(working),
                       '--label', 'com.docker.compose.service=web',
                       '--user', '0', '--mount', f'type=volume,src={volume},dst=/data',
                       '--env', 'PASSWORD=fixture-only-secret', '--entrypoint', '/bin/sh', image,
                       '-c', 'test -f /data/proof || echo retained > /data/proof; echo registration-proof; trap "exit 0" TERM; while :; do sleep 1; done')
            before = json.loads(dock('inspect', cid))[0]
            proof = dock('exec', cid, 'cat', '/data/proof')
            password = uuid.uuid4().hex
            salt = os.urandom(16)
            enc = lambda b: base64.urlsafe_b64encode(b).decode().rstrip('=')
            key = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 600000)
            config = {'domain':'devkit.test', 'prune':{'daily':False}, 'projects_dir':str(root/'projects'),
                      'web':{'password_hash':'pbkdf2-sha256$600000$'+enc(salt)+'$'+enc(key)}}
            (root/'config.json').write_text(json.dumps(config))
            fakebin = root/'bin'; fakebin.mkdir()
            # No command can mutate a user container or trigger housekeeping.
            (fakebin/'docker').write_text('#!/usr/bin/python3\nimport os,sys\na=sys.argv[1:]\n'
                + f'cid={cid!r}\nreal={docker!r}\n'
                + 'if a and a[0]=="ps":\n'
                + ' if not any("label=" in x for x in a): print(cid)\n'
                + 'elif a==["inspect",cid] or a==["start",cid] or a==["stop","--timeout","-1",cid] or (a and a[0]=="logs" and a[-1]==cid): os.execv(real,[real,*a])\n'
                + 'else: sys.exit(0)\n')
            for tool in ['virsh','tailscale']:
                (fakebin/tool).write_text('#!/bin/sh\nexit 0\n')
            for p in fakebin.iterdir(): p.chmod(0o755)
            env = dict(os.environ, DEVKIT_CONFIG_DIR=tmp, DEVKIT_STATE_DIR=tmp, PATH=str(fakebin)+':'+os.environ['PATH'])
            with socket.socket() as s:
                s.bind(('127.0.0.1',0)); webport=s.getsockname()[1]
            origin=f'http://127.0.0.1:{webport}'
            server=subprocess.Popen([binary,'web','--host','127.0.0.1','--port',str(webport)],cwd=root,env=env,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
            jar=http.cookiejar.CookieJar()
            client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
            def request(path,body=None,anonymous=False,text=False):
                req=urllib.request.Request(origin+path,data=None if body is None else json.dumps(body).encode(),headers={'Origin':origin,'Content-Type':'application/json'})
                with (urllib.request.urlopen(req,timeout=15) if anonymous else client.open(req,timeout=15)) as r:
                    return r.read().decode() if text else json.load(r)
            def reject(path,body=None,anonymous=False,code=400):
                try:request(path,body,anonymous);raise AssertionError('request was accepted: '+path)
                except urllib.error.HTTPError as e:assert e.code==code,(e.code,path)
            for _ in range(80):
                try:rows=request('/api/containers')['containers'];break
                except OSError:time.sleep(.1)
            else:raise AssertionError('server did not start')
            assert rows[0]['registered'] and rows[0]['project']=='example' and rows[0]['actions']==['stop'],rows
            assert request('/api/projects')['projects'][0]['main']['containers'][0]['id']==cid
            details=request('/api/container?container='+cid)
            assert 'fixture-only-secret' not in json.dumps(details)
            assert 'registration-proof' in request('/api/logs?container='+cid,text=True)
            for endpoint in ['container','logs']:
                reject('/api/'+endpoint+'?container='+cid[:12])
            recipe.write_text(json.dumps({'project':'example'}))
            reject('/api/container?container='+cid)
            reject('/api/logs?container='+cid)
            recipe.write_text(json.dumps(registration))
            stop={'action':'container-stop','container':cid,'confirm':cid}
            start={'action':'container-start','container':cid}
            reject('/api/action',start,anonymous=True,code=401)
            reject('/api/action',stop,anonymous=True,code=401)
            request('/api/login',{'password':password})
            reject('/api/action',{'action':'container-stop','container':cid})
            def act(body):
                job=request('/api/action',body)
                for _ in range(150):
                    state=request('/api/job?id='+job['id'])
                    if state['done']:
                        assert state['ok'],state
                        return
                    time.sleep(.1)
                raise AssertionError('job did not finish')
            act(stop)
            assert not json.loads(dock('inspect',cid))[0]['State']['Running']
            row=request('/api/projects')['projects'][0]['main']['containers'][0]
            assert row['actions']==['start'],row
            recipe.write_text(json.dumps({'project':'example'}))
            reject('/api/action',start)
            recipe.write_text(json.dumps(registration))
            act(start)
            after=json.loads(dock('inspect',cid))[0]
            assert after['State']['Running'] and before['Id']==after['Id']
            assert before['Image']==after['Image'] and before['Mounts']==after['Mounts']
            assert dock('exec',cid,'cat','/data/proof')==proof=='retained'
            events=request('/api/history')['events']
            assert any(e['action']=='container-start' and e['ok'] for e in events)
            assert any(e['action']=='container-stop' and e['ok'] for e in events)
            print('PASS: project grouping; masked details; logs; revoked access; anonymous rejection; confirmed stop; authenticated start; same ID/image/volume/data; history.',flush=True)
            if os.environ.get('DEVKIT_QA_HOLD')=='1':
                # Private fixture-only credentials for a bounded browser check.
                qa=root/'qa.json';qa.write_text(json.dumps({'origin':origin,'password':password,'container':cid}));qa.chmod(0o600)
                print('QA_FILE='+str(qa),flush=True)
                deadline=time.monotonic()+900
                while not (root/'finish').exists() and time.monotonic()<deadline:time.sleep(1)
        finally:
            if server:
                server.terminate()
                try:server.wait(timeout=5)
                except subprocess.TimeoutExpired:server.kill();server.wait()
            if cid:dock('rm','-f',cid)
            if volume in dock('volume','ls','--format','{{.Name}}').splitlines():dock('volume','rm',volume)

if __name__=='__main__':main()
