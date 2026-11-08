#!/usr/bin/env python3
"""Validate shipped agent assets in a disposable home; no agent processes are launched.
Fresh Codex/Claude discovery is a separate manual check documented in docs/testing.md.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("--devkit", required=True, type=Path)
args = parser.parse_args()
cli = args.devkit.resolve()
with tempfile.TemporaryDirectory(prefix="devkit-agent-assets-") as directory:
    root = Path(directory)
    env = dict(os.environ, HOME=str(root), DEVKIT_CONFIG_DIR=str(root / "config"), DEVKIT_STATE_DIR=str(root / "state"))
    def run(*arguments):
        return subprocess.run([str(cli), *arguments], env=env, check=True, capture_output=True, text=True).stdout
    run("agents", "skills", "install")
    result = json.loads(run("agents", "skills", "status", "--json"))
    assert result == {"codex": "current", "claude": "current"}, result
    codex = root / ".agents/skills/devkit-secrets/SKILL.md"
    claude = root / ".claude/skills/devkit-secrets/SKILL.md"
    assert codex.read_bytes() == claude.read_bytes()
    codex.write_text(codex.read_text() + "\nLocal instructions must survive.\n")
    repair = subprocess.run([str(cli), "agents", "skills", "repair"], env=env, capture_output=True)
    assert repair.returncode != 0
    assert "Local instructions must survive." in codex.read_text()
print("PASS: embedded skills install identically and preserve local edits in a disposable home")
print("PENDING: fresh Codex/Claude session discovery (not exercised by this asset check)")
