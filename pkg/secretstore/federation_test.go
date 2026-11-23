package secretstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The JWT is traded at the identity for a token, which then reads the
// config; a refusal is the store's, not a failure worth retrying.
func TestDopplerOIDC(t *testing.T) {
	var sent map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/auth/oidc", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		if sent["identity"] != "said_1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "token": "dp.said.token"})
	}))
	t.Cleanup(server.Close)

	token, err := DopplerOIDC(context.Background(), nil, server.URL, "said_1", "eyJ.jwt")
	require.NoError(t, err)
	assert.Equal(t, "dp.said.token", token)
	assert.Equal(t, map[string]string{"identity": "said_1", "token": "eyJ.jwt"}, sent)

	_, err = DopplerOIDC(context.Background(), nil, server.URL, "said_other", "eyJ.jwt")
	require.ErrorIs(t, err, ErrDenied)
	assert.False(t, Transient(err))
}
