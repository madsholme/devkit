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
CLI = str(KIT / "bin/devkit")
env = dict(os.environ, DEV_DOMAIN="devkit.test")


def run(*args, cwd=None, capture=False):
    return subprocess.run(list(map(str, args)), cwd=cwd, env=env, check=True,
                          text=True, stdout=subprocess.PIPE if capture else None).stdout


def containers(project):
    ids = run("docker", "ps", "-aq", "--filter", "label=devkit.project=" + project, capture=True).split()
    return json.loads(run("docker", "inspect", *ids, capture=True)) if ids else []


def web(project, branch=None, mode="live"):
    return next(c for c in containers(project)
                if c["Config"]["Labels"]["devkit.mode"] == mode
                and (branch is None or c["Config"]["Labels"]["devkit.branch"] == branch))


def request(container, method="GET"):
    code = "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8000/" + (
        "increment', data=b'')" if method == "POST" else "')"
    ) + ".read().decode())"
    return run("docker", "exec", container["Id"], "python", "-c", code, capture=True)


def rebuild_checks(tmp, project, preflight_only=False):
    root = Path(tmp) / "rebuild"
    shutil.copytree(KIT / "project-template", root)
    cfg = json.loads((root / "devkit.json").read_text())
    cfg["project"] = project
    cfg["wait_timeout"] = 4
    (root / "devkit.json").write_text(json.dumps(cfg))
    model = (root / "compose.live.yaml").read_text().replace("image: python:3.13-slim", "image: devkit/" + project + ":live\n    build: .")
    model = model.replace("      - .:/app:ro\n", "")
    (root / "compose.live.yaml").write_text(model)
    (root / "message.txt").write_text("rebuild-v1")
    app = (root / "app.py").read_text().replace('self.send_response(200)', 'self.send_response(503 if (data_dir / "fail-health").exists() else 200)', 1)
    (root / "app.py").write_text(app)
    run("git", "init", "-b", "main", cwd=root, capture=True)
    run("git", "config", "user.name", "Devkit acceptance", cwd=root)
    run("git", "config", "user.email", "devkit@example.invalid", cwd=root)
    run("git", "config", "commit.gpgsign", "false", cwd=root)
    run("git", "config", "core.hooksPath", "/dev/null", cwd=root)
    run("git", "add", ".", cwd=root)
    run("git", "commit", "-m", "Rebuild fixture", cwd=root, capture=True)
    name = None
    try:
        run(CLI, "live", cwd=root)
        initial = web(project)
        name = initial["Config"]["Labels"]["devkit.preview"]
        saved = root / ".git/devkit/runs" / name / "compose.json"
        before = saved.read_bytes()
        assert "<strong>1</strong>" in request(initial, "POST")
        validation = json.loads(run(CLI, "project", "validate", "--json", cwd=root, capture=True))
        assert validation["complete"]
        context = json.loads(run(CLI, "context", "--json", cwd=root, capture=True))
        assert any(p["id"] == name for p in context["data"]["previews"])
        (root / "message.txt").write_text("rebuild-v2")
        with (root / "Dockerfile").open("a") as f:
            f.write("\nLABEL acceptance.version=v2\n")
        plan = json.loads(run(CLI, "rebuild", name, "--json", cwd=root, capture=True))
        assert plan["data"]["build_services"] == ["web"] and not plan["data"]["apply"]
        assert saved.read_bytes() == before and web(project)["Id"] == initial["Id"]
        assert "rebuild-v1" in request(initial)
        print("PASS: validate/context/rebuild preflight leaves running fixture unchanged", flush=True)
        if preflight_only:
            return
        applied = json.loads(run(CLI, "rebuild", name, "--yes", "--json", cwd=root, capture=True))
        assert applied["data"]["apply"]
        updated = web(project)
        assert updated["Id"] != initial["Id"] and updated["Image"] != initial["Image"]
        assert updated["Mounts"] == initial["Mounts"]
        assert updated["Config"]["Labels"]["devkit.url"] == initial["Config"]["Labels"]["devkit.url"]
        assert "rebuild-v2" in request(updated) and "<strong>1</strong>" in request(updated)
        print("PASS: rebuilt image serves new behavior at the same URL with SQLite sentinel retained", flush=True)
        before = saved.read_bytes()
        dockerfile = (root / "Dockerfile").read_text()
        (root / "Dockerfile").write_text(dockerfile + "\nRUN false\n")
        failed = subprocess.run([CLI, "rebuild", name, "--yes", "--json"], cwd=root, env=env, text=True, capture_output=True)
        assert failed.returncode == 1 and json.loads(failed.stdout)["schema_version"] == 1
        assert saved.read_bytes() == before and web(project)["Id"] == updated["Id"]
        record = Path(env["DEVKIT_STATE_DIR"]) / "rebuilds" / name / "operation.json"
        failure = json.loads(record.read_text())
        assert failure["phase"] == "failed" and not failure["built"]
        # Explicit manual retirement is safe in this disposable fixture: build never applied.
        record.unlink()
        (root / "Dockerfile").write_text(dockerfile)
        print("PASS: failed build retains saved configuration, runtime and data", flush=True)
        run("docker", "exec", updated["Id"], "touch", "/data/fail-health")
        (root / "message.txt").write_text("rebuild-v3")
        failed = subprocess.run([CLI, "rebuild", name, "--yes", "--json"], cwd=root, env=env, text=True, capture_output=True)
        assert failed.returncode == 1, failed.stdout
        failure = json.loads(record.read_text())
        assert failure["phase"] == "failed" and failure["built"]
        assert saved.read_bytes() == before
        blocked = subprocess.run([CLI, "start", name], cwd=root, env=env, capture_output=True)
        assert blocked.returncode == 1
        run("docker", "exec", web(project)["Id"], "rm", "/data/fail-health")
        recovered = json.loads(run(CLI, "rebuild", name, "--recover", "--yes", "--json", cwd=root, capture=True))
        assert recovered["data"]["apply"]
        assert "rebuild-v3" in request(web(project)) and "<strong>1</strong>" in request(web(project))
        print("PASS: failed health apply blocks stale start; same-candidate recovery preserves sentinel", flush=True)
        run(CLI, "stop", name, cwd=root)
        (root / "message.txt").write_text("rebuild-stopped")
        run(CLI, "rebuild", name, "--yes", "--json", cwd=root, capture=True)
        assert not web(project)["State"]["Running"]
        run(CLI, "start", name, cwd=root)
        assert "rebuild-stopped" in request(web(project)) and "<strong>1</strong>" in request(web(project))
        print("PASS: stopped rebuild stages image without starting services; explicit start retains data", flush=True)
    finally:
        if name:
            # The fixture owns all of these resources; retire recovery evidence only
            # for teardown, never to resume or recreate an interrupted preview.
            record = Path(env["DEVKIT_STATE_DIR"]) / "rebuilds" / name / "operation.json"
            if record.exists():
                record.unlink()
            run(CLI, "destroy", name, cwd=root)


