// Package web holds the pages terminal-share serves. static/vendor/ is the npm build's
// output (build.mjs), put there by the image's build stage; a checkout without it still
// builds, and serves the pages without the engine they load.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// Static is the page files, rooted at static/.
func Static() fs.FS {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
