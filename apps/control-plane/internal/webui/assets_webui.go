//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

// dist is the web app's build output, which apps/control-plane/Dockerfile
// places here before compiling with -tags webui. A build without the tag does
// not need it, so `go build` and the tests never require pnpm.
//
//go:embed dist
var dist embed.FS

// Embedded returns the web app this binary carries, or nil when the build did
// not include it.
func Embedded() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
