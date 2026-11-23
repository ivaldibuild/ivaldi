package federation

import (
	"encoding/json"
	"net/http"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
)

type handlers struct {
	discovery []byte
	jwks      []byte
}

// NewHandlers serves the issuer's discovery document and keys, which stores
// such as AWS and Doppler fetch to verify the JWTs.
func NewHandlers(keys token.KeyRing, externalURL string) (api.Handlers, error) {
	iss, err := IssuerURL(externalURL)
	if err != nil {
		return nil, err
	}
	var algorithms []string
	for _, k := range keys.PublicKeys() {
		algorithms = append(algorithms, k.Algorithm)
	}
	doc, err := json.Marshal(discovery{
		Issuer:                           iss,
		JWKSURI:                          iss + "/jwks.json",
		ResponseTypesSupported:           []string{"id_token"},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: algorithms,
		ClaimsSupported: []string{
			"iss", "sub", "aud", "iat", "nbf", "exp", "jti", "org", "workspace", "project", "application",
			"environment", "environment_tier", "target", "cluster", "deployment", "release",
		},
	})
	if err != nil {
		return nil, err
	}
	return &handlers{discovery: doc, jwks: keys.JWKS()}, nil
}

func (h *handlers) RegisterHandlers(mux *http.ServeMux) {
	// Stores cache both; a key is published before it signs, so a short
	// cache never misses one.
	serve := func(contentType string, body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "max-age=300")
			_, _ = w.Write(body)
		}
	}
	mux.HandleFunc("GET "+DiscoveryPath, serve("application/json", h.discovery))
	mux.HandleFunc("GET "+JWKSPath, serve("application/jwk-set+json", h.jwks))
}
