package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

// An Infisical store is logged in to by its method, only at a site the
// operator allowed; what the login sends and gives is masked while the
// values are read, and no longer after.
func TestResolver_OpensInfisicalStores(t *testing.T) {
	const jwt = "eyJhbGciOiJFUzI1NiJ9.grant.signature"
	var r *resolver
	var masker *Masker
	var loginPath string
	var login map[string]string
	var maskedDuringRead []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			loginPath, login = req.URL.Path, nil
			require.NoError(t, json.NewDecoder(req.Body).Decode(&login))
			_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "inf.access", "expiresIn": 600, "tokenType": "Bearer"})
			return
		}
		require.Equal(t, "Bearer inf.access", req.Header.Get("Authorization"))
		assert.Equal(t, "/app", req.URL.Query().Get("secretPath"))
		maskedDuringRead = []string{masker.Mask("inf.access"), masker.Mask("client-secret")}
		_ = json.NewEncoder(w).Encode(map[string]any{"secret": map[string]any{"secretKey": "API_KEY", "secretValue": "s3cret-api-key", "version": 5}})
	}))
	t.Cleanup(server.Close)

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("infisical-audience-token\n"), 0o600))
	r, masker, _ = newTestResolver(t, fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "infisical", Namespace: "ivaldi-system"},
		Data:       map[string][]byte{"universal-auth": []byte(`{"client_id":"client-id","client_secret":"client-secret"}`)},
	}))
	r.cfg.Credentials = map[string]Credential{"infisical-production": {Secret: "infisical", Key: "universal-auth"}}
	r.cfg.InfisicalTokenFile = tokenFile
	r.cfg.Grants = func(context.Context, string) ([]apiclient.GrantView, error) {
		return []apiclient.GrantView{{StoreId: "sst_inf", Mode: "federated", Jwt: new(jwt)}}, nil
	}
	secret := func(access apiclient.StoreAccess) []apiclient.AssignmentSecret {
		return []apiclient.AssignmentSecret{{
			Name: "Api.Key", DeliverAs: apiclient.AssignmentSecretDeliverAsSecret,
			Store: apiclient.StoreSettings{Id: "sst_inf", Kind: apiclient.StoreSettingsKindInfisical, Access: access,
				Infisical: &apiclient.InfisicalStore{SiteUrl: server.URL, ProjectId: "prj", Environment: "prod", SecretPath: "/app"}},
			Ref: apiclient.SecretRef{Path: "API_KEY"},
		}}
	}
	universal := apiclient.StoreAccess{Mode: apiclient.StoreAccessModeAgent,
		Method: new(apiclient.StoreAccessMethodUniversalAuth), Credential: new("infisical-production")}

	_, err := r.Resolve(t.Context(), "dpl_a", "payments", secret(universal))
	require.ErrorContains(t, err, "not set up to log in to")
	assert.Empty(t, loginPath, "nothing is sent to a self-hosted Infisical the operator did not list")
	assert.Contains(t, r.cfg.StoreKinds(), "infisical", "Infisical Cloud needs no listing")

	r.cfg.Addresses = []string{server.URL}
	cases := []struct {
		name   string
		access apiclient.StoreAccess
		path   string
		login  map[string]string
	}{
		{"universal auth, with the named credential", universal,
			"/api/v1/auth/universal-auth/login", map[string]string{"clientId": "client-id", "clientSecret": "client-secret"}},
		{"kubernetes auth, with the projected token",
			apiclient.StoreAccess{Mode: apiclient.StoreAccessModeAgent, Method: new(apiclient.StoreAccessMethodKubernetes), IdentityId: new("idt_1")},
			"/api/v1/auth/kubernetes-auth/login", map[string]string{"identityId": "idt_1", "jwt": "infisical-audience-token"}},
		{"federated, with the granted JWT",
			apiclient.StoreAccess{Mode: apiclient.StoreAccessModeFederated, Method: new(apiclient.StoreAccessMethodOidc), IdentityId: new("idt_1")},
			"/api/v1/auth/oidc-auth/login", map[string]string{"identityId": "idt_1", "jwt": jwt}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := r.Resolve(t.Context(), "dpl_a", "payments", secret(tt.access))
			require.NoError(t, err)
			assert.Equal(t, tt.path, loginPath)
			assert.Equal(t, tt.login, login)
			assert.Equal(t, "s3cret-api-key", resolved[0].Value)
			assert.Equal(t, "5", resolved[0].Version)
			assert.NotContains(t, maskedDuringRead[0], "inf.access", "the access token is masked while it is used")
			assert.Equal(t, "inf.access", masker.Mask("inf.access"), "and no longer once the values are read")
			if tt.access.Credential != nil {
				assert.NotContains(t, maskedDuringRead[1], "client-secret", "so is the client secret")
			}
			r.Release(resolved)
		})
	}
}
