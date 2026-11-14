package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
)

func TestFromAPI_PullsTheChartFromWhereItIsPublished(t *testing.T) {
	var a apiclient.DeploymentAssignment
	a.Apply.Definition = apiclient.AssignmentArtifact{
		ArtifactName: "chart", PublishedAs: "ghcr.io/acme/charts/payments", Digest: "sha256:chart",
	}

	got := FromAPI(a, "prod-eu")

	assert.Equal(t, "ghcr.io/acme/charts/payments@sha256:chart", got.Definition.Ref())
}
