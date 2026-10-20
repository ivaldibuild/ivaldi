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
	domAgent      = "prn_agent"
	domOtherAgent = "prn_other"
)

func domTargetProps() NewTargetProps {
	return NewTargetProps{
		OrganizationID: domOrg,
		EnvironmentID:  id.New("env"),
		Slug:           "eu-west",
		DisplayName:    "EU West",
		Runtime:        RuntimeKubernetes,
		Locator:        map[string]string{LocatorCluster: "prod-eu-1", LocatorNamespace: "payments"},
	}
}

func domNewTarget(t *testing.T) *Target {
	t.Helper()
	tgt, err := NewTarget(domTargetProps(), nil)
	require.NoError(t, err)
	return tgt
}

func TestNewTarget(t *testing.T) {
	clock.Fix(t, domT0)
	props := domTargetProps()
	props.AgentPrincipalID = new(domAgent)

	tgt, err := NewTarget(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("tgt", tgt.ID()), "a new target gets a tgt_ ID: %q", tgt.ID())
	assert.Equal(t, domOrg, tgt.OrganizationID())
	assert.Equal(t, props.EnvironmentID, tgt.EnvironmentID())
	assert.Equal(t, "eu-west", tgt.Slug())
	assert.Equal(t, "EU West", tgt.DisplayName())
	assert.Equal(t, RuntimeKubernetes, tgt.Runtime())
	assert.Equal(t, map[string]string{LocatorCluster: "prod-eu-1", LocatorNamespace: "payments"}, tgt.Locator())
	require.NotNil(t, tgt.AgentPrincipalID())
	assert.Equal(t, domAgent, *tgt.AgentPrincipalID())
	assert.Nil(t, tgt.LastReportedAt())
	assert.Equal(t, domT0, tgt.CreatedAt())
	assert.Equal(t, domT0, tgt.UpdatedAt())
	assert.Nil(t, tgt.DeletedAt())
	assert.False(t, tgt.Archived())
}

func TestNewTargetDefaults(t *testing.T) {
	props := domTargetProps()
	props.Slug = ""
	props.AgentPrincipalID = new("")

	tgt, err := NewTarget(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "eu-west", tgt.Slug(), "the slug is derived from the display name")
	assert.Nil(t, tgt.AgentPrincipalID(), "an empty agent is none")
}

func TestNewTargetWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("tgt")
	props := domTargetProps()
	props.LastReportedAt = new(domT1)
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.DeletedAt = new(domT1)

	tgt, err := NewTarget(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, tgt.ID())
	assert.Equal(t, domT1, *tgt.LastReportedAt())
	assert.Equal(t, domT0, tgt.CreatedAt())
	assert.Equal(t, domT1, tgt.UpdatedAt())
	assert.Equal(t, domT1, *tgt.DeletedAt())
	assert.True(t, tgt.Archived())
}

func TestNewTargetIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewTargetProps)
		id     *string
	}{
		{"no organization", func(p *NewTargetProps) { p.OrganizationID = "" }, nil},
		{"no environment", func(p *NewTargetProps) { p.EnvironmentID = "" }, nil},
		{"environment ID with wrong prefix", func(p *NewTargetProps) { p.EnvironmentID = id.New("lfc") }, nil},
		{"ID with wrong prefix", func(*NewTargetProps) {}, new(id.New("env"))},
		{"malformed ID", func(*NewTargetProps) {}, new("tgt_")},
		{"no display name", func(p *NewTargetProps) { p.DisplayName = "" }, nil},
		{"bad slug", func(p *NewTargetProps) { p.Slug = "eu_west" }, nil},
		{"unsupported runtime", func(p *NewTargetProps) { p.Runtime = "nomad" }, nil},
		{"no runtime", func(p *NewTargetProps) { p.Runtime = "" }, nil},
		{"no locator", func(p *NewTargetProps) { p.Locator = nil }, nil},
		{"no cluster", func(p *NewTargetProps) { delete(p.Locator, LocatorCluster) }, nil},
		{"cluster with a space", func(p *NewTargetProps) { p.Locator[LocatorCluster] = "prod eu" }, nil},
		{"cluster too long", func(p *NewTargetProps) { p.Locator[LocatorCluster] = strings.Repeat("c", maxDisplayNameLength+1) }, nil},
		{"no namespace", func(p *NewTargetProps) { delete(p.Locator, LocatorNamespace) }, nil},
		{"namespace not a DNS label", func(p *NewTargetProps) { p.Locator[LocatorNamespace] = "Payments_NS" }, nil},
		{"namespace too long", func(p *NewTargetProps) { p.Locator[LocatorNamespace] = strings.Repeat("n", 64) }, nil},
		{"unknown locator key", func(p *NewTargetProps) { p.Locator["region"] = "eu-west-1" }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domTargetProps()
			tt.mutate(&props)
			_, err := NewTarget(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestTargetGettersReturnCopies(t *testing.T) {
	props := domTargetProps()
	props.AgentPrincipalID = new(domAgent)
	props.LastReportedAt = new(domT0)
	props.DeletedAt = new(domT0)
	tgt, err := NewTarget(props, nil)
	require.NoError(t, err)
	before := tgt.Snapshot()

	props.Locator[LocatorNamespace] = "changed"
	*props.AgentPrincipalID = "changed"
	*props.LastReportedAt = domT2
	tgt.Locator()[LocatorNamespace] = "changed"
	*tgt.AgentPrincipalID() = "changed"
	*tgt.LastReportedAt() = domT2
	*tgt.DeletedAt() = domT2
	snap := tgt.Snapshot()
	snap.Locator[LocatorCluster] = "changed"
	*snap.AgentPrincipalID = "changed"

	assert.Equal(t, before, tgt.Snapshot())
}

func TestTargetSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	props := domTargetProps()
	tgt, err := NewTarget(props, nil)
	require.NoError(t, err)

	assert.Equal(t, TargetSnapshot{
		ID:             tgt.ID(),
		OrganizationID: domOrg,
		EnvironmentID:  props.EnvironmentID,
		Slug:           "eu-west",
		DisplayName:    "EU West",
		Runtime:        RuntimeKubernetes,
		Locator:        map[string]string{LocatorCluster: "prod-eu-1", LocatorNamespace: "payments"},
		CreatedAt:      domT0,
		UpdatedAt:      domT0,
	}, tgt.Snapshot())
}

func TestTargetUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	tgt := domNewTarget(t)
	clock.Fix(t, domT1)
	locator := map[string]string{LocatorCluster: "prod-eu-2", LocatorNamespace: "billing"}

	require.NoError(t, tgt.Update(UpdateTargetProps{Slug: new("eu-west-2"), DisplayName: new("EU West 2"), Locator: locator}))

	assert.Equal(t, "eu-west-2", tgt.Slug())
	assert.Equal(t, "EU West 2", tgt.DisplayName())
	assert.Equal(t, locator, tgt.Locator())
	assert.Equal(t, domT1, tgt.UpdatedAt())

	locator[LocatorNamespace] = "changed"
	assert.Equal(t, "billing", tgt.Locator()[LocatorNamespace], "the given locator is copied")

	t.Run("a nil locator is kept", func(t *testing.T) {
		require.NoError(t, tgt.Update(UpdateTargetProps{DisplayName: new("EU")}))
		assert.Equal(t, "billing", tgt.Locator()[LocatorNamespace])
		assert.Equal(t, "eu-west-2", tgt.Slug())
	})
}

func TestTargetUpdateIsAtomic(t *testing.T) {
	good := map[string]string{LocatorCluster: "c2", LocatorNamespace: "ns2"}
	tests := []struct {
		name  string
		props UpdateTargetProps
	}{
		{"bad slug", UpdateTargetProps{Slug: new("Bad"), DisplayName: new("New"), Locator: good}},
		{"bad display name", UpdateTargetProps{Slug: new("new"), DisplayName: new(""), Locator: good}},
		{"empty locator", UpdateTargetProps{Slug: new("new"), DisplayName: new("New"), Locator: map[string]string{}}},
		{"bad namespace", UpdateTargetProps{Slug: new("new"), DisplayName: new("New"), Locator: map[string]string{LocatorCluster: "c2", LocatorNamespace: "-ns"}}},
		{"unknown key", UpdateTargetProps{Slug: new("new"), DisplayName: new("New"), Locator: map[string]string{LocatorCluster: "c2", LocatorNamespace: "ns2", "zone": "a"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			tgt := domNewTarget(t)
			before := tgt.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, tgt.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, tgt.Snapshot())
		})
	}
}

func TestTargetRefusesChangesWhenArchived(t *testing.T) {
	props := domTargetProps()
	props.AgentPrincipalID = new(domAgent)
	tgt, err := NewTarget(props, nil)
	require.NoError(t, err)
	tgt.Archive()
	before := tgt.Snapshot()

	assert.ErrorIs(t, tgt.Update(UpdateTargetProps{DisplayName: new("New")}), ErrArchived)
	assert.ErrorIs(t, tgt.AssignAgent(domOtherAgent), ErrArchived)
	assert.ErrorIs(t, tgt.RecordReport(domAgent), ErrArchived)
	assert.Equal(t, before, tgt.Snapshot())
}

func TestTargetAssignAgent(t *testing.T) {
	clock.Fix(t, domT0)
	tgt := domNewTarget(t)

	clock.Fix(t, domT1)
	require.NoError(t, tgt.AssignAgent(domAgent))
	assert.Equal(t, domAgent, *tgt.AgentPrincipalID())
	assert.Equal(t, domT1, tgt.UpdatedAt())

	require.NoError(t, tgt.RecordReport(domAgent))
	require.NotNil(t, tgt.LastReportedAt())

	t.Run("another agent replaces it and clears the last report", func(t *testing.T) {
		clock.Fix(t, domT2)
		require.NoError(t, tgt.AssignAgent(domOtherAgent))
		assert.Equal(t, domOtherAgent, *tgt.AgentPrincipalID())
		assert.Nil(t, tgt.LastReportedAt())
		assert.Equal(t, domT2, tgt.UpdatedAt())
		assert.ErrorIs(t, tgt.RecordReport(domAgent), ErrNotTargetAgent, "the replaced agent may no longer report")
	})

	t.Run("an empty principal is refused", func(t *testing.T) {
		before := tgt.Snapshot()
		assert.ErrorIs(t, tgt.AssignAgent(""), ErrInvalid)
		assert.Equal(t, before, tgt.Snapshot())
	})
}

func TestTargetUnassignAgent(t *testing.T) {
	clock.Fix(t, domT0)
	tgt := domNewTarget(t)
	require.NoError(t, tgt.AssignAgent(domAgent))
	require.NoError(t, tgt.RecordReport(domAgent))

	clock.Fix(t, domT1)
	require.NoError(t, tgt.UnassignAgent())
	assert.Nil(t, tgt.AgentPrincipalID())
	assert.Nil(t, tgt.LastReportedAt())
	assert.Equal(t, domT1, tgt.UpdatedAt())
	assert.ErrorIs(t, tgt.RecordReport(domAgent), ErrNotTargetAgent)

	clock.Fix(t, domT2)
	require.NoError(t, tgt.UnassignAgent())
	assert.Equal(t, domT1, tgt.UpdatedAt(), "unassigning with no agent does nothing")
}

func TestTargetUnassignAgentRefusesArchived(t *testing.T) {
	clock.Fix(t, domT0)
	tgt := domNewTarget(t)
	require.NoError(t, tgt.AssignAgent(domAgent))
	tgt.Archive()
	before := tgt.Snapshot()

	assert.ErrorIs(t, tgt.UnassignAgent(), ErrArchived)
	assert.Equal(t, before, tgt.Snapshot())
}

func TestTargetRecordReport(t *testing.T) {
	clock.Fix(t, domT0)
	tgt := domNewTarget(t)

	assert.ErrorIs(t, tgt.RecordReport(domAgent), ErrNotTargetAgent, "a target without an agent takes no reports")
	assert.Nil(t, tgt.LastReportedAt())

	require.NoError(t, tgt.AssignAgent(domAgent))

	clock.Fix(t, domT1)
	assert.ErrorIs(t, tgt.RecordReport(domOtherAgent), ErrNotTargetAgent)
	assert.ErrorIs(t, tgt.RecordReport(""), ErrNotTargetAgent)
	assert.Nil(t, tgt.LastReportedAt())

	require.NoError(t, tgt.RecordReport(domAgent))
	require.NotNil(t, tgt.LastReportedAt())
	assert.Equal(t, domT1, *tgt.LastReportedAt())
	assert.Equal(t, domT0, tgt.UpdatedAt(), "a report is not a configuration change")
}

func TestTargetArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	tgt := domNewTarget(t)

	clock.Fix(t, domT1)
	tgt.Archive()
	assert.True(t, tgt.Archived())
	assert.Equal(t, domT1, *tgt.DeletedAt())
	assert.Equal(t, domT1, tgt.UpdatedAt())

	clock.Fix(t, domT2)
	tgt.Archive()
	assert.Equal(t, domT1, *tgt.DeletedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, tgt.UpdatedAt())

	tgt.Restore()
	assert.False(t, tgt.Archived())
	assert.Nil(t, tgt.DeletedAt())
	assert.Equal(t, domT2, tgt.UpdatedAt())

	clock.Fix(t, domT0)
	tgt.Restore()
	assert.Equal(t, domT2, tgt.UpdatedAt(), "restoring an active target does nothing")
}
