package delivery_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

var (
	deploymentApp   = "app_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	deploymentRel   = "rel_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	deploymentEnv   = "env_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	deploymentTgt   = "tgt_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	deploymentStart = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
)

func deploymentProps() delivery.NewDeploymentProps {
	return delivery.NewDeploymentProps{
		OrganizationID: "org_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ApplicationID:  deploymentApp, ReleaseID: deploymentRel,
		EnvironmentID: deploymentEnv, TargetID: deploymentTgt,
	}
}

func TestNewDeployment(t *testing.T) {
	clock.Fix(t, deploymentStart)
	d, err := delivery.NewDeployment(deploymentProps(), nil)
	require.NoError(t, err)
	assert.Equal(t, delivery.DeploymentExecuted, d.Source())
	assert.Equal(t, delivery.DeploymentRollout, d.Reason())
	assert.Equal(t, release.DeploymentPending, d.Status())
	assert.Equal(t, deploymentStart, d.RequestedAt())
	assert.Contains(t, d.PullEvents(), delivery.DeploymentCreated{d.Snapshot()})
}

func TestDeploymentWaitForUpdatesItsCreatedEvent(t *testing.T) {
	clock.Fix(t, deploymentStart)
	props := deploymentProps()
	props.Status = release.DeploymentAwaitingApproval
	d, err := delivery.NewDeployment(props, nil)
	require.NoError(t, err)

	require.NoError(t, d.WaitFor("apr_01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	events := d.PullEvents()
	assert.Equal(t, []aggregate.Event{delivery.DeploymentCreated{d.Snapshot()}}, events,
		"the created event names the approval request the stored deployment waits for")
	assert.Equal(t, new("apr_01ARZ3NDEKTSV4RRFFQ69G5FAV"), d.Snapshot().ApprovalRequestID)
}

func TestDeploymentStatusChanges(t *testing.T) {
	clock.Fix(t, deploymentStart)
	d, err := delivery.NewDeployment(deploymentProps(), nil)
	require.NoError(t, err)
	d.PullEvents()

	at := deploymentStart.Add(time.Minute)
	clock.Fix(t, at)
	require.NoError(t, d.MoveTo(release.DeploymentApplying))
	assert.Equal(t, release.DeploymentApplying, d.Status())
	assert.Equal(t, &at, d.StartedAt())
	events := d.PullEvents()
	require.Len(t, events, 1)
	changed, ok := events[0].(delivery.DeploymentStatusChanged)
	require.True(t, ok)
	assert.Equal(t, delivery.DeploymentStatusChanged{
		Deployment: d.Snapshot(), PreviousStatus: release.DeploymentPending,
	}, changed)

	clock.Fix(t, at.Add(time.Minute))
	require.NoError(t, d.MoveTo(release.DeploymentProgressing))
	require.NoError(t, d.MoveTo(release.DeploymentHealthy))
	assert.Equal(t, &at, d.StartedAt())
	assert.NotNil(t, d.FinishedAt())
	assert.NotNil(t, d.SucceededAt())
	firstSuccess := *d.SucceededAt()

	clock.Fix(t, at.Add(time.Hour))
	require.NoError(t, d.MoveTo(release.DeploymentDegraded))
	assert.Equal(t, firstSuccess, *d.SucceededAt(), "succeeded_at keeps the first healthy time")
	assert.NotNil(t, d.FinishedAt(), "finished_at does not move on later health changes")

	assert.ErrorIs(t, d.MoveTo(release.DeploymentApplying), delivery.ErrDeploymentStatus)
}

func TestNewDeploymentRefusesInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*delivery.NewDeploymentProps)
	}{
		{"no organization", func(p *delivery.NewDeploymentProps) { p.OrganizationID = "" }},
		{"no application", func(p *delivery.NewDeploymentProps) { p.ApplicationID = "" }},
		{"bad target", func(p *delivery.NewDeploymentProps) { p.TargetID = "not-a-target" }},
		{"unknown source", func(p *delivery.NewDeploymentProps) { p.Source = "imported" }},
		{"unknown reason", func(p *delivery.NewDeploymentProps) { p.Reason = "experiment" }},
		{"unknown status", func(p *delivery.NewDeploymentProps) { p.Status = "waiting" }},
		{"bad external ref", func(p *delivery.NewDeploymentProps) { p.ExternalRef = []byte("{") }},
		{"findings on an executed deployment", func(p *delivery.NewDeploymentProps) {
			p.Findings = []delivery.Finding{delivery.FindingOutsideLifecycle}
		}},
		{"unknown finding", func(p *delivery.NewDeploymentProps) {
			p.Source = delivery.DeploymentObserved
			p.Findings = []delivery.Finding{"looked_odd"}
		}},
		{"a finding twice", func(p *delivery.NewDeploymentProps) {
			p.Source = delivery.DeploymentObserved
			p.Findings = []delivery.Finding{delivery.FindingPhaseNotOpen, delivery.FindingPhaseNotOpen}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := deploymentProps()
			tt.mutate(&props)
			_, err := delivery.NewDeployment(props, nil)
			assert.ErrorIs(t, err, delivery.ErrInvalid)
		})
	}
}
