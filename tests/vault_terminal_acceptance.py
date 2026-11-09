#!/usr/bin/env python3
"""Exercise human input delay through a real PTY; only synthetic credentials are used."""
import argparse
import json
import os
from pathlib import Path
import pty
import select
import signal
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

parser = argparse.ArgumentParser()
parser.add_argument('--devkit', required=True, type=Path)
args = parser.parse_args()
cli = args.devkit.resolve()
requests = []

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        requests.append((self.path, data))
        body = b'{"initialized":true,"sealed":false}'
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
try:
    with tempfile.TemporaryDirectory(prefix='devkit-vault-terminal-') as folder:
        root = Path(folder)
        config = root / 'config'
        config.mkdir(mode=0o700)
        (config / 'config.json').write_text(json.dumps({'secrets': {
            'provider': 'openbao', 'address': f'http://127.0.0.1:{server.server_port}'}}))
        env = dict(os.environ, DEVKIT_CONFIG_DIR=str(config), DEVKIT_STATE_DIR=str(root / 'state'))
        pid, fd = pty.fork()
        if pid == 0:
            os.execve(str(cli), [str(cli), 'vault', 'unseal'], env)
        transcript = b''
        waited = False
        reaped = False
        deadline = time.monotonic() + 50
        try:
            while time.monotonic() < deadline:
                readable, _, _ = select.select([fd], [], [], 0.1)
                if readable:
                    try:
                        transcript += os.read(fd, 4096)
                    except OSError:
                        pass
                if b'Unseal share:' in transcript and not waited:
                    # Exceed the 30-second API deadline while the human is still typing.
                    time.sleep(31)
                    os.write(fd, b'synthetic-unseal-share\n')
                    waited = True
                done, status = os.waitpid(pid, os.WNOHANG)
                if done:
                    reaped = True
                    code = os.waitstatus_to_exitcode(status)
                    if code != 0 or requests != [('/v1/sys/unseal', {'key': 'synthetic-unseal-share'})]:
                        raise SystemExit(f'FAIL: delayed terminal input prevented unseal (exit {code}, requests {len(requests)})')
                    if b'synthetic-unseal-share' in transcript:
                        raise SystemExit('FAIL: terminal echoed credential input')
                    print('PASS: input longer than the API timeout still unseals; credential input stays hidden')
                    break
            else:
                raise SystemExit('FAIL: terminal workflow did not finish')
        finally:
            os.close(fd)
            if not reaped:
                os.kill(pid, signal.SIGKILL)
                os.waitpid(pid, 0)
finally:
    server.shutdown()
    server.server_close()
