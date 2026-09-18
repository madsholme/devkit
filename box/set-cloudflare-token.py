#!/usr/bin/env python3
"""Enter the Cloudflare credential directly on the host, without echoing it."""
import getpass
import os
from pathlib import Path

directory = Path(__file__).resolve().parent / "traefik"
env_file = directory / ".env"
if not env_file.exists():
    raise SystemExit("Configure box/traefik/.env before storing the token.")
print("Token permissions: Zone:DNS:Edit and Zone:Zone:Read, scoped to steenfeldt.net.")
token = getpass.getpass("Cloudflare API token (hidden): ").strip()
email = input("Email for the Let's Encrypt account: ").strip()
if not token or any(c.isspace() for c in token) or "@" not in email or any(c in email for c in "\r\n$#\"'"):
    raise SystemExit("Token or email format is invalid; nothing written.")
credential = directory / "dns-provider.env"
fd = os.open(credential, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w") as stream:
    stream.write("CF_DNS_API_TOKEN=" + token + "\n")
os.chmod(credential, 0o600)
lines = [line for line in env_file.read_text().splitlines() if not line.startswith("ACME_EMAIL=")]
lines.append("ACME_EMAIL=" + email)
env_file.write_text("\n".join(lines) + "\n")
os.chmod(env_file, 0o600)
print("Credential saved privately. No DNS records changed and no services started.")
