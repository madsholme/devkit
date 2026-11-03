#!/bin/sh
# Build a release bundle for a Linux host: bin/devkit-linux-<arch>.tar.gz with the devkit
# binary, bootstrap.sh, the project template, README.md and LICENSE.
#   ./bundle.sh [amd64|arm64]   (default: amd64)
set -eu
arch=${1:-amd64}
cd "$(dirname "$0")"
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
mkdir "$staging/devkit"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags='-s -w' -o "$staging/devkit/devkit" ./cmd/devkit
cp -R bootstrap.sh README.md LICENSE project-template "$staging/devkit/"
mkdir -p bin
tar -C "$staging" -czf "bin/devkit-linux-$arch.tar.gz" devkit
echo "bin/devkit-linux-$arch.tar.gz ($(git rev-parse --short HEAD)$(git diff --quiet HEAD || echo ', uncommitted changes'))"
