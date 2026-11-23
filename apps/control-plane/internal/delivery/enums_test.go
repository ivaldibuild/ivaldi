package delivery

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// enumTag returns the values a struct field's enum tag lists, which the
// API's description publishes for clients.
func enumTag(t *testing.T, v any, field string) []string {
	t.Helper()
	f, ok := reflect.TypeOf(v).FieldByName(field)
	if !ok {
		t.Fatalf("%T has no field %s", v, field)
	}
	return strings.Split(f.Tag.Get("enum"), ",")
}

// The enum tags on snapshots must list exactly the values the domain uses,
// or the clients generated from the API's description refuse real answers.
func TestSnapshotEnumTagsMatchTheDomain(t *testing.T) {
	assert.ElementsMatch(t, []Tier{TierDevelopment, TierStaging, TierProduction, TierOther}, toTier(enumTag(t, EnvironmentSnapshot{}, "Tier")))
	assert.ElementsMatch(t, []Tier{TierDevelopment, TierStaging, TierProduction, TierOther}, toTier(enumTag(t, DeploymentApprovalSummary{}, "EnvironmentTier")))
	assert.ElementsMatch(t, []string{string(DeploymentRollout), string(DeploymentRedeploy), string(DeploymentRollback)},
		enumTag(t, DeploymentApprovalSummary{}, "Reason"))
	kinds := []string{string(ArtifactContainerImage), string(ArtifactHelmChart), string(ArtifactArchive)}
	assert.ElementsMatch(t, kinds, enumTag(t, ArtifactSnapshot{}, "ArtifactKind"))
	assert.ElementsMatch(t, kinds, enumTag(t, BuildSnapshot{}, "ArtifactKind"))
	assert.ElementsMatch(t, []string{string(ArtifactDeclared), string(ArtifactObserved)}, enumTag(t, ArtifactSnapshot{}, "Source"))
	assert.ElementsMatch(t, []string{string(release.RuleVersion), string(release.RuleGitRef)}, enumTag(t, ChannelRuleSnapshot{}, "Kind"))
	assert.ElementsMatch(t, []string{string(PlacementConfigured), string(PlacementObserved)}, enumTag(t, PlacementSnapshot{}, "Source"))
	assert.ElementsMatch(t, []string{string(BuildCIReported), string(BuildObserved)}, enumTag(t, BuildSnapshot{}, "Provenance"))
	assert.ElementsMatch(t, []string{string(release.StatusDraft), string(release.StatusPublished), string(release.StatusWithdrawn)}, enumTag(t, ReleaseSnapshot{}, "Status"))
	assert.ElementsMatch(t, []string{string(ReleaseCreated), string(ReleaseObserved)}, enumTag(t, ReleaseSnapshot{}, "Source"))
	assert.ElementsMatch(t, []string{string(VariableKindText), string(VariableKindSecret)}, enumTag(t, VariableSnapshot{}, "Kind"))
	assert.ElementsMatch(t, []string{string(ReleaseVariablesCurrent), string(ReleaseVariablesSuperseded)}, enumTag(t, ReleaseVariablesSnapshot{}, "Status"))
	assert.ElementsMatch(t, []string{string(VariableDeliverAsSecret), string(VariableDeliverAsValue)}, enumTag(t, VariableSnapshot{}, "DeliverAs"))
	assert.ElementsMatch(t, []string{string(FindingOutsideLifecycle), string(FindingPhaseNotOpen), string(FindingWouldRequireApproval), string(FindingWouldDeny)},
		enumTag(t, DeploymentSnapshot{}, "Findings"))
}

// The assignment is the agent's contract, frozen from slice 5 on: its enum
// tags must list exactly the domain's values, or the agent's generated client
// refuses a real assignment.
func TestAssignmentEnumTagsMatchTheDomain(t *testing.T) {
	assert.ElementsMatch(t, []string{string(DeployerHelm)}, enumTag(t, ApplyAssignment{}, "Deployer"))
	assert.ElementsMatch(t, []string{string(DeploymentRollout), string(DeploymentRedeploy), string(DeploymentRollback)},
		enumTag(t, ApplyAssignment{}, "Reason"))
	sources := []string{string(VariableSourceProject), string(VariableSourceApplication)}
	assert.ElementsMatch(t, sources, enumTag(t, AssignmentVariable{}, "Source"))
	assert.ElementsMatch(t, sources, enumTag(t, AssignmentSecret{}, "Source"))
	assert.ElementsMatch(t, []string{string(VariableKindText), string(VariableKindSecret)}, enumTag(t, AssignmentVariable{}, "Kind"))
	deliverAs := []string{string(VariableDeliverAsSecret), string(VariableDeliverAsValue)}
	assert.ElementsMatch(t, deliverAs, enumTag(t, AssignmentVariable{}, "DeliverAs"))
	assert.ElementsMatch(t, deliverAs, enumTag(t, AssignmentSecret{}, "DeliverAs"))
	assert.ElementsMatch(t, []string{string(StoreKubernetes), string(StoreAWSSecretsManager), string(StoreVault), string(StoreDoppler)},
		enumTag(t, StoreSettings{}, "Kind"))
	assert.ElementsMatch(t, []string{string(StoreAccessAgent), string(StoreAccessFederated)}, enumTag(t, StoreAccess{}, "Mode"))
}

func toTier(s []string) []Tier {
	out := make([]Tier, len(s))
	for i, v := range s {
		out[i] = Tier(v)
	}
	return out
}
