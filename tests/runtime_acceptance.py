#!/usr/bin/env python3
"""Opt-in Docker acceptance check. Creates only temporary test resources."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import uuid

KIT = Path(__file__).resolve().parents[1]
CLI = str(KIT / "bin/devbox")
env = dict(os.environ, DEV_DOMAIN="devbox.test")


def run(*args, cwd=None, capture=False):
    return subprocess.run(list(map(str, args)), cwd=cwd, env=env, check=True,
                          text=True, stdout=subprocess.PIPE if capture else None).stdout


def containers(project):
    ids = run("docker", "ps", "-aq", "--filter", "label=devbox.project=" + project, capture=True).split()
    return json.loads(run("docker", "inspect", *ids, capture=True)) if ids else []


def web(project, branch=None, mode="live"):
    return next(c for c in containers(project)
                if c["Config"]["Labels"]["devbox.mode"] == mode
                and (branch is None or c["Config"]["Labels"]["devbox.branch"] == branch))


def request(container, method="GET"):
    code = "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8000/" + (
        "increment', data=b'')" if method == "POST" else "')"
    ) + ".read().decode())"
    return run("docker", "exec", container["Id"], "python", "-c", code, capture=True)


def main():
    if os.environ.get("DEVBOX_RUNTIME_TEST") != "1":
        raise SystemExit("Set DEVBOX_RUNTIME_TEST=1 to create temporary test containers.")
    project = "smoke-" + uuid.uuid4().hex[:8]
    proxy = "devbox-smoke-edge-" + project
    network_exists = subprocess.run(["docker", "network", "inspect", "devbox-proxy"],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    if not network_exists:
        run("docker", "network", "create", "devbox-proxy")
    with tempfile.TemporaryDirectory(prefix="devbox-smoke-") as tmp:
        root = Path(tmp) / "repo"
        shutil.copytree(KIT / "project-template", root)
        cfg = json.loads((root / "devbox.json").read_text())
        cfg["project"] = project
        (root / "devbox.json").write_text(json.dumps(cfg))
        run("git", "init", "-b", "main", cwd=root)
        run("git", "config", "user.name", "Devbox acceptance", cwd=root)
        run("git", "config", "user.email", "devbox@example.invalid", cwd=root)
        run("git", "config", "commit.gpgsign", "false", cwd=root)
        run("git", "config", "core.hooksPath", "/dev/null", cwd=root)
        run("git", "add", ".", cwd=root)
        run("git", "commit", "-m", "Acceptance sample", cwd=root)
        try:
            paths = []
            for branch in ("try/a", "try-a"):
                output = run(CLI, "new", branch, cwd=root, capture=True)
                task = Path(output.strip().splitlines()[-1])
                paths.append(task)
                run(CLI, "live", cwd=task)
            a, b = web(project, "try/a"), web(project, "try-a")
            assert a["Config"]["Labels"]["devbox.url"] != b["Config"]["Labels"]["devbox.url"]
            (paths[0] / "message.txt").write_text("First live task")
            (paths[1] / "message.txt").write_text("Second live task")
            assert "First live task" in request(a)
            assert "Second live task" in request(b)
            assert "<strong>1</strong>" in request(a, "POST")
            assert "<strong>0</strong>" in request(b)
            print("PASS: simultaneous live previews, slug collision avoidance, independent data", flush=True)
            run("git", "add", "message.txt", cwd=paths[0])
            run("git", "commit", "-m", "Review version", cwd=paths[0])
            run(CLI, "review", "HEAD", cwd=paths[0])
            review = web(project, mode="review")
            labels = review["Config"]["Labels"]
            image_id = review["Image"]
            assert image_id == labels["devbox.image"]
            (paths[0] / "message.txt").write_text("Agent edits after review build")
            assert "Agent edits after review build" in request(a)
            assert "First live task" in request(review)
            assert "<strong>1</strong>" in request(review, "POST")
            run(CLI, "stop", labels["devbox.preview"], cwd=root)
            run(CLI, "start", labels["devbox.preview"], cwd=root)
            review = web(project, mode="review")
            assert review["Image"] == image_id
            assert "<strong>1</strong>" in request(review)
            print("PASS: commit-based image stays fixed; stop/start retains data", flush=True)
            run("docker", "run", "-d", "--name", proxy, "--network", "devbox-proxy",
                "-p", "127.0.0.1::443", "-v", "/var/run/docker.sock:/var/run/docker.sock:ro",
                "traefik:v3", "--entrypoints.websecure.address=:443", "--providers.docker=true",
                "--providers.docker.exposedbydefault=false", "--providers.docker.network=devbox-proxy")
            address = run("docker", "port", proxy, "443/tcp", capture=True).strip()
            port = address.rsplit(":", 1)[1]
            host = labels["devbox.url"].removeprefix("https://")
            for attempt in range(30):
                response = subprocess.run(["curl", "-ksS", "--noproxy", "*", "--resolve",
                    f"{host}:{port}:127.0.0.1", f"https://{host}:{port}/"], text=True, capture_output=True)
                if "First live task" in response.stdout:
                    break
                time.sleep(1)
            else:
                raise AssertionError("Traefik did not route to the review app")
            print("PASS: real Traefik hostname routing over test TLS (not ACME)", flush=True)
        finally:
            subprocess.run(["docker", "rm", "-f", proxy], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            for path in (root / ".git/devbox/runs").glob("*/compose.json"):
                run(CLI, "destroy", path.parent.name, cwd=root)
            if not network_exists:
                run("docker", "network", "rm", "devbox-proxy")
    print("PASS: test containers and volumes cleaned up; downloaded image caches retained", flush=True)


if __name__ == "__main__":
    main()
