package grants

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
)

// GrantView is access to one store. Its JWT is a credential: the agent
// masks it, uses it once, and never stores or reports it.
type GrantView struct {
	StoreID   string    `json:"store_id"`
	Mode      string    `json:"mode" enum:"federated"`
	ExpiresAt time.Time `json:"expires_at"`
	// JWT is set for a federated grant: the agent exchanges it at the store.
	JWT string `json:"jwt,omitempty"`
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
	}
	return &GrantResponse{Body: api.SuccessResponse(nil, out)}
}
