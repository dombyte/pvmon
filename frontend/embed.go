// Package frontend embeds the built web UI (frontend/dist, built by `make assets`) into the
// binary. Without a build the directory holds only .gitkeep and the server says so.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// FS returns the content of frontend/dist.
func FS() fs.FS {
	sub, _ := fs.Sub(files, "dist") //nolint:errcheck // fails only for an invalid path
	return sub
}
