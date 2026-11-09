#!/usr/bin/env python3
"""Real OpenBao + controlling-terminal setup; isolated network/service fixtures.
Never uses host credentials/services or the deployed devkit-vault project.
"""
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import socket
import subprocess
import tempfile
import termios
import time
from urllib.request import Request, urlopen
from urllib.error import HTTPError

if os.environ.get('DEVKIT_GUIDED_SETUP_TEST') != '1':
    raise SystemExit('Set DEVKIT_GUIDED_SETUP_TEST=1 to run disposable local Docker acceptance.')
repo = Path(__file__).resolve().parents[1]
if os.environ.get('DOCKER_HOST', '').startswith(('ssh:', 'tcp:')):
    raise SystemExit('This runner requires local Docker.')
endpoint = json.loads(subprocess.check_output(['docker', 'context', 'inspect'], text=True))[0]['Endpoints']['docker']['Host']
if not endpoint.startswith('unix://'):
    raise SystemExit('This runner requires a local Unix Docker socket.')
subprocess.run(['docker', 'info'], check=True, stdout=subprocess.DEVNULL)
baseline_ids = subprocess.check_output(['docker', 'ps', '-q'], text=True).split()
baseline = {c['Id']: c['State']['StartedAt'] for c in json.loads(subprocess.check_output(['docker', 'inspect', *baseline_ids]))} if baseline_ids else {}
PASSWORD = 'guided-human-password-SENTINEL'
DNS = 'guided-dns-SENTINEL'

def request(address, path, data=None, token=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['X-Vault-Token'] = token
    req = Request(address + '/v1/' + path, data=json.dumps(data).encode() if data is not None else None, headers=headers)
    with urlopen(req, timeout=15) as response:
        raw = response.read()
        return json.loads(raw) if raw else {}

def session(binary, root, project, recovery, shares=1, threshold=1, interrupt=False):
    env = dict(os.environ, DEVKIT_GUIDED_SETUP_ROOT=str(root), DEVKIT_GUIDED_SETUP_PROJECT=project, TMPDIR=str(root/'tmp'))
    pid, fd = pty.fork()
    if pid == 0:
        os.execve(str(binary), [str(binary), '-test.run=^TestGuidedSetupLocalHostProcess$', '-test.v', '-test.timeout=120s'], env)
    transcript = b''
    cursor = 0
    secret_inputs = 0
    unseal_index = 0
    reaped = False
    deadline = time.monotonic() + 120
    prompts = [
        (b'Preview domain (', b'\n', False),
        (b'Agent harness on this host', b'\n', False),
        (b"Email for the Let's Encrypt account", b'\n', False),
        (b'DNS provider for the certificate', b'\n', False),
        (b'Traefik listens on the Tailscale IP.', b'\n', False),
        (b'Recovery shares (', str(shares).encode()+b'\n', False),
        (b'Shares required to unseal', str(threshold).encode()+b'\n', False),
        (b'Have you saved the recovery shares', b'y\n', False),
        (b'Unseal share:', None, True),
        (b'Bootstrap root token (', None, True),
        (b'New native UI password for admin', PASSWORD.encode()+b'\n', True),
        (b'CF_DNS_API_TOKEN (saved directly to OpenBao):', DNS.encode()+b'\n', True),
        (b'Keep it?', b'y\n', False),
    ]
    try:
        while time.monotonic() < deadline:
            if select.select([fd], [], [], .05)[0]:
                try:
                    transcript += os.read(fd, 8192)
                except OSError:
                    pass
            for line in transcript.splitlines():
                if line.startswith(b'{"keys_base64":'):
                    result = json.loads(line)
                    recovery.update(keys=result['keys_base64'], root=result['root_token'])
            pending = transcript[cursor:]
            for marker, answer, hidden in prompts:
                offset = pending.find(marker)
                if offset < 0 or b': ' not in pending[offset:]:
                    continue
                if marker.startswith(b'Have you saved') and interrupt:
                    assert recovery.get('keys') and recovery.get('root')
                    os.kill(pid, signal.SIGTERM)
                    os.waitpid(pid, 0)
                    reaped = True
                    return {'interrupted': True, 'secret_inputs': secret_inputs}
                if hidden and termios.tcgetattr(fd)[3] & termios.ECHO:
                    break
                if marker == b'Unseal share:':
                    answer = recovery['keys'][unseal_index].encode()+b'\n'
                    unseal_index += 1
                elif marker.startswith(b'Bootstrap root token'):
                    answer = recovery['root'].encode()+b'\n'
                os.write(fd, answer)
                secret_inputs += int(hidden)
                cursor += offset + len(marker)
                break
            done, status = os.waitpid(pid, os.WNOHANG)
            if done:
                reaped = True
                code = os.waitstatus_to_exitcode(status)
                if code != 0 or b'GUIDED_SETUP_COMPLETE' not in transcript:
                    safe = transcript.decode(errors='replace')
                    safe = re.sub(r'\{"keys_base64":.*?\}\r?\n', '[recovery material omitted]\n', safe)
                    for secret in [PASSWORD, DNS, recovery.get('root', ''), *recovery.get('keys', [])]:
                        if secret:
                            safe = safe.replace(secret, '[redacted]')
                    raise RuntimeError('Guided setup failed (exit %s):\n%s' % (code, safe[-4500:]))
                assert PASSWORD.encode() not in transcript and DNS.encode() not in transcript, 'Hidden input was echoed'
                # Recovery output may appear only once in initialization JSON, never echoed later.
                for secret in [recovery.get('root', ''), *recovery.get('keys', [])]:
                    assert not secret or transcript.count(secret.encode()) <= 1, 'Recovery input was echoed'
                return {'interrupted': False, 'secret_inputs': secret_inputs, 'unseal_shares': unseal_index}
        raise RuntimeError('Guided setup timed out (terminal content withheld)')
    finally:
        os.close(fd)
        if not reaped:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)

