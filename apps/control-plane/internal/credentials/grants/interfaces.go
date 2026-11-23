package grants

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Service grants the Secret Store access a Deployment needs.
type Service interface {
	// Grant grants access to every store in a mode that needs a grant
	// among those the Deployment's secrets are in, or to none: the caller
	// must be the agent of the Cluster the Deployment's Target is in, and
	// the Deployment applying. It returns no grant for a Deployment whose
	// stores need none.
	Grant(ctx context.Context, caller identity.Caller, deploymentID string) ([]Grant, error)
}

// Assignments reads one Deployment's assignment for its Cluster's agent,
// with its stores checked again. delivery.DeploymentService provides it.
type Assignments interface {
	GetDeploymentAssignment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*delivery.DeploymentAssignment, error)
}

// Applications reads the Application a Deployment is of, for its Project.
// delivery.ApplicationRepository provides it.
type Applications interface {
	GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error)
}
