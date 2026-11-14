package clusters

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

var ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

func TestNewClusterServiceRequiresEveryDependency(t *testing.T) {
	s := newStore()
	all := s.options(&authorizer{})
	for i := range all {
		opts := append(append([]ClusterServiceOption(nil), all[:i]...), all[i+1:]...)
		_, err := NewClusterService(opts...)
		assert.Error(t, err, "without option %d", i)
	}
	_, err := NewClusterService(all...)
	assert.NoError(t, err)
}

func TestCreateCluster(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)

	c, err := w.svc.CreateCluster(ctx, ada, delivery.CreateClusterInput{
		OrganizationID: org, DisplayName: "Prod EU", Runtime: delivery.RuntimeKubernetes,
	})
	require.NoError(t, err)

	assert.Equal(t, "prod-eu", c.Slug(), "the slug is derived from the display name")
	assert.Equal(t, delivery.RuntimeKubernetes, c.Runtime())
	require.Len(t, w.authz.asked, 1)
	req := w.authz.asked[0]
	assert.Equal(t, delivery.PermissionClusterManage, req.Permission)
	assert.Equal(t, delivery.ActionClusterCreate, req.Action)
	assert.Equal(t, org, req.ResourceID, "authorized on the Organization")

	assert.Contains(t, w.clusters, c.ID())
	assert.Equal(t, []string{"delivery.v1.cluster.created"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, delivery.EventClusterCreated, w.audit[0].Type)
	assert.Equal(t, "dec-"+org, w.audit[0].DecisionID)
	assert.Equal(t, ada, w.audit[0].Actor)
	assert.Equal(t, c.Snapshot(), w.audit[0].After)
}

func TestCreateClusterRefusesABadRuntime(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.CreateCluster(context.Background(), ada, delivery.CreateClusterInput{
		OrganizationID: org, DisplayName: "Prod EU", Runtime: "nomad",
	})
	require.ErrorIs(t, err, delivery.ErrInvalid)
	assert.Empty(t, w.clusters)
	assert.Empty(t, w.audit)
	assert.Empty(t, w.events)
}

// Every operation asks the authorizer the right question, about the
// Organization, before it does anything.
func TestOperationsAreAuthorizedOnTheirResource(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	active := w.cluster(t, "prod-eu", "", false)
	archived := w.cluster(t, "old", "", true)

	cases := []struct {
		name       string
		call       func() error
		permission string
		action     string
	}{
		{"create", func() error {
			_, err := w.svc.CreateCluster(ctx, ada, delivery.CreateClusterInput{OrganizationID: org, DisplayName: "Staging", Runtime: delivery.RuntimeKubernetes})
			return err
		}, delivery.PermissionClusterManage, delivery.ActionClusterCreate},
		{"get", func() error {
			_, err := w.svc.GetCluster(ctx, ada, org, active.ID())
			return err
		}, delivery.PermissionClusterRead, delivery.ActionClusterRead},
		{"list", func() error {
			_, err := w.svc.ListClusters(ctx, ada, delivery.ListClustersQuery{OrganizationID: org})
			return err
		}, delivery.PermissionClusterRead, delivery.ActionClusterList},
		{"update", func() error {
			_, err := w.svc.UpdateCluster(ctx, ada, org, active.ID(), delivery.UpdateClusterProps{DisplayName: new("Prod EU 2")})
			return err
		}, delivery.PermissionClusterManage, delivery.ActionClusterUpdate},
		{"archive", func() error {
			_, err := w.svc.ArchiveCluster(ctx, ada, org, active.ID())
			return err
		}, delivery.PermissionClusterManage, delivery.ActionClusterArchive},
		{"restore", func() error {
			_, err := w.svc.RestoreCluster(ctx, ada, org, archived.ID())
			return err
		}, delivery.PermissionClusterManage, delivery.ActionClusterRestore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w.reset()
			require.NoError(t, tc.call())
			require.Len(t, w.authz.asked, 1)
			req := w.authz.asked[0]
			assert.Equal(t, ada, req.Actor)
			assert.Equal(t, org, req.OrganizationID)
			assert.Equal(t, tc.permission, req.Permission)
			assert.Equal(t, tc.action, req.Action)
			assert.Equal(t, org, req.ResourceID)
		})
	}
}

