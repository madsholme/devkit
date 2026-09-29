// Package web holds the dashboard page and its icons, built into the devbox binary.
package web

import "embed"

//go:embed index.html logo.svg favicon.ico apple-touch-icon.png
var Files embed.FS
