#!/bin/sh
# Install what devbox needs from the system on a fresh Ubuntu or Debian host:
# Git, Docker Engine with Compose v2, Tailscale, the docker group and lingering.
# Run it as the user who will run devbox (not root); it uses sudo where needed.
# Safe to run again: each step is skipped when it is already done.
set -eu

say() { printf '\n==> %s\n' "$*"; }
die() { printf 'bootstrap: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -ne 0 ] || die "run this as the user who will run devbox, not as root; it uses sudo itself"
command -v sudo >/dev/null || die "sudo is missing"
[ -r /etc/os-release ] || die "cannot read /etc/os-release"
# shellcheck source=/dev/null
. /etc/os-release
case "$ID" in
ubuntu | debian) ;;
*) die "only Ubuntu and Debian are supported, not $ID; install Docker, Tailscale and lingering by hand (see README.md)" ;;
esac
[ -d /run/systemd/system ] || die "systemd is not running; devbox needs systemd user services"
user=$(id -un)
codename=${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}
[ -n "$codename" ] || die "/etc/os-release names no release codename"

say "Base packages"
sudo apt-get update -qq
sudo apt-get install -y -qq ca-certificates curl git

say "Docker Engine and Compose v2"
if docker compose version >/dev/null 2>&1 || sudo docker compose version >/dev/null 2>&1; then
	echo "already installed: $(sudo docker --version)"
else
	# Docker's own apt repository (https://docs.docker.com/engine/install/).
	sudo install -m 0755 -d /etc/apt/keyrings
	sudo curl -fsSL "https://download.docker.com/linux/$ID/gpg" -o /etc/apt/keyrings/docker.asc
	sudo chmod a+r /etc/apt/keyrings/docker.asc
	sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/$ID
Suites: $codename
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
	sudo apt-get update -qq
	sudo apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
	sudo systemctl enable --now docker
fi

relogin=""
if id -nG "$user" | tr ' ' '\n' | grep -qx docker; then
	echo "$user is in the docker group"
else
	sudo usermod -aG docker "$user"
	echo "added $user to the docker group"
	relogin=yes
fi

say "Tailscale"
if command -v tailscale >/dev/null; then
	echo "already installed: $(tailscale version | head -n 1)"
else
	# Tailscale's own installer (https://tailscale.com/download/linux).
	curl -fsSL https://tailscale.com/install.sh | sh
fi
if tailscale ip -4 >/dev/null 2>&1; then
	echo "connected as $(tailscale ip -4)"
else
	echo "Log this machine in to your tailnet (open the link it prints):"
	sudo tailscale up
fi

say "Lingering"
if [ "$(loginctl show-user "$user" --property=Linger --value 2>/dev/null)" = yes ]; then
	echo "already on for $user"
else
	sudo loginctl enable-linger "$user"
	echo "on: $user's services now run without a login"
fi

say "Done"
if [ -n "$relogin" ]; then
	echo "Log out and back in (or run 'newgrp docker') so Docker works without sudo."
fi
cat <<'EOF'
Next, in this checkout, build devbox and configure the host (see README.md, Host setup):

  curl https://mise.run | sh                # if you have no Go yet; mise.toml pins it
  mise install && mise exec -- go build -o bin/devbox ./cmd/devbox
  install -d ~/.local/bin && ln -sf "$PWD/bin/devbox" ~/.local/bin/devbox
  devbox setup
EOF
