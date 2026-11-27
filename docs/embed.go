// Package docs embeds the API documentation: openapi.yaml and the Swagger UI build
// (docs/dist, built by `make docs`). Without a build, dist holds only .gitkeep and the
// server says so.
package docs

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

//go:embed openapi.yaml
var spec []byte

// FS returns the content of docs/dist.
func FS() fs.FS {
	sub, _ := fs.Sub(files, "dist") //nolint:errcheck // fails only for an invalid path
	return sub
}

// Spec returns the OpenAPI document of the source tree.
func Spec() []byte { return spec }
