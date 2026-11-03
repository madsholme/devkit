#!/bin/sh
# Install what devkit needs from the system on a fresh Ubuntu, Debian or Fedora host
# (Ubuntu and Debian derivatives such as Mint or Pop!_OS too): Git, Docker Engine with
# Compose v2, Tailscale, the docker group, the firewall for the tailnet and lingering.
# Run it as the user who will run devkit (not root); it uses sudo where needed.
# Safe to run again: each step is skipped when it is already done.
set -eu

say() { printf '\n==> %s\n' "$*"; }
die() { printf 'bootstrap: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -ne 0 ] || die "run this as the user who will run devkit, not as root; it uses sudo itself"
command -v sudo >/dev/null || die "sudo is missing"
[ -r /etc/os-release ] || die "cannot read /etc/os-release"
# shellcheck source=/dev/null
. /etc/os-release
# family is the package manager; docker_os the distro Docker's repository is named after.
case "$ID ${ID_LIKE:-}" in
fedora*) family=dnf docker_os=fedora ;;
ubuntu* | *" ubuntu"*) family=apt docker_os=ubuntu ;;
debian* | *" debian"*) family=apt docker_os=debian ;;
*) die "only Ubuntu, Debian (and their derivatives) and Fedora are supported, not $ID; install Docker, Tailscale and lingering by hand (see README.md)" ;;
esac
[ -d /run/systemd/system ] || die "systemd is not running; devkit needs systemd user services"
user=$(id -un)
if [ "$family" = apt ]; then
	# Derivatives name their own release; Docker's repository knows the base one.
	codename=${UBUNTU_CODENAME:-${DEBIAN_CODENAME:-${VERSION_CODENAME:-}}}
	[ -n "$codename" ] || die "/etc/os-release names no release codename"
fi

say "Base packages"
if [ "$family" = apt ]; then
	sudo apt-get update -qq
	sudo apt-get install -y -qq ca-certificates curl git
else
	sudo dnf install -y -q ca-certificates curl git
fi

say "Docker Engine and Compose v2"
if docker compose version >/dev/null 2>&1 || sudo docker compose version >/dev/null 2>&1; then
	echo "already installed: $(sudo docker --version)"
else
	# Docker's own repository (https://docs.docker.com/engine/install/).
	packages="docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin"
	if [ "$family" = apt ]; then
		sudo install -m 0755 -d /etc/apt/keyrings
		sudo curl -fsSL "https://download.docker.com/linux/$docker_os/gpg" -o /etc/apt/keyrings/docker.asc
		sudo chmod a+r /etc/apt/keyrings/docker.asc
		sudo tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/$docker_os
Suites: $codename
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
		sudo apt-get update -qq
		# shellcheck disable=SC2086
		sudo apt-get install -y -qq $packages
	else
		sudo curl -fsSL "https://download.docker.com/linux/$docker_os/docker-ce.repo" -o /etc/yum.repos.d/docker-ce.repo
		# shellcheck disable=SC2086
		sudo dnf install -y -q $packages
	fi
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

say "Firewall"
# Traefik (443) and media ports listen on the Tailscale IP; let tailnet traffic in.
# Who may connect is up to your tailnet's access controls.
if command -v firewall-cmd >/dev/null && sudo firewall-cmd --state >/dev/null 2>&1; then
	if sudo firewall-cmd --permanent --zone=trusted --query-interface=tailscale0 >/dev/null 2>&1; then
		echo "firewalld already trusts tailscale0"
	else
		sudo firewall-cmd --permanent --zone=trusted --change-interface=tailscale0 >/dev/null
		sudo firewall-cmd --reload >/dev/null
		echo "firewalld now trusts tailscale0"
	fi
elif command -v ufw >/dev/null && sudo ufw status | grep -q '^Status: active'; then
	if sudo ufw status | grep -q 'on tailscale0'; then
		echo "ufw already allows tailscale0"
	else
		sudo ufw allow in on tailscale0 >/dev/null
		echo "ufw now allows tailscale0"
	fi
else
	echo "no active firewalld or ufw"
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
if [ -z "$(git config --global user.email 2>/dev/null)" ]; then
	echo "Git has no identity yet; commits need one:"
	echo "  git config --global user.name 'Your Name' && git config --global user.email you@example.com"
fi
here=$(cd "$(dirname "$0")" && pwd)
if [ -x "$here/devkit" ]; then
	# A release bundle: the binary sits next to this script.
	cat <<EOF
Next, install devkit and configure the host:

  install -D "$here/devkit" ~/.local/bin/devkit
  devkit setup

If devkit is not found, log in again: most distributions add ~/.local/bin to PATH at login.
EOF
else
	cat <<EOF
Next, build devkit in this checkout and configure the host (see README.md, Host setup):

  cd "$here"
  curl https://mise.run | sh                # if you have no Go yet; mise.toml pins it
  mise trust && mise install && mise exec -- go build -o bin/devkit ./cmd/devkit
  install -d ~/.local/bin && ln -sf "\$PWD/bin/devkit" ~/.local/bin/devkit
  devkit setup
EOF
fi
