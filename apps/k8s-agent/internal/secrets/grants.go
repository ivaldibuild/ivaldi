package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// grants asks the control plane for access to the federated and brokered
// stores among the secrets', and returns each store's grant by its ID. It
// asks nothing when no store needs a grant, and retries while the control
// plane is unreachable or a store it mints at is.
func (r *resolver) grants(ctx context.Context, deploymentID string, secrets []apiclient.AssignmentSecret) (map[string]apiclient.GrantView, error) {
	if !slices.ContainsFunc(secrets, func(s apiclient.AssignmentSecret) bool {
		return s.Store.Access.Mode == apiclient.StoreAccessModeFederated || s.Store.Access.Mode == apiclient.StoreAccessModeBrokered
	}) {
		return nil, nil
	}
	if r.cfg.Grants == nil {
		return nil, deploy.Failf("this agent cannot ask for access to federated or brokered secret stores")
	}
	var grants []apiclient.GrantView
	wait := r.grantRetryBase
	for attempt := 1; ; attempt++ {
		var err error
		grants, err = r.cfg.Grants(ctx, deploymentID)
		if err == nil {
			break
		}
		switch {
		case errors.Is(err, apiclient.ErrConflict):
			return nil, fmt.Errorf("%w: %v", ErrNotApplying, err)
		case !apiclient.Transient(err) || attempt == retries:
			return nil, &deploy.Failure{Reason: "the control plane granted no access to the deployment's secret stores", Err: err}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	out := make(map[string]apiclient.GrantView, len(grants))
	for _, g := range grants {
		out[g.StoreId] = g
	}
	return out, nil
}
