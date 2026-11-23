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

// grants asks the control plane for access to the federated stores among
// the secrets', and returns each store's JWT by its ID. It asks nothing when
// none is federated, and retries while the control plane is unreachable.
func (r *resolver) grants(ctx context.Context, deploymentID string, secrets []apiclient.AssignmentSecret) (map[string]string, error) {
	if !slices.ContainsFunc(secrets, func(s apiclient.AssignmentSecret) bool {
		return s.Store.Access.Mode == apiclient.StoreAccessModeFederated
	}) {
		return nil, nil
	}
	if r.cfg.Grants == nil {
		return nil, deploy.Failf("this agent cannot ask for access to federated secret stores")
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
	out := map[string]string{}
	for _, g := range grants {
		if g.Jwt != nil && *g.Jwt != "" {
			out[g.StoreId] = *g.Jwt
		}
	}
	return out, nil
}
