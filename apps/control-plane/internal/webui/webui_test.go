package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// build is the web app's output, as Angular writes it.
func build() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><app-root></app-root>")},
		"main-4F2A9C1D.js":     {Data: []byte("console.log('app')")},
		"styles-8B3D0E5A.css":  {Data: []byte("body{}")},
		"favicon.ico":          {Data: []byte("ico")},
		"media/logo.svg":       {Data: []byte("<svg/>")},
		"chunk-ABCD1234.js":    {Data: []byte("")},
		"assets/config.json":   {Data: []byte(`{"api":""}`)},
		"assets/nested/x.json": {Data: []byte(`{}`)},
	}
}

func TestHandlerServesTheApp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		path        string
		status      int
		body        string
		cache       string
		contentType string
	}{
		{
			name: "the app's entry point", path: "/", status: http.StatusOK,
			body: "<!doctype html>", cache: "no-cache", contentType: "text/html",
		},
		{
			name: "a route of the single-page app", path: "/clusters/eng/staging", status: http.StatusOK,
			body: "<!doctype html>", cache: "no-cache", contentType: "text/html",
		},
		{
			name: "the sign-in callback route", path: "/auth/callback", status: http.StatusOK,
			body: "<!doctype html>", cache: "no-cache", contentType: "text/html",
		},
		{
			name: "a content-hashed script", path: "/main-4F2A9C1D.js", status: http.StatusOK,
			body: "console.log", cache: "public, max-age=31536000, immutable", contentType: "text/javascript",
		},
		{
			name: "a content-hashed stylesheet", path: "/styles-8B3D0E5A.css", status: http.StatusOK,
			body: "body{}", cache: "public, max-age=31536000, immutable", contentType: "text/css",
		},
		{
			name: "an unhashed file", path: "/favicon.ico", status: http.StatusOK,
			cache: "no-cache",
		},
		{
			name: "a media file", path: "/media/logo.svg", status: http.StatusOK,
			cache: "public, max-age=31536000, immutable",
		},
		{
			name: "a nested asset", path: "/assets/nested/x.json", status: http.StatusOK,
			cache: "no-cache", contentType: "application/json",
		},
		{
			name: "a missing script is a 404, not the app", path: "/main-GONE.js", status: http.StatusNotFound,
		},
		{
			name: "a missing stylesheet is a 404", path: "/styles-GONE.css", status: http.StatusNotFound,
		},
		{
			// Clean normalizes the path inside the build, so it becomes an app
			// route rather than a file outside it.
			name: "a traversal attempt stays inside the build", path: "/../../etc/passwd",
			status: http.StatusOK, body: "<!doctype html>",
		},
	}

	handler, err := Handler(build())
	require.NoError(t, err)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			require.Equal(t, tc.status, rec.Code)
			if tc.cache != "" {
				assert.Equal(t, tc.cache, rec.Header().Get("Cache-Control"))
			}
			if tc.body != "" {
				assert.Contains(t, rec.Body.String(), tc.body)
			}
			if tc.contentType != "" {
				assert.Contains(t, rec.Header().Get("Content-Type"), tc.contentType)
			}
		})
	}
}

// A build without an entry point would 404 every route, so it is refused when
// the server starts rather than when someone opens the app.
func TestHandlerRefusesABuildWithoutIndexHTML(t *testing.T) {
	t.Parallel()

	_, err := Handler(fstest.MapFS{"main-4F2A9C1D.js": {Data: []byte("")}})
	require.Error(t, err)
}
