package workloadauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
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
	return signKubernetesTokenWithKID(t, key, "key-1", claims)
}

func signKubernetesTokenWithKID(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", kid))
	require.NoError(t, err)
	jws, err := signer.Sign(payload)
	require.NoError(t, err)
	token, err := jws.CompactSerialize()
	require.NoError(t, err)
	return token
}

func TestKubernetesVerifierPicksUpRotatedKeys(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	clock.Fix(t, now)
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var issuer string
	var mu sync.Mutex
	published := map[string]*rsa.PrivateKey{"key-1": oldKey}
	keyFetches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": issuer + "/keys"})
		case "/keys":
			keyFetches++
			var set jose.JSONWebKeySet
			for kid, k := range published {
				set.Keys = append(set.Keys, jose.JSONWebKey{Key: &k.PublicKey, KeyID: kid, Algorithm: string(jose.RS256), Use: "sig"})
			}
			_ = json.NewEncoder(w).Encode(set)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	verifier, err := NewKubernetesVerifier([]string{issuer}, "ivaldi", server.Client())
	require.NoError(t, err)
	claims := map[string]any{
		"iss": issuer, "sub": "system:serviceaccount:default:agent", "aud": "ivaldi",
		"exp": now.Add(time.Hour).Unix(),
	}

	_, err = verifier.Verify(context.Background(), signKubernetesTokenWithKID(t, oldKey, "key-1", claims))
	require.NoError(t, err)
	require.Equal(t, 1, keyFetches)

	mu.Lock()
	published = map[string]*rsa.PrivateKey{"key-2": newKey}
	mu.Unlock()
	rotated := signKubernetesTokenWithKID(t, newKey, "key-2", claims)

	_, err = verifier.Verify(context.Background(), rotated)
	assert.ErrorIs(t, err, identity.ErrInvalidToken, "keys fetched moments ago are not fetched again")
	assert.Equal(t, 1, keyFetches)

	clock.Fix(t, now.Add(2*time.Minute))
	_, err = verifier.Verify(context.Background(), rotated)
	require.NoError(t, err, "an unknown key ID refreshes keys older than the refresh interval")
	assert.Equal(t, 2, keyFetches)
}
