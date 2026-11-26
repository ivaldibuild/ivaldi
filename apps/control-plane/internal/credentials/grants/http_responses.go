package grants

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
)

// GrantView is access to one store. Its JWT or credential is a credential:
// the agent masks it, uses it for one Deployment's reads, and never stores
// or reports it.
type GrantView struct {
	StoreID   string    `json:"store_id"`
	Mode      string    `json:"mode" enum:"federated,brokered"`
	ExpiresAt time.Time `json:"expires_at"`
	// JWT is set for a federated grant: the agent exchanges it at the store.
	JWT string `json:"jwt,omitempty"`
	// Credential is set for a brokered grant: the agent reads with it as it
	// is.
	Credential *BrokeredCredentialView `json:"credential,omitempty"`
}

// BrokeredCredentialView is a credential minted for one Deployment: a token
// for doppler, vault, and infisical, and session credentials for
// aws_secrets_manager.
type BrokeredCredentialView struct {
	Token           string `json:"token,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
}

type GrantsView struct {
	Grants []GrantView `json:"grants"`
}

type GrantResponse struct {
	Body api.Response[GrantsView]
}

func grantsResponse(grants []Grant) *GrantResponse {
	out := GrantsView{Grants: make([]GrantView, len(grants))}
	for i, g := range grants {
		out.Grants[i] = GrantView{StoreID: g.StoreID, Mode: string(g.Mode), ExpiresAt: g.ExpiresAt, JWT: g.JWT}
		if c := g.Credential; c != nil {
			out.Grants[i].Credential = &BrokeredCredentialView{
				Token: c.Token, AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, SessionToken: c.SessionToken,
			}
		}
	}
	return &GrantResponse{Body: api.SuccessResponse(nil, out)}
}
