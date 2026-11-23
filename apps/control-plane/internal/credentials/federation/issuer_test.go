package federation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/token"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// A JWT a store verifies with the published keys, naming the Deployment it
// is for under the store's own audience.
func TestIssuerSignsWhatTheStoreVerifies(t *testing.T) {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	clock.Fix(t, now)
	keys, err := token.NewEphemeralKeyRing()
	require.NoError(t, err)
	iss, err := NewIssuer(keys, "https://ivaldi.example.com/")
	require.NoError(t, err)
	assert.Equal(t, "https://ivaldi.example.com/federation", iss.URL())

	grant := Grant{
		OrganizationID: "org_a", WorkspaceID: "wsp_a", ProjectID: "prj_a", ApplicationID: "app_a",
		EnvironmentID: "env_a", EnvironmentTier: "production", TargetID: "tgt_a", ClusterID: "cls_a",
		DeploymentID: "dpl_a", ReleaseID: "rel_a", StoreID: "sst_a",
	}
	tok, err := iss.Issue(grant)
	require.NoError(t, err)
	assert.Equal(t, now.Add(5*time.Minute), tok.ExpiresAt)

	mux := http.NewServeMux()
	h, err := NewHandlers(keys, "https://ivaldi.example.com")
	require.NoError(t, err)
	h.RegisterHandlers(mux)
	get := func(path string) []byte {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path)
		return rec.Body.Bytes()
	}
	var doc discovery
	require.NoError(t, json.Unmarshal(get("/federation/.well-known/openid-configuration"), &doc))
	assert.Equal(t, iss.URL(), doc.Issuer, "a store compares the discovery document's issuer with the JWT's")
	assert.Equal(t, "https://ivaldi.example.com/federation/jwks.json", doc.JWKSURI)

	var set jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(get("/federation/jwks.json"), &set))
	jws, err := jose.ParseSigned(tok.Value, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	assert.Equal(t, "JWT", jws.Signatures[0].Protected.ExtraHeaders["typ"], "not an access token's type")
	keysFor := set.Key(jws.Signatures[0].Protected.KeyID)
	require.Len(t, keysFor, 1)
	payload, err := jws.Verify(keysFor[0])
	require.NoError(t, err)

	var c Claims
	require.NoError(t, json.Unmarshal(payload, &c))
	assert.Equal(t, "https://ivaldi.example.com/federation", c.Issuer)
	assert.Equal(t, "org:org_a:store:sst_a:env:env_a", c.Subject)
	assert.Equal(t, []string{"ivaldi:sst_a"}, c.Audience)
	assert.Equal(t, now.Unix(), c.IssuedAt)
	assert.Equal(t, now.Add(5*time.Minute).Unix(), c.ExpiresAt)
	assert.Equal(t, tok.ID, c.ID)
	assert.Equal(t, "dpl_a", c.Deployment)
	assert.Equal(t, "production", c.EnvironmentTier)

	_, err = iss.Issue(Grant{StoreID: "sst_a"})
	assert.Error(t, err, "a grant names what it is for")
	_, err = NewIssuer(keys, "ivaldi.example.com")
	assert.Error(t, err, "the issuer is an absolute URL")
}
