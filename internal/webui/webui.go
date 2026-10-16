// Package webui embeds the compiled SolidJS interface into the binary.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Files returns the compiled web UI rooted at its index.html.
func Files() fs.FS {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		// Unreachable: "dist" is a literal embedded directory.
		panic(err)
	}
	return files
}
