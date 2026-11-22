// Package web embeds the built GUI (web/dist) into the Go binary.
package web

import "embed"

// Dist holds the Vite build output.
//
//go:embed all:dist
var Dist embed.FS
