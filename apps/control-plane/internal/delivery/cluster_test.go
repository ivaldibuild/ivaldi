package delivery

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const (
	domClusterAgent      = "prn_cluster_agent"
	domClusterOtherAgent = "prn_cluster_other_agent"
)

func domClusterProps() NewClusterProps {
	return NewClusterProps{
		OrganizationID: domOrg,
		Slug:           "prod-eu",
		DisplayName:    "Prod EU",
		Runtime:        RuntimeKubernetes,
	}
}

func domNewCluster(t *testing.T) *Cluster {
	t.Helper()
	c, err := NewCluster(domClusterProps(), nil)
	require.NoError(t, err)
	return c
}

func TestNewCluster(t *testing.T) {
	clock.Fix(t, domT0)
	props := domClusterProps()
	props.AgentPrincipalID = new(domClusterAgent)
	props.AgentCapabilities = map[string]any{"agent_version": "1.4.0"}

	c, err := NewCluster(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("cls", c.ID()), "a new cluster gets a cls_ ID: %q", c.ID())
	assert.Equal(t, domOrg, c.OrganizationID())
	assert.Equal(t, "prod-eu", c.Slug())
	assert.Equal(t, "Prod EU", c.DisplayName())
	assert.Equal(t, RuntimeKubernetes, c.Runtime())
	require.NotNil(t, c.AgentPrincipalID())
	assert.Equal(t, domClusterAgent, *c.AgentPrincipalID())
	assert.Equal(t, map[string]any{"agent_version": "1.4.0"}, c.AgentCapabilities())
	assert.Nil(t, c.AgentLastFailure())
	assert.Nil(t, c.LastReportedAt())
	assert.Equal(t, domT0, c.CreatedAt())
	assert.Equal(t, domT0, c.UpdatedAt())
	assert.Nil(t, c.DeletedAt())
	assert.False(t, c.Archived())
}

func TestNewClusterDefaults(t *testing.T) {
	props := domClusterProps()
	props.Slug = ""
	props.AgentPrincipalID = new("")

	c, err := NewCluster(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "prod-eu", c.Slug(), "the slug is derived from the display name")
	assert.Nil(t, c.AgentPrincipalID(), "an empty agent is none")
	assert.Empty(t, c.AgentCapabilities(), "an agent that has not reported has no facts")
}

func TestNewClusterWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("cls")
	failure := &AgentFailure{Reason: "identity_in_use", At: domT1}
	props := domClusterProps()
	props.AgentCapabilities = map[string]any{"kubernetes_version": "v1.31.4"}
	props.AgentLastFailure = failure
	props.LastReportedAt = new(domT1)
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.DeletedAt = new(domT1)

	c, err := NewCluster(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, c.ID())
	assert.Equal(t, map[string]any{"kubernetes_version": "v1.31.4"}, c.AgentCapabilities())
	require.NotNil(t, c.AgentLastFailure())
	assert.Equal(t, *failure, *c.AgentLastFailure())
	assert.Equal(t, domT1, *c.LastReportedAt())
	assert.Equal(t, domT0, c.CreatedAt())
	assert.Equal(t, domT1, c.UpdatedAt())
	assert.Equal(t, domT1, *c.DeletedAt())
	assert.True(t, c.Archived())
}

func TestNewClusterIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewClusterProps)
		id     *string
	}{
		{"no organization", func(p *NewClusterProps) { p.OrganizationID = "" }, nil},
		{"ID with wrong prefix", func(*NewClusterProps) {}, new(id.New("tgt"))},
		{"malformed ID", func(*NewClusterProps) {}, new("cls_")},
		{"no display name", func(p *NewClusterProps) { p.DisplayName = "" }, nil},
		{"bad slug", func(p *NewClusterProps) { p.Slug = "prod_eu" }, nil},
		{"no runtime", func(p *NewClusterProps) { p.Runtime = "" }, nil},
		{"unsupported runtime", func(p *NewClusterProps) { p.Runtime = "nomad" }, nil},
		{"capabilities that cannot be JSON", func(p *NewClusterProps) {
			p.AgentCapabilities = map[string]any{"bad": func() {}}
		}, nil},
		{"capabilities too large", func(p *NewClusterProps) {
			p.AgentCapabilities = map[string]any{"blob": strings.Repeat("x", maxClusterCapabilitiesBytes)}
		}, nil},
		{"empty failure reason", func(p *NewClusterProps) {
			p.AgentLastFailure = &AgentFailure{At: domT0}
		}, nil},
		{"failure reason too long", func(p *NewClusterProps) {
			p.AgentLastFailure = &AgentFailure{Reason: strings.Repeat("r", maxAgentFailureReasonLength+1), At: domT0}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domClusterProps()
			tt.mutate(&props)
			_, err := NewCluster(props, tt.id)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestClusterUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	c.PullEvents()

	clock.Fix(t, domT1)
	require.NoError(t, c.Update(UpdateClusterProps{DisplayName: new("Prod EU West")}))
	assert.Equal(t, "Prod EU West", c.DisplayName())
	assert.Equal(t, domT1, c.UpdatedAt())
	events := c.PullEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "delivery.v1.cluster.updated", events[0].EventName())

	clock.Fix(t, domT2)
	require.NoError(t, c.Update(UpdateClusterProps{DisplayName: new("Prod EU West")}))
	assert.Empty(t, c.PullEvents(), "an update that changes nothing records no event")

	require.NoError(t, c.Update(UpdateClusterProps{}))
	assert.Empty(t, c.PullEvents(), "no fields set changes nothing")
}

func TestClusterUpdateIsRefused(t *testing.T) {
	c := domNewCluster(t)
	c.Archive()

	assert.ErrorIs(t, c.Update(UpdateClusterProps{DisplayName: new("Renamed")}), ErrArchived)
	assert.Equal(t, "Prod EU", c.DisplayName())
}

func TestClusterAssignAgent(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	c.PullEvents()

	clock.Fix(t, domT1)
	require.NoError(t, c.AssignAgent(domClusterAgent))
	require.NotNil(t, c.AgentPrincipalID())
	assert.Equal(t, domClusterAgent, *c.AgentPrincipalID())
	assert.Equal(t, domT1, c.UpdatedAt())
	events := c.PullEvents()
	require.Len(t, events, 1)
	assigned, ok := events[0].(ClusterAgentAssigned)
	require.True(t, ok)
	assert.Nil(t, assigned.PreviousAgentPrincipalID)

	clock.Fix(t, domT2)
	require.NoError(t, c.AssignAgent(domClusterOtherAgent))
	events = c.PullEvents()
	require.Len(t, events, 1)
	assigned, ok = events[0].(ClusterAgentAssigned)
	require.True(t, ok)
	require.NotNil(t, assigned.PreviousAgentPrincipalID)
	assert.Equal(t, domClusterAgent, *assigned.PreviousAgentPrincipalID)

	require.ErrorIs(t, c.AssignAgent(""), ErrInvalid)
}

func TestClusterAssignAgentClearsThePreviousAgentsState(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	require.NoError(t, c.AssignAgent(domClusterAgent))
	require.NoError(t, c.RecordHeartbeat(domClusterAgent, map[string]any{"agent_version": "1.0.0"}))

	clock.Fix(t, domT1)
	require.NoError(t, c.AssignAgent(domClusterOtherAgent))

	assert.Empty(t, c.AgentCapabilities(), "a new agent has reported nothing")
	assert.Nil(t, c.LastReportedAt())
}

func TestClusterUnassignAgent(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	require.NoError(t, c.AssignAgent(domClusterAgent))
	c.PullEvents()

	clock.Fix(t, domT1)
	require.NoError(t, c.UnassignAgent())
	assert.Nil(t, c.AgentPrincipalID())
	events := c.PullEvents()
	require.Len(t, events, 1)
	unassigned, ok := events[0].(ClusterAgentUnassigned)
	require.True(t, ok)
	assert.Equal(t, domClusterAgent, unassigned.PreviousAgentPrincipalID)

	require.NoError(t, c.UnassignAgent(), "unassigning none does nothing")
	assert.Empty(t, c.PullEvents())
}

func TestClusterHeartbeat(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	require.NoError(t, c.AssignAgent(domClusterAgent))
	c.PullEvents()

	clock.Fix(t, domT1)
	require.NoError(t, c.RecordHeartbeat(domClusterAgent, map[string]any{
		"agent_version":      "1.4.0",
		"kubernetes_version": "v1.31.4",
		"deployers":          []any{"helm"},
	}))
	assert.Equal(t, domT1, *c.LastReportedAt())
	assert.Equal(t, "1.4.0", c.AgentCapabilities()["agent_version"])
	assert.Equal(t, domT0, c.UpdatedAt(), "contact is not a configuration change")
	assert.Empty(t, c.PullEvents(), "a heartbeat records no event")

	assert.ErrorIs(t, c.RecordHeartbeat(domClusterOtherAgent, nil), ErrNotClusterAgent)
	assert.Equal(t, domT1, *c.LastReportedAt(), "a refused heartbeat changes nothing")
}

func TestClusterHeartbeatWithoutAnAgent(t *testing.T) {
	c := domNewCluster(t)
	assert.ErrorIs(t, c.RecordHeartbeat(domClusterAgent, nil), ErrNotClusterAgent)
}

func TestClusterHeartbeatIsRefusedAfterArchive(t *testing.T) {
	c := domNewCluster(t)
	require.NoError(t, c.AssignAgent(domClusterAgent))
	c.Archive()

	assert.ErrorIs(t, c.RecordHeartbeat(domClusterAgent, nil), ErrArchived)
}

func TestClusterRecordAgentFailure(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	c.PullEvents()

	clock.Fix(t, domT1)
	require.NoError(t, c.RecordAgentFailure("identity_in_use: served by prod-eu-2"))
	require.NotNil(t, c.AgentLastFailure())
	assert.Equal(t, "identity_in_use: served by prod-eu-2", c.AgentLastFailure().Reason)
	assert.Equal(t, domT1, c.AgentLastFailure().At)
	assert.Equal(t, domT0, c.UpdatedAt(), "a refusal is not a configuration change")
	assert.Empty(t, c.PullEvents())

	require.ErrorIs(t, c.RecordAgentFailure(""), ErrInvalid)

	require.NoError(t, c.AssignAgent(domClusterAgent))
	assert.Nil(t, c.AgentLastFailure(), "a successful enrolment clears the failure")
}

func TestClusterArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewCluster(t)
	require.NoError(t, c.AssignAgent(domClusterAgent))
	require.NoError(t, c.RecordHeartbeat(domClusterAgent, map[string]any{"agent_version": "1.4.0"}))
	require.NoError(t, c.RecordAgentFailure("identity_in_use"))
	c.PullEvents()

	clock.Fix(t, domT1)
	previous := c.Archive()
	require.NotNil(t, previous)
	assert.Equal(t, domClusterAgent, *previous, "archiving returns the principal to revoke")
	assert.True(t, c.Archived())
	assert.Nil(t, c.AgentPrincipalID())
	assert.Empty(t, c.AgentCapabilities())
	assert.Nil(t, c.AgentLastFailure())
	assert.Nil(t, c.LastReportedAt())
	assert.Equal(t, domT1, c.UpdatedAt())
	events := c.PullEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "delivery.v1.cluster.archived", events[0].EventName())
	assert.Nil(t, c.Archive(), "archiving an archived cluster does nothing")
	assert.Empty(t, c.PullEvents())

	clock.Fix(t, domT2)
	c.Restore()
	assert.False(t, c.Archived())
	assert.Nil(t, c.AgentPrincipalID(), "a restored cluster has no agent")
	assert.Equal(t, domT2, c.UpdatedAt())
	events = c.PullEvents()
	require.Len(t, events, 1)
	assert.Equal(t, "delivery.v1.cluster.restored", events[0].EventName())
	c.Restore()
	assert.Empty(t, c.PullEvents(), "restoring an active cluster does nothing")
}

