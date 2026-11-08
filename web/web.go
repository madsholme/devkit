// Package web holds the dashboard page and its icons, built into the devkit binary.
package web

import "embed"

//go:embed dist logo.svg favicon.ico apple-touch-icon.png
var Files embed.FS
