package authhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
)

func TestJWKS(t *testing.T) {
	keys, err := token.NewEphemeralKeyRing()
	require.NoError(t, err)
	mux := http.NewServeMux()
	NewWellKnown(keys).RegisterHandlers(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, iam.JWKSPath, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/jwk-set+json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, string(keys.JWKS()), rec.Body.String())
	assert.Contains(t, rec.Body.String(), keys.PublicKeys()[0].KeyID)
}
