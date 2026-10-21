// Package ebotweb embeds the templates and static assets.
package ebotweb

import "embed"

//go:embed all:web
var FS embed.FS
