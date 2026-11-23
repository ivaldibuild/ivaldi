package secrets

import (
	"context"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

// Resolver resolves a Deployment's secrets.
type Resolver interface {
	// Resolve reads secrets for a Deployment in namespace, fingerprints
	// them, and checks pinned fingerprints. A store in federated mode is
	// opened with access the control plane grants for the Deployment.
	// Every value is masked from then on, until Release. A failure is a
	// *deploy.Failure naming the secret and its reference, never its value,
	// or ErrNotApplying.
	Resolve(ctx context.Context, deploymentID, namespace string, secrets []apiclient.AssignmentSecret) ([]Resolved, error)
	// Release stops masking values that are no longer in use.
	Release(resolved []Resolved)
}