def inspect_case(root, address, recovery):
    config = json.loads((root/'config/config.json').read_text())
    assert config['proxy']['dns_env']['CF_DNS_API_TOKEN']['openbao'].startswith('openbao://')
    assert DNS not in json.dumps(config)
    backend = (root/'config/secrets/backend-token').read_text().strip()
    assert request(address, 'sys/seal-status')['sealed'] is False
    try:
        request(address, 'auth/token/lookup-self', token=recovery['root'])
        raise AssertionError('Bootstrap token was not revoked')
    except HTTPError as error:
        assert error.code == 403
    login = request(address, 'auth/userpass/login/admin', {'password': PASSWORD})
    human = login['auth']['client_token']
    value = request(address, 'kv/data/devkit/integrations/dns/CF_DNS_API_TOKEN', token=human)
    assert value['data']['data']['value'] == DNS
    request(address, 'auth/token/revoke-self', {}, human)
    for path in [root/'config/config.json', root/'state/history.jsonl', root/'state/secrets/openbao/data/audit.jsonl']:
        content = path.read_bytes()
        for secret in [PASSWORD, DNS, recovery['root'], *recovery['keys']]:
            assert secret.encode() not in content, 'Credential leaked into configuration/history/audit'
    for agent in ['.agents', '.claude']:
        assert (root/'home'/agent/'skills/devkit-secrets/SKILL.md').is_file()
    assert (root/'home/.config/systemd/user/devkit-web.service').is_file()
    return backend

with tempfile.TemporaryDirectory(prefix='devkit-guided-build-') as build:
    binary = Path(build)/'setup-test'
    subprocess.run(['go', 'test', '-c', '-o', str(binary), './cmd/devkit'], cwd=repo, check=True)
    for case in ['fresh', 'interrupted']:
        with tempfile.TemporaryDirectory(prefix='devkit-guided-'+case+'-') as directory:
            root = Path(directory)
            for sub in ['config', 'tmp']:
                (root/sub).mkdir(mode=0o700)
            with socket.socket() as listener:
                listener.bind(('127.0.0.1', 0))
                port = listener.getsockname()[1]
            address = f'http://127.0.0.1:{port}'
            (root/'config/config.json').write_text(json.dumps({'secrets': {'provider': 'openbao', 'address': address}}))
            (root/'config/config.json').chmod(0o600)
            project = root.name
            compose = root/'config/secrets/openbao/compose.json'
            recovery = {}
            try:
                if case == 'interrupted':
                    first = session(binary, root, project, recovery, shares=3, threshold=2, interrupt=True)
                    assert first['interrupted'] and request(address, 'sys/seal-status')['initialized']
                    resumed = session(binary, root, project, recovery)
                    assert resumed['unseal_shares'] == 2
                    print('PASS: interruption after init resumes without reinitialization; two shares unseal real OpenBao')
                else:
                    result = session(binary, root, project, recovery)
                    assert result['unseal_shares'] == 1
                    print('PASS: fresh guided setup completes in one terminal session')
                backend = inspect_case(root, address, recovery)
                repeated = session(binary, root, project, recovery)
                assert repeated['secret_inputs'] == 0, 'Ready rerun requested credentials again'
                assert inspect_case(root, address, recovery) == backend
                print('PASS: ready rerun preserves backend identity, admin login, DNS secret and installed skills')
                if case == 'interrupted':
                    subprocess.run(['docker', 'compose', '-p', project, '-f', str(compose), 'restart'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                    result = session(binary, root, project, recovery)
                    assert result['unseal_shares'] == 2 and result['secret_inputs'] == 2
                    assert inspect_case(root, address, recovery) == backend
                    print('PASS: sealed restart resumes with unseal only; provisioning is not repeated')
            finally:
                if compose.exists():
                    subprocess.run(['docker', 'compose', '-p', project, '-f', str(compose), 'down', '--volumes', '--remove-orphans'], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
if baseline:
    current = json.loads(subprocess.check_output(['docker', 'inspect', *baseline]))
    assert all(c['State']['Running'] and c['State']['StartedAt'] == baseline[c['Id']] for c in current), 'Existing container changed during acceptance'
print('PASS: disposable vaults removed; pre-existing running containers preserved')
print('LIMIT: Tailscale/DNS/certificate/service-manager boundaries use fixtures; no real enrollment, public issuance or OS service start is claimed')
