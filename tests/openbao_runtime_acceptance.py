#!/usr/bin/env python3
"""Opt-in disposable OpenBao checks; never starts the deployed devkit-vault project."""
import os
from pathlib import Path
import subprocess

if os.environ.get("DEVKIT_OPENBAO_RUNTIME_TEST") != "1":
    raise SystemExit("Set DEVKIT_OPENBAO_RUNTIME_TEST=1 on a Docker host.")
root = Path(__file__).resolve().parents[1]
raise SystemExit(subprocess.call([
    "go", "test", "./cmd/devkit", "-run", "^TestVaultRuntimeBindRestartAndSnapshot$",
    "-count=1", "-v", "-timeout=8m" if (os.environ.get("DEVKIT_OPENBAO_ENCRYPTED_TEST") == "1" or os.environ.get("DEVKIT_OPENBAO_UPGRADE_TEST") == "1") else "-timeout=4m",
], cwd=root))