func TestArchiveClusterRevokesTheAgent(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.cluster(t, "prod-eu", "wkp_agent", false)
	w.reset()

	got, err := w.svc.ArchiveCluster(ctx, ada, org, c.ID())
	require.NoError(t, err)

	assert.True(t, got.Archived())
	assert.Nil(t, got.AgentPrincipalID())
	assert.Equal(t, []string{"wkp_agent"}, w.revoked)
	stored := w.clusters[c.ID()]
	assert.Nil(t, stored.AgentPrincipalID, "the agent is cleared")
	assert.Empty(t, stored.AgentCapabilities)
	assert.Equal(t, []string{"delivery.v1.cluster.archived"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, delivery.EventClusterArchived, w.audit[0].Type)
	assert.Equal(t, c.Snapshot().AgentPrincipalID, w.audit[0].Before.(delivery.ClusterSnapshot).AgentPrincipalID)
}

func TestArchiveClusterWithActiveTargets(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.cluster(t, "prod-eu", "wkp_agent", false)
	w.activeTargets = 2
	w.reset()

	_, err := w.svc.ArchiveCluster(ctx, ada, org, c.ID())

	require.ErrorIs(t, err, delivery.ErrInUse)
	assert.False(t, w.clusters[c.ID()].DeletedAt != nil, "the cluster is not archived")
	assert.Empty(t, w.revoked)
	assert.Empty(t, w.audit)
	assert.Empty(t, w.events)
}

func TestArchiveClusterWithoutAnAgent(t *testing.T) {
	w := newWorld(t)
	c := w.cluster(t, "prod-eu", "", false)

	_, err := w.svc.ArchiveCluster(context.Background(), ada, org, c.ID())
	require.NoError(t, err)
	assert.Empty(t, w.revoked)
	assert.Len(t, w.audit, 1)
}

func TestArchiveAndRestoreAreIdempotent(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.cluster(t, "prod-eu", "wkp_agent", false)

	_, err := w.svc.ArchiveCluster(ctx, ada, org, c.ID())
	require.NoError(t, err)
	w.reset()

	_, err = w.svc.ArchiveCluster(ctx, ada, org, c.ID())
	require.NoError(t, err)
	assert.Empty(t, w.audit, "archiving an archived cluster records nothing")
	assert.Empty(t, w.events)
	assert.Empty(t, w.revoked)

	_, err = w.svc.RestoreCluster(ctx, ada, org, c.ID())
	require.NoError(t, err)
	assert.Equal(t, []string{"delivery.v1.cluster.restored"}, w.eventTypes())
	w.reset()

	_, err = w.svc.RestoreCluster(ctx, ada, org, c.ID())
	require.NoError(t, err)
	assert.Empty(t, w.audit, "restoring an active cluster records nothing")
	assert.Empty(t, w.events)
}

func TestRestoreLeavesTheClusterWithoutAnAgent(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.cluster(t, "prod-eu", "wkp_agent", true)

	got, err := w.svc.RestoreCluster(ctx, ada, org, c.ID())
	require.NoError(t, err)
	assert.False(t, got.Archived())
	assert.Nil(t, got.AgentPrincipalID(), "re-enrolment is needed after a restore")
}

func TestUpdateCluster(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	c := w.cluster(t, "prod-eu", "", false)

	got, err := w.svc.UpdateCluster(ctx, ada, org, c.ID(), delivery.UpdateClusterProps{DisplayName: new("Prod EU West")})
	require.NoError(t, err)
	assert.Equal(t, "Prod EU West", got.DisplayName())
	assert.Equal(t, []string{"delivery.v1.cluster.updated"}, w.eventTypes())
	require.Len(t, w.audit, 1)
	assert.Equal(t, delivery.EventClusterUpdated, w.audit[0].Type)
}

// A refused change writes nothing.
func TestRefusedChangesWriteNothing(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(w *world, cluster *delivery.Cluster) error{
		"create": func(w *world, _ *delivery.Cluster) error {
			_, err := w.svc.CreateCluster(ctx, ada, delivery.CreateClusterInput{OrganizationID: org, DisplayName: "Staging", Runtime: delivery.RuntimeKubernetes})
			return err
		},
		"update": func(w *world, cluster *delivery.Cluster) error {
			_, err := w.svc.UpdateCluster(ctx, ada, org, cluster.ID(), delivery.UpdateClusterProps{DisplayName: new("Renamed")})
			return err
		},
		"archive": func(w *world, cluster *delivery.Cluster) error {
			_, err := w.svc.ArchiveCluster(ctx, ada, org, cluster.ID())
			return err
		},
		"restore": func(w *world, cluster *delivery.Cluster) error {
			_, err := w.svc.RestoreCluster(ctx, ada, org, cluster.ID())
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			cluster := w.cluster(t, "prod-eu", "wkp_agent", name == "restore")
			w.reset()
			w.authz.deny = func(authorization.Request) bool { return true }

			err := call(w, cluster)
			require.ErrorIs(t, err, authorization.ErrDenied)
			assert.Empty(t, w.audit)
			assert.Empty(t, w.events)
			assert.Empty(t, w.revoked)
		})
	}
}

func TestMissingCluster(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.GetCluster(context.Background(), ada, org, "cls_01JZ0000000000000000000099")
	require.ErrorIs(t, err, delivery.ErrNotFound)

	_, err = w.svc.CreateCluster(context.Background(), ada, delivery.CreateClusterInput{
		OrganizationID: org, DisplayName: "Prod EU", Runtime: delivery.RuntimeKubernetes,
	})
	require.NoError(t, err)
	_, err = w.svc.CreateCluster(context.Background(), ada, delivery.CreateClusterInput{
		OrganizationID: org, DisplayName: "Prod EU", Runtime: delivery.RuntimeKubernetes,
	})
	require.ErrorIs(t, err, delivery.ErrSlugTaken)
}
