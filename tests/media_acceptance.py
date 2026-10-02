#!/usr/bin/env python3
"""Opt-in media-port acceptance; only temporary repositories/containers change."""
import concurrent.futures
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import uuid

KIT = Path(__file__).resolve().parents[1]
CLI = str(KIT / "bin/devbox")
ENV = dict(os.environ, DEV_DOMAIN="devbox.test")

APP = '''import http.server
import os
from pathlib import Path
import socket
import threading

def response():
    return Path("message.txt").read_bytes()

def udp():
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(("0.0.0.0", int(os.environ["UDP_PORT"])))
    while True:
        _, peer = sock.recvfrom(1024)
        sock.sendto(response(), peer)

def tcp():
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.bind(("0.0.0.0", int(os.environ["TCP_PORT"])))
    sock.listen()
    while True:
        conn, _ = sock.accept()
        with conn:
            conn.sendall(response())

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(response())

threading.Thread(target=udp, daemon=True).start()
threading.Thread(target=tcp, daemon=True).start()
http.server.HTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
'''


def run(*args, cwd=None):
    result = subprocess.run(list(map(str, args)), cwd=cwd, env=ENV, text=True,
                            capture_output=True)
    if result.returncode:
        raise RuntimeError(f"{args[0]} failed: {result.stdout}\n{result.stderr}")
    return result.stdout.strip()


def saved(root):
    return {path.parent.name: (path, json.loads(path.read_text()))
            for path in (root / ".git/devbox/runs").glob("*/compose.json")}


def probe(config, expected, ip):
    mappings = config["services"]["web"]["ports"]
    assert len(mappings) == 2
    result = {}
    for mapping in mappings:
        assert mapping["host_ip"] == ip
        port = int(mapping["published"])
        assert 20000 <= port <= 29999
        assert mapping["target"] == port
        protocol = mapping["protocol"]
        result[protocol] = port
        if protocol == "udp":
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                sock.settimeout(5)
                sock.sendto(b"probe", (ip, port))
                data, _ = sock.recvfrom(1024)
        else:
            with socket.create_connection((ip, port), timeout=5) as sock:
                data = sock.recv(1024)
        assert data.decode() == expected, (protocol, data, expected)
    return result


def main():
    if os.environ.get("DEVBOX_RUNTIME_TEST") != "1":
        raise SystemExit("Set DEVBOX_RUNTIME_TEST=1 to create temporary test containers.")
    ip = run("tailscale", "ip", "-4")
    run("docker", "network", "inspect", "devbox-proxy")
    project = "media-smoke-" + uuid.uuid4().hex[:8]
    with tempfile.TemporaryDirectory(prefix="devbox-media-") as tmp:
        root = Path(tmp) / "repo"
        shutil.copytree(KIT / "project-template", root)
        cfg = json.loads((root / "devbox.json").read_text())
        cfg["project"] = project
        cfg["media_ports"] = [
            {"name": "sfu_udp", "service": "web", "protocol": "udp"},
            {"name": "ice_tcp", "service": "web", "protocol": "tcp"},
        ]
        (root / "devbox.json").write_text(json.dumps(cfg))
        (root / "app.py").write_text(APP)
        for name in ("compose.live.yaml", "compose.review.yaml"):
            path = root / name
            path.write_text(path.read_text().replace("      DATA_DIR: /data", """      DATA_DIR: /data
      UDP_PORT: ${DEVBOX_MEDIA_SFU_UDP_PORT:?}
      TCP_PORT: ${DEVBOX_MEDIA_ICE_TCP_PORT:?}
      ICE_IP: ${DEVBOX_MEDIA_IP:?}"""))
        run("git", "init", "-b", "main", cwd=root)
        for key, value in (("user.name", "Devbox acceptance"),
                           ("user.email", "devbox@example.invalid"),
                           ("commit.gpgsign", "false"), ("core.hooksPath", "/dev/null")):
            run("git", "config", key, value, cwd=root)
        run("git", "add", ".", cwd=root)
        run("git", "commit", "-m", "Media acceptance", cwd=root)
        tasks = []
        try:
            for branch in ("one", "two"):
                task = Path(tmp) / branch
                run("git", "worktree", "add", "-b", branch, task, cwd=root)
                (task / "message.txt").write_text(branch)
                tasks.append(task)
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                list(pool.map(lambda task: run(CLI, "live", cwd=task), tasks))
            allocations = {}
            for name, (_, config) in saved(root).items():
                branch = config["services"]["web"]["labels"]["devbox.branch"]
                allocations[name] = probe(config, branch, ip)
            ports = [port for allocation in allocations.values() for port in allocation.values()]
            assert len(ports) == 4 and len(set(ports)) == 4
            print("PASS: two concurrent previews use four distinct Tailscale-bound UDP/TCP ports", flush=True)
            name = project + "-one"
            _, before = saved(root)[name]
            run(CLI, "stop", name, cwd=root)
            # A new review cannot take the stopped preview's ports.
            run("git", "add", "message.txt", cwd=tasks[1])
            run("git", "commit", "-m", "Review media", cwd=tasks[1])
            run(CLI, "review", "HEAD", cwd=tasks[1])
            review_name, (_, review) = next((n, v) for n, v in saved(root).items()
                                           if v[1]["services"]["web"]["labels"]["devbox.mode"] == "review")
            review_ports = probe(review, "two", ip)
            assert not set(review_ports.values()).intersection(ports)
            run(CLI, "start", name, cwd=root)
            _, after = saved(root)[name]
            assert before["services"]["web"]["ports"] == after["services"]["web"]["ports"]
            probe(after, "one", ip)
            print("PASS: stopped preview reserves ports, review has separate ports, restart preserves mapping", flush=True)
            run(CLI, "destroy", review_name, cwd=root)
        finally:
            for name in saved(root):
                run(CLI, "destroy", name, cwd=root)
    print("PASS: temporary media previews and allocations removed", flush=True)


if __name__ == "__main__":
    main()