def main():
    if os.environ.get("DEVKIT_RUNTIME_TEST") != "1":
        raise SystemExit("Set DEVKIT_RUNTIME_TEST=1 to create temporary test containers.")
    project = "smoke-" + uuid.uuid4().hex[:8]
    proxy = "devkit-smoke-edge-" + project
    network_exists = subprocess.run(["docker", "network", "inspect", "devkit-proxy"],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    if not network_exists:
        run("docker", "network", "create", "devkit-proxy")
    with tempfile.TemporaryDirectory(prefix="devkit-smoke-") as tmp:
        env["DEVKIT_STATE_DIR"] = str(Path(tmp) / "state")
        env["DEVKIT_CONFIG_DIR"] = str(Path(tmp) / "config")
        if os.environ.get("DEVKIT_REBUILD_PREFLIGHT") == "1":
            try:
                rebuild_checks(tmp, project, preflight_only=True)
            finally:
                if not network_exists:
                    run("docker", "network", "rm", "devkit-proxy")
            return
        if os.environ.get("DEVKIT_REBUILD_ONLY") == "1":
            try:
                rebuild_checks(tmp, project)
            finally:
                if not network_exists:
                    run("docker", "network", "rm", "devkit-proxy")
            return
        root = Path(tmp) / "repo"
        shutil.copytree(KIT / "project-template", root)
        cfg = json.loads((root / "devkit.json").read_text())
        cfg["project"] = project
        (root / "devkit.json").write_text(json.dumps(cfg))
        run("git", "init", "-b", "main", cwd=root)
        run("git", "config", "user.name", "Devkit acceptance", cwd=root)
        run("git", "config", "user.email", "devkit@example.invalid", cwd=root)
        run("git", "config", "commit.gpgsign", "false", cwd=root)
        run("git", "config", "core.hooksPath", "/dev/null", cwd=root)
        run("git", "add", ".", cwd=root)
        run("git", "commit", "-m", "Acceptance sample", cwd=root)
        try:
            paths = []
            for index, branch in enumerate(("try/a", "try-a")):
                # Worktrees are created by the user's tools; devkit only runs previews in them.
                task = Path(tmp) / f"task-{index}"
                run("git", "worktree", "add", "-b", branch, task, cwd=root)
                paths.append(task)
                run(CLI, "live", cwd=task)
            a, b = web(project, "try/a"), web(project, "try-a")
            assert a["Config"]["Labels"]["devkit.url"] != b["Config"]["Labels"]["devkit.url"]
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
            assert image_id == labels["devkit.image"]
            (paths[0] / "message.txt").write_text("Agent edits after review build")
            assert "Agent edits after review build" in request(a)
            assert "First live task" in request(review)
            assert "<strong>1</strong>" in request(review, "POST")
            run(CLI, "stop", labels["devkit.preview"], cwd=root)
            run(CLI, "start", labels["devkit.preview"], cwd=root)
            review = web(project, mode="review")
            assert review["Image"] == image_id
            assert "<strong>1</strong>" in request(review)
            print("PASS: commit-based image stays fixed; stop/start retains data", flush=True)
            run("docker", "run", "-d", "--name", proxy, "--network", "devkit-proxy",
                "-p", "127.0.0.1::443", "-v", "/var/run/docker.sock:/var/run/docker.sock:ro",
                "traefik:v3", "--entrypoints.websecure.address=:443", "--providers.docker=true",
                "--providers.docker.exposedbydefault=false", "--providers.docker.network=devkit-proxy")
            address = run("docker", "port", proxy, "443/tcp", capture=True).strip()
            port = address.rsplit(":", 1)[1]
            host = labels["devkit.url"].removeprefix("https://")
            for attempt in range(30):
                response = subprocess.run(["curl", "-ksS", "--noproxy", "*", "--resolve",
                    f"{host}:{port}:127.0.0.1", f"https://{host}:{port}/"], text=True, capture_output=True)
                if "First live task" in response.stdout:
                    break
                time.sleep(1)
            else:
                raise AssertionError("Traefik did not route to the review app")
            print("PASS: real Traefik hostname routing over test TLS (not ACME)", flush=True)
            rebuild_checks(tmp, project + "-rebuild")
        finally:
            subprocess.run(["docker", "rm", "-f", proxy], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            for path in (root / ".git/devkit/runs").glob("*/compose.json"):
                run(CLI, "destroy", path.parent.name, cwd=root)
            if not network_exists:
                run("docker", "network", "rm", "devkit-proxy")
    print("PASS: test containers and volumes cleaned up; downloaded image caches retained", flush=True)


if __name__ == "__main__":
    main()
