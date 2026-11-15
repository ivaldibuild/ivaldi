//go:build !webui

package webui

import "io/fs"

// Embedded returns nil: a build without -tags webui carries no web app, which
// is every build except the release image's. `make local_serve` serves a build
// from --web-dir instead, and `ng serve` serves the sources.
func Embedded() fs.FS { return nil }
