package iam

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func agentProps() NewAgentProps {
	return NewAgentProps{
		PrincipalProps: PrincipalProps{OrganizationID: testOrg, DisplayName: "Release agent"},
		Slug:           "release-agent",
	}
}

func agentNew(t *testing.T) *Agent {
	t.Helper()
	return mustBuild(NewAgent(agentProps(), nil))
}

func TestNewAgent(t *testing.T) {
	clock.Fix(t, t0)

	a := agentNew(t)

	assert.True(t, id.Valid("prn", a.ID()), "an agent is a principal: %q", a.ID())
	assert.Equal(t, PrincipalAgent, a.Kind())
	assert.Equal(t, testOrg, a.OrganizationID())
	assert.Equal(t, "release-agent", a.Slug())
	assert.Equal(t, "Release agent", a.DisplayName())
	assert.Equal(t, PrincipalActive, a.Status(), "a new agent is active")
	assert.Equal(t, t0, a.CreatedAt())
	assert.Equal(t, AgentSnapshot{
		PrincipalSnapshot: PrincipalSnapshot{
			ID: a.ID(), OrganizationID: testOrg, Kind: PrincipalAgent, DisplayName: "Release agent",
			Status: PrincipalActive, CreatedAt: t0, UpdatedAt: t0,
		},
		Slug: "release-agent",
	}, a.Snapshot())
}

func TestNewAgentWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("prn")
	props := agentProps()
	props.Status = PrincipalSuspended
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)

	a, err := NewAgent(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, a.ID())
	assert.Equal(t, PrincipalSuspended, a.Status())
	assert.Equal(t, t0, a.CreatedAt())
	assert.Equal(t, t1, a.UpdatedAt())
	assert.Empty(t, a.PullEvents(), "a stored agent records nothing")
}

func TestNewAgentIsRefused(t *testing.T) {
	tests := map[string]func(*NewAgentProps){
		"no slug":           func(p *NewAgentProps) { p.Slug = "" },
		"a bad slug":        func(p *NewAgentProps) { p.Slug = "Release Agent" },
		"no display name":   func(p *NewAgentProps) { p.DisplayName = "" },
		"no organization":   func(p *NewAgentProps) { p.OrganizationID = "" },
		"an unknown status": func(p *NewAgentProps) { p.Status = "deleted" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			props := agentProps()
			mutate(&props)
			_, err := NewAgent(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestAgentEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, agentNew, []eventCase[*Agent]{
		{"suspend", func(t *testing.T, a *Agent) { require.NoError(t, a.Suspend()) },
			func(a *Agent) aggregate.Event {
				return PrincipalStatusChanged{Principal: a.snapshot(), PreviousStatus: PrincipalActive}
			}},
		{"rename", func(t *testing.T, a *Agent) { require.NoError(t, a.Rename("Releases")) },
			func(a *Agent) aggregate.Event { return PrincipalRenamed{a.snapshot()} }},
	})
}
