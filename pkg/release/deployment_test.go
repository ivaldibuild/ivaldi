package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeploymentStatusTransitions(t *testing.T) {
	assert.True(t, CanMoveDeployment(DeploymentPending, DeploymentApplying))
	assert.True(t, CanMoveDeployment(DeploymentPending, DeploymentAwaitingApproval))
	assert.True(t, CanMoveDeployment(DeploymentAwaitingApproval, DeploymentCancelled))
	assert.True(t, CanMoveDeployment(DeploymentApplying, DeploymentProgressing))
	assert.True(t, CanMoveDeployment(DeploymentProgressing, DeploymentHealthy))
	assert.True(t, CanMoveDeployment(DeploymentHealthy, DeploymentDegraded))
	assert.True(t, CanMoveDeployment(DeploymentDegraded, DeploymentHealthy))
	assert.True(t, CanMoveDeployment(DeploymentHealthy, DeploymentSuperseded))

	assert.False(t, CanMoveDeployment(DeploymentHealthy, DeploymentApplying))
	assert.False(t, CanMoveDeployment(DeploymentFailed, DeploymentApplying))
	assert.False(t, CanMoveDeployment(DeploymentCancelled, DeploymentApplying))
	assert.Error(t, CheckDeploymentMove(DeploymentSuperseded, DeploymentHealthy))
}

func TestDeploymentStatusValidity(t *testing.T) {
	for _, s := range []DeploymentStatus{
		DeploymentPending, DeploymentAwaitingApproval, DeploymentRejected, DeploymentCancelled,
		DeploymentApplying, DeploymentProgressing, DeploymentHealthy, DeploymentDegraded,
		DeploymentFailed, DeploymentSuperseded,
	} {
		assert.True(t, s.Valid(), s)
	}
	assert.False(t, DeploymentStatus("waiting").Valid())
	assert.True(t, DeploymentFailed.Terminal())
	assert.False(t, DeploymentHealthy.Terminal())
}
