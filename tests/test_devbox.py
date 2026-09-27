import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

KIT = Path(__file__).resolve().parents[1]
loader = importlib.machinery.SourceFileLoader("devbox", str(KIT / "bin/devbox"))
spec = importlib.util.spec_from_loader(loader.name, loader)
dbx = importlib.util.module_from_spec(spec)
loader.exec_module(dbx)


class Naming(unittest.TestCase):
    def test_normalization_collisions_and_length(self):
        names = [dbx.preview_name("shop", task, "live") for task in
                 ("fix/login", "fix-login", "FIX-login", "x" * 400, "🍋")]
        self.assertEqual(len(names), len(set(names)))
        for name in names:
            self.assertLessEqual(len(name), 63)
            self.assertRegex(name, r"^[a-z0-9-]+$")
        self.assertEqual(names[0], dbx.preview_name("shop", "fix/login", "live"))

    def test_hostname_rejects_config_injection(self):
        for value in ("", "*.dev.example.com", "dev.example.com`)", "a..com"):
            with patch.dict(os.environ, DEV_DOMAIN=value):
                with self.assertRaises(ValueError):
                    dbx.domain()


class ComposeContract(unittest.TestCase):
    def env(self):
        return dict(os.environ, DEVBOX_PREVIEW="demo-review-123", DEVBOX_HOST="demo-review-123.dev.example.com",
                    DEVBOX_IMAGE="devbox/demo:abc", DEVBOX_COMMIT="abc")

    def test_real_compose_template_and_pinned_web_image(self):
        actual_run = dbx.run

        def run(args, **kwargs):
            if args[:3] == ["docker", "image", "inspect"]:
                return json.dumps([{"Id": "sha256:fixed-image"}])
            return actual_run(args, **kwargs)

        root = KIT / "project-template"
        with patch.object(dbx, "run", side_effect=run):
            output = dbx.render(root, dbx.config(root), "review", self.env(), {dbx.LABEL: "true"})
        web = output["services"]["web"]
        self.assertEqual(web["image"], "sha256:fixed-image")
        self.assertEqual(web["pull_policy"], "never")
        self.assertFalse(any(v["type"] == "bind" for v in web["volumes"]))
        self.assertEqual(output["volumes"]["data"]["name"], "demo-review-123_data")
        self.assertEqual(web["labels"]["traefik.docker.network"], "devbox-proxy")

    def test_live_template_resolves_absolute_source_mount(self):
        root = KIT / "project-template"
        output = dbx.render(root, dbx.config(root), "live", self.env(), {})
        binds = [v for v in output["services"]["web"]["volumes"] if v["type"] == "bind"]
        self.assertEqual(binds[0]["source"], str(root))
        self.assertEqual(output["services"]["web"]["restart"], "unless-stopped")

    def test_review_has_no_restart_policy(self):
        run = lambda args, **kwargs: (json.dumps([{"Id": "sha256:x"}]) if args[:3] == ["docker", "image", "inspect"]
                                      else json.dumps({"services": {"web": {}}}))
        with patch.object(dbx, "run", side_effect=run):
            output = dbx.render(KIT / "project-template", {"project": "demo"}, "review", self.env(), {})
        self.assertNotIn("restart", output["services"]["web"])

    def test_saved_live_preview_gains_restart_policy(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "compose.json"
            path.write_text(json.dumps({"services": {
                "web": {"labels": {"devbox.mode": "live"}},
                "seed": {"labels": {"devbox.mode": "live"}, "restart": "no"}}}))
            with patch.object(dbx, "compose"):
                dbx.up(path)
            services = json.loads(path.read_text())["services"]
            self.assertEqual(services["web"]["restart"], "unless-stopped")
            self.assertEqual(services["seed"]["restart"], "no")

    def test_refuse_source_mounts_in_review_and_shared_data(self):
        cases = [
            {"services": {"web": {"volumes": [{"type": "bind", "source": "/tmp"}]}}},
            {"services": {"web": {"ports": [{"published": "8000"}]}}},
            {"services": {"web": {}}, "volumes": {"data": {"name": "shared-data"}}},
            {"services": {"web": {}}, "networks": {"db": {"external": True}}},
        ]
        for config in cases:
            with self.subTest(config=config), patch.object(dbx, "run", return_value=json.dumps(config)):
                with self.assertRaises(ValueError):
                    dbx.render(KIT / "project-template", {"project": "demo"}, "review", self.env(), {})


class Lifecycle(unittest.TestCase):
    def test_stop_preserves_and_destroy_removes_volumes(self):
        with tempfile.TemporaryDirectory() as temp:
            common = Path(temp)
            path = common / "devbox/runs/demo-live-123/compose.json"
            path.parent.mkdir(parents=True)
            path.write_text("{}")
            with patch.object(dbx, "repository", return_value=(common, common)), \
                 patch.object(dbx, "assert_owner"), patch.object(dbx, "compose") as compose:
                dbx.lifecycle("stop", "demo-live-123")
                compose.assert_called_with(path, "stop")
                self.assertTrue(path.exists())
                dbx.lifecycle("destroy", "demo-live-123")
                compose.assert_called_with(path, "down", "--volumes", "--remove-orphans")
                self.assertFalse(path.exists())

    def test_no_argument_resolves_current_worktree(self):
        with tempfile.TemporaryDirectory() as temp:
            root, other, common = Path(temp, "wt"), Path(temp, "other"), Path(temp, ".git")
            saved = {"demo-a-live-1": {"devbox.mode": "live", "devbox.worktree": str(root)},
                     "demo-b-live-2": {"devbox.mode": "live", "devbox.worktree": str(other)},
                     "demo-old-live-3": {"devbox.mode": "live", "devbox.branch": "old"},
                     "demo-abc-review-4": {"devbox.mode": "review"}}
            trees = {str(root): "a", str(other): "b"}
            with patch.object(dbx, "saved_previews", return_value=saved), \
                 patch.object(dbx, "worktrees", return_value=trees):
                self.assertEqual(dbx.current_preview(root, common), "demo-a-live-1")
                saved["demo-abc-review-5"] = {"devbox.mode": "review", "devbox.worktree": str(root)}
                with self.assertRaisesRegex(ValueError, "demo-a-live-1.*\n.*demo-abc-review-5"):
                    dbx.current_preview(root, common)
                # A legacy preview without devbox.worktree is found through its branch.
                trees[str(root)] = "old"
                del saved["demo-a-live-1"], saved["demo-abc-review-5"]
                self.assertEqual(dbx.current_preview(root, common), "demo-old-live-3")
                trees[str(root)] = "elsewhere"
                with self.assertRaisesRegex(ValueError, "No preview"):
                    dbx.current_preview(root, common)

    def test_gc_only_removes_previews_of_removed_worktrees(self):
        with tempfile.TemporaryDirectory() as temp:
            common = Path(temp)
            saved = {"demo-kept-live-1": {"devbox.mode": "live", "devbox.worktree": "/wt/kept"},
                     "demo-gone-live-2": {"devbox.mode": "live", "devbox.worktree": "/wt/gone"},
                     "demo-gone-review-3": {"devbox.mode": "review", "devbox.worktree": "/wt/gone"},
                     "demo-old-live-4": {"devbox.mode": "live", "devbox.branch": "deleted"},
                     "demo-old-review-5": {"devbox.mode": "review"}}
            with patch.object(dbx, "repository", return_value=(common, common)), \
                 patch.object(dbx, "saved_previews", return_value=saved), \
                 patch.object(dbx, "worktrees", return_value={"/wt/kept": "kept"}), \
                 patch.object(dbx, "assert_owner"), patch.object(dbx, "destroy") as destroy:
                dbx.gc(apply=False)
                destroy.assert_not_called()
                dbx.gc(apply=True)
            removed = {call.args[0].parent.name for call in destroy.call_args_list}
            self.assertEqual(removed, {"demo-gone-live-2", "demo-gone-review-3", "demo-old-live-4"})

    def test_refuse_another_clone(self):
        container = {"Config": {"Labels": {"devbox.repository": "/other/.git"}}}
        with patch.object(dbx, "containers_for", return_value=[container]):
            with self.assertRaises(ValueError):
                dbx.assert_owner("demo-live", Path("/mine/.git"))


class WebOverview(unittest.TestCase):
    def container(self, service, running, health, web=False):
        labels = {"devbox.preview": "demo-live-1", "devbox.mode": "live", "devbox.project": "demo",
                  "com.docker.compose.service": service}
        if web:
            labels["traefik.enable"] = "true"
        return {"Id": service, "Config": {"Labels": labels},
                "State": {"Running": running, "Status": "running" if running else "exited",
                          "StartedAt": "2026-01-01T00:00:00Z", "Health": {"Status": health}}}

    def test_stopped_preview_reports_state_not_stale_health(self):
        with patch.object(dbx, "docker_containers", return_value=[
                self.container("web", False, "unhealthy", web=True), self.container("db", True, "healthy")]), \
             patch.object(dbx, "host_facts", return_value={}):
            (preview,) = dbx.overview()["previews"]
        self.assertEqual(preview["status"], "exited")
        self.assertIsNone(preview["started"])
        self.assertEqual({s["name"]: s["status"] for s in preview["services"]}, {"web": "exited", "db": "healthy"})

    def test_logs_reject_anything_but_a_preview_id(self):
        for name in ("", "../etc", "a b", "x;rm"):
            with self.assertRaises(ValueError):
                dbx.preview_logs(name)


class GitIntegration(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name) / "repo"
        shutil.copytree(KIT / "project-template", self.root)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Devbox test")
        self.git("config", "user.email", "test@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.hooksPath", "/dev/null")
        self.git("add", ".")
        self.git("commit", "-m", "sample")
        self.commit = self.git("rev-parse", "HEAD")
        self.common = self.root / ".git"

    def tearDown(self):
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.check_output(["git", "-C", self.root, *args], text=True, stderr=subprocess.DEVNULL).strip()

    def test_review_uses_committed_files_and_removes_checkout(self):
        original = (self.root / "message.txt").read_text()
        (self.root / "message.txt").write_text("Uncommitted live change")
        observed = []

        def recipe(root, cfg, name, env):
            observed.append((root / "message.txt").read_text())
            self.assertEqual(env["DEVBOX_COMMIT"], self.commit)
            return True

        def render(root, cfg, mode, env, metadata):
            return {"services": {"web": {"labels": metadata}}}

        with patch.object(dbx, "repository", return_value=(self.root, self.common)), \
             patch.object(dbx, "assert_owner"), patch.object(dbx, "hook", side_effect=recipe), \
             patch.object(dbx, "render", side_effect=render), patch.object(dbx, "compose"), \
             patch.dict(os.environ, DEV_DOMAIN="dev.example.com"):
            dbx.prepare_preview("review", self.commit)
        self.assertTrue(observed)
        self.assertTrue(all(value == original for value in observed))
        self.assertEqual((self.root / "message.txt").read_text(), "Uncommitted live change")
        self.assertEqual(self.git("worktree", "list", "--porcelain").count("worktree "), 1)

    def test_hooks_run_from_devbox_json_and_missing_ones_are_skipped(self):
        cfg = {"hooks": {"check": 'echo "$DEVBOX_COMMIT" > hook.out'}}
        self.assertTrue(dbx.hook(self.root, cfg, "check", {**os.environ, "DEVBOX_COMMIT": "abc"}))
        self.assertEqual((self.root / "hook.out").read_text(), "abc\n")
        self.assertFalse(dbx.hook(self.root, cfg, "prepare", os.environ))
        with self.assertRaises(subprocess.CalledProcessError):
            dbx.hook(self.root, {"hooks": {"check": "false"}}, "check", os.environ)

    def test_worktrees_skip_removed_directories(self):
        kept, removed = Path(self.temp.name) / "kept", Path(self.temp.name) / "removed"
        self.git("worktree", "add", "-b", "kept", kept)
        self.git("worktree", "add", "--detach", removed)
        locked = Path(self.temp.name) / "locked"
        self.git("worktree", "add", "--lock", "-b", "locked", locked)
        shutil.rmtree(removed)
        shutil.rmtree(locked)
        self.assertEqual(dbx.worktrees(self.root),
                         {str(self.root): "main", str(kept): "kept", str(locked): "locked"})

    def test_existing_checkout_is_never_removed_on_failure(self):
        name = dbx.preview_name("demo", self.commit, "review")
        checkout = self.common / "devbox/checkouts" / name
        checkout.mkdir(parents=True)
        sentinel = checkout / "keep"
        sentinel.write_text("existing work")
        with patch.object(dbx, "repository", return_value=(self.root, self.common)), \
             patch.object(dbx, "assert_owner"):
            with self.assertRaises(ValueError):
                dbx.prepare_preview("review", self.commit)
        self.assertEqual(sentinel.read_text(), "existing work")


if __name__ == "__main__":
    unittest.main()
