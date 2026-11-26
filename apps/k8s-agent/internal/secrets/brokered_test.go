package secrets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

// A brokered store is read with the credential the control plane minted,
// as it is, with no login of the agent's own; the credential is masked
// while it is used, and no longer once the values are read.
func TestResolver_ReadsBrokeredStoresWithTheirCredential(t *testing.T) {
	var r *resolver
	var masker *Masker
	var posts int
	var maskedDuringRead string
	vault := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			posts++
		}
		require.Equal(t, "hvs.minted", req.Header.Get("X-Vault-Token"))
		maskedDuringRead = masker.Mask("hvs.minted")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"data": map[string]any{"key": "s3cret-api-key"}, "metadata": map[string]any{"version": 2},
		}})
	}))
	t.Cleanup(vault.Close)
	r, masker, _ = newTestResolver(t, fake.NewClientset())
	r.cfg.Addresses = []string{vault.URL}
	r.cfg.Grants = func(context.Context, string) ([]apiclient.GrantView, error) {
		return []apiclient.GrantView{{StoreId: "sst_vault", Mode: apiclient.GrantViewModeBrokered,
			Credential: &apiclient.BrokeredCredentialView{Token: new("hvs.minted")}}}, nil
	}
	secret := apiclient.AssignmentSecret{
		Name: "Api.Key", DeliverAs: apiclient.AssignmentSecretDeliverAsSecret,
		Store: apiclient.StoreSettings{Id: "sst_vault", Kind: apiclient.StoreSettingsKindVault,
			Access: apiclient.StoreAccess{Mode: apiclient.StoreAccessModeBrokered},
			Vault:  &apiclient.VaultStore{Address: vault.URL, Mount: "kv"}},
		Ref: apiclient.SecretRef{Path: "api", Field: new("key")},
	}

	resolved, err := r.Resolve(t.Context(), "dpl_a", "payments", []apiclient.AssignmentSecret{secret})
	require.NoError(t, err)
	assert.Equal(t, "s3cret-api-key", resolved[0].Value)
	assert.Zero(t, posts, "no login: the token is used as it is")
	assert.NotContains(t, maskedDuringRead, "hvs.minted", "masked while it is used")
	assert.Equal(t, "hvs.minted", masker.Mask("hvs.minted"), "and no longer once the values are read")

	r.cfg.Grants = func(context.Context, string) ([]apiclient.GrantView, error) { return nil, nil }
	_, err = r.Resolve(t.Context(), "dpl_a", "payments", []apiclient.AssignmentSecret{secret})
	assert.ErrorContains(t, err, "granted no access to secret store sst_vault, which is in brokered mode")
}
