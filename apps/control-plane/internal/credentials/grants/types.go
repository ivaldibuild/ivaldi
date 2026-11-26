package grants

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
)

// Grant is access to one store, for one Deployment: a JWT for a federated
// store, or a credential minted for a brokered one. Both are credentials:
// they go to the agent and nowhere else.
type Grant struct {
	StoreID    string
	Mode       credentials.AccessMode
	ExpiresAt  time.Time
	JWT        string
	Credential *credentials.BrokeredCredential
}
