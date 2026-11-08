// Package projecttemplate embeds only the starter files, not this implementation.
package projecttemplate

import "embed"

// Files includes dotfiles explicitly so standalone Go builds can create projects.
//
//go:embed .dockerignore .gitignore AGENTS.md CLAUDE.md Dockerfile app.py compose.live.yaml compose.review.yaml devkit.json message.txt t3.json
var Files embed.FS
