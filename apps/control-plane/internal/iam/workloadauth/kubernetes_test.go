package workloadauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

func TestKubernetesVerifier(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": issuer + "/keys"})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
				Key: &key.PublicKey, KeyID: "key-1", Algorithm: string(jose.RS256), Use: "sig",
			}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL

	verifier, err := NewKubernetesVerifier([]string{issuer}, "ivaldi", server.Client())
	require.NoError(t, err)

	token := signKubernetesToken(t, key, map[string]any{
		"iss": issuer,
		"sub": "system:serviceaccount:default:agent",
		"aud": []string{"ivaldi"},
		"exp": time.Now().Add(time.Hour).Unix(),
		"nbf": time.Now().Add(-time.Minute).Unix(),
	})
	got, err := verifier.Verify(context.Background(), token)
	require.NoError(t, err)
	assert.Equal(t, issuer, got.Issuer)
	assert.Equal(t, "system:serviceaccount:default:agent", got.Subject)
	assert.Equal(t, "ivaldi", got.Audience)

	badAudience := signKubernetesToken(t, key, map[string]any{
		"iss": issuer, "sub": "system:serviceaccount:default:agent", "aud": "other",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	_, err = verifier.Verify(context.Background(), badAudience)
	assert.ErrorIs(t, err, identity.ErrInvalidToken)

	_, err = verifier.Verify(context.Background(), "not-a-jwt")
	assert.ErrorIs(t, err, identity.ErrInvalidToken)
}

func signKubernetesToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "key-1"))
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	token, err := jws.CompactSerialize()
	require.NoError(t, err)
	return token
}
