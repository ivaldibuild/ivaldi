// Package webui serves the web app the control plane ships with.
//
// The web app and the API share one origin, so the control plane serves its
// files: no separate web server, no CORS, and no reverse proxy stitching two
// origins together.
package webui

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

// hashedAsset matches the build's content-hashed files, such as
// main-4F2A9C1D.js. Their names change with their contents, so they can be
// cached forever.
var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9]{8}\.(?:js|css)$`)

// handler serves a build of the web app.
type handler struct {
	fsys  fs.FS
	files http.Handler
	index []byte
	// indexModified is index.html's modification time, which a conditional
	// request is answered from. A zero time disables revalidation by date.
	indexModified time.Time
}

// Handler serves fsys: a matching file when there is one, and index.html
// otherwise, because every other path is a route of the single-page app.
//
// A missing file is a 404 rather than index.html: answering a renamed asset
// with HTML would fail in the browser as a syntax error, which reads as a
// server fault.
func Handler(fsys fs.FS) (http.Handler, error) {
	info, err := fs.Stat(fsys, "index.html")
	if err != nil {
		return nil, fmt.Errorf("webui: the build has no index.html: %w", err)
	}
	index, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, fmt.Errorf("webui: read index.html: %w", err)
	}
	return &handler{
		fsys:          fsys,
		files:         http.FileServer(http.FS(fsys)),
		index:         index,
		indexModified: info.ModTime(),
	}, nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean rejects ../ before it reaches the file system.
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")

	if name != "" {
		if info, err := fs.Stat(h.fsys, name); err == nil && !info.IsDir() {
			w.Header().Set("Cache-Control", cacheControl(name))
			h.files.ServeHTTP(w, r)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
	}

	h.serveIndex(w, r)
}

// serveIndex answers a route of the single-page app. index.html is revalidated
// on every load, so a new release is picked up without a hard refresh.
func (h *handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", h.indexModified, bytes.NewReader(h.index))
}

// cacheControl is the header the web app's files get. Only content-named files
// are immutable; the rest, index.html above all, are revalidated.
func cacheControl(name string) string {
	if strings.HasPrefix(name, "media/") || hashedAsset.MatchString(name) {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}
