package authorizer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

const resolveRoute = "version_control.webhook_route.resolve"

func platformRequest(actor identity.Actor, permission string) authorization.Request {
	return authorization.Request{
		Actor:      actor,
		Permission: permission,
		ResourceID: authorization.PlatformResource,
		Action:     "webhook_route.resolve",
	}
}

func TestPlatformRequests(t *testing.T) {
	gateway, err := iam.NewPlatformWorkload("gateway", iam.ComponentGateway)
	require.NoError(t, err)
	gatewayActor := identity.Actor{PrincipalID: gateway.ID(), Kind: identity.ActorWorkload}
	platform := fakePlatform{gateway.ID(): gateway}

	t.Run("a platform workload may use its component's permissions", func(t *testing.T) {
		access := &fakeAccess{}
		d, err := newAuthorizerWith(t, access, platform).Authorize(context.Background(), platformRequest(gatewayActor, resolveRoute))
		require.NoError(t, err)
		assert.Equal(t, authorization.OutcomeAllow, d.Outcome, d.Reason)
		assert.NotEmpty(t, d.ID)
		assert.Empty(t, access.asked, "platform requests consult no grants")
	})

	denials := []struct {
		name string
		req  authorization.Request
		with fakePlatform
	}{
		{"a permission its component does not hold", platformRequest(gatewayActor, "source.manage"), platform},
		{"a user", platformRequest(identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}, resolveRoute), platform},
		{"an Organization workload", platformRequest(identity.Actor{PrincipalID: "prn_agent", Kind: identity.ActorWorkload}, resolveRoute), platform},
	}
	for _, tt := range denials {
		t.Run("denies "+tt.name, func(t *testing.T) {
			d, err := newAuthorizerWith(t, &fakeAccess{}, tt.with).Authorize(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, authorization.OutcomeDeny, d.Outcome)
		})
	}

	t.Run("denies a suspended platform workload", func(t *testing.T) {
		suspended, err := iam.NewPlatformWorkload("old gateway", iam.ComponentGateway)
		require.NoError(t, err)
		require.NoError(t, suspended.Suspend())
		actor := identity.Actor{PrincipalID: suspended.ID(), Kind: identity.ActorWorkload}
		d, err := newAuthorizerWith(t, &fakeAccess{}, fakePlatform{suspended.ID(): suspended}).
			Authorize(context.Background(), platformRequest(actor, resolveRoute))
		require.NoError(t, err)
		assert.Equal(t, authorization.OutcomeDeny, d.Outcome)
	})

	t.Run("denies a platform workload an Organization's resource", func(t *testing.T) {
		req := request("source.read")
		req.Actor = gatewayActor
		d, err := newAuthorizerWith(t, &fakeAccess{access: active(role(iam.RoleOwner))}, platform).Authorize(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, authorization.OutcomeDeny, d.Outcome, "it holds no grants")
	})

	t.Run("refuses a platform request that names an Organization", func(t *testing.T) {
		req := platformRequest(gatewayActor, resolveRoute)
		req.OrganizationID = "org_acme"
		_, err := newAuthorizerWith(t, &fakeAccess{}, platform).Authorize(context.Background(), req)
		assert.Error(t, err)
	})
}
