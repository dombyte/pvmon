// Package profiles embeds the built-in device profiles into the binary (spec 4.5).
package profiles

import (
	"embed"
	"io/fs"
)

//go:embed */*.yaml
var files embed.FS

// FS returns the built-in profiles as <vendor>/<model>.yaml files.
func FS() fs.FS { return files }
