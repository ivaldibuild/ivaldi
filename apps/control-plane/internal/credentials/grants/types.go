package grants

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
)

// Grant is access to one store, for one Deployment. JWT is a credential:
// it goes to the agent and nowhere else.
type Grant struct {
	StoreID   string
	Mode      credentials.AccessMode
	ExpiresAt time.Time
	JWT       string
}