func TestClusterSnapshotCopies(t *testing.T) {
	clock.Fix(t, domT0)
	props := domClusterProps()
	props.AgentCapabilities = map[string]any{
		"agent_version": "1.4.0",
		"deployers":     []any{"helm"},
		"watch":         map[string]any{"mode": "cluster"},
	}
	c, err := NewCluster(props, nil)
	require.NoError(t, err)

	snapshot := c.Snapshot()
	snapshot.AgentCapabilities["agent_version"] = "changed"
	snapshot.AgentCapabilities["deployers"].([]any)[0] = "changed"
	snapshot.AgentCapabilities["watch"].(map[string]any)["mode"] = "changed"
	assert.Equal(t, "1.4.0", c.AgentCapabilities()["agent_version"], "the snapshot cannot change the cluster")
	assert.Equal(t, []any{"helm"}, c.AgentCapabilities()["deployers"], "nor through a nested list")
	assert.Equal(t, map[string]any{"mode": "cluster"}, c.AgentCapabilities()["watch"], "nor through a nested object")

	capabilities := c.AgentCapabilities()
	capabilities["agent_version"] = "changed"
	capabilities["deployers"].([]any)[0] = "changed"
	assert.Equal(t, "1.4.0", c.AgentCapabilities()["agent_version"], "the getter cannot change the cluster")
	assert.Equal(t, []any{"helm"}, c.AgentCapabilities()["deployers"], "nor through a nested list")

	props.AgentCapabilities["agent_version"] = "changed"
	assert.Equal(t, "1.4.0", c.AgentCapabilities()["agent_version"], "the constructor copies what it is given")
}
