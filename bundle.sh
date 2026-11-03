#!/bin/sh
# Build a release bundle for a Linux host: bin/devbox-linux-<arch>.tar.gz with the devbox
# binary, bootstrap.sh, the project template, README.md and LICENSE.
#   ./bundle.sh [amd64|arm64]   (default: amd64)
set -eu
arch=${1:-amd64}
cd "$(dirname "$0")"
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
mkdir "$staging/devbox"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$staging/devbox/devbox" ./cmd/devbox
cp -R bootstrap.sh README.md LICENSE project-template "$staging/devbox/"
mkdir -p bin
tar -C "$staging" -czf "bin/devbox-linux-$arch.tar.gz" devbox
echo "bin/devbox-linux-$arch.tar.gz ($(git rev-parse --short HEAD)$(git diff --quiet HEAD || echo ', uncommitted changes'))"
