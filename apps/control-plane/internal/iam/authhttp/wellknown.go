// Package authhttp serves the control plane's authorization server over
// plain HTTP: its well-known documents, the OAuth endpoints, and the sign-in
// pages. OAuth's form posts, redirects, and error format do not fit the
// OpenAPI operations under /v1, so these are mounted beside them.
package authhttp

import (
	"net/http"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
)

// JWKSPath serves the public keys that verify access tokens.
const JWKSPath = "/.well-known/jwks.json"

type wellKnown struct {
	jwks []byte
}

// NewWellKnown serves the key set at JWKSPath, so resource servers outside
// the control plane can verify tokens offline.
func NewWellKnown(keys token.KeyRing) api.Handlers {
	return &wellKnown{jwks: keys.JWKS()}
}

func (h *wellKnown) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET "+JWKSPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwk-set+json")
		// Verifiers may cache the keys briefly; a rotated-in key is published
		// before it signs, so a short cache never misses one.
		w.Header().Set("Cache-Control", "max-age=300")
		_, _ = w.Write(h.jwks)
	})
}
