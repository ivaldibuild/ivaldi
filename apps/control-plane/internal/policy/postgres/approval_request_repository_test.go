package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// digest is the SHA-256 of an empty proposal.
const digest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

const (
	envProd    = "env_prod"
	envStaging = "env_staging"
	project    = "prj_web"
	ada        = "prn_ada"
	grace      = "prn_grace"
	agent      = "prn_agent"
)

func newDeployment() policy.Subject {
	return policy.Subject{Type: "delivery.deployment", ID: "dpl_" + id.ULID()}
}

// newRequest opens a request for a Deployment to the Environment, asked by
// requestedBy, optionally through an agent Session acting for onBehalfOf.
func newRequest(t *testing.T, subject policy.Subject, env, requestedBy string, onBehalfOf *string) *policy.ApprovalRequest {
	t.Helper()
	props := policy.NewApprovalRequestProps{
		OrganizationID: orgA,
		Subject:        subject,
		Decision:       authorization.Decision{ID: authorization.NewDecisionID(), Rule: "builtin.test", Reason: "an agent asked"},
		Action:         "deployment.create", Permission: "deployment.create", ResourceID: env, ProposalDigest: digest,
		Summary: json.RawMessage(`{"environment_id":"` + env + `"}`), ReadResourceID: project,
		RequestedByPrincipalID: requestedBy, OnBehalfOfPrincipalID: onBehalfOf,
		Requirements: []policy.Requirement{
			{Permission: "deployment.create", ResourceID: env, ResourcePath: []string{env, wsA, orgA}},
			{Permission: "application.read", ResourceID: project, ResourcePath: []string{project, wsA, orgA}},
		},
	}
	if onBehalfOf != nil {
		props.SessionID = new("ses_1")
	}
	r, err := policy.NewApprovalRequest(props, nil)
	require.NoError(t, err)
	return r
}

func TestApprovalRequestRepository(t *testing.T) {
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, t0)
	repo := NewApprovalRequestRepository(db)
	ctx := context.Background()

	prod := newRequest(t, newDeployment(), envProd, agent, new(ada))
	require.NoError(t, repo.CreateApprovalRequest(ctx, prod))
	got, err := repo.GetApprovalRequest(ctx, orgA, prod.ID())
	require.NoError(t, err)
	want, stored := prod.Snapshot(), got.Snapshot()
	assert.JSONEq(t, string(want.Summary), string(stored.Summary), "PostgreSQL keeps the summary's JSON, not its spacing")
	want.Summary, stored.Summary = nil, nil
	assert.Equal(t, want, stored, "a request survives a round trip")

	twin := newRequest(t, prod.Subject(), envProd, agent, nil)
	assert.ErrorIs(t, repo.CreateApprovalRequest(ctx, twin), policy.ErrConflict, "a subject has one pending request")

	staging := newRequest(t, newDeployment(), envStaging, grace, nil)
	require.NoError(t, repo.CreateApprovalRequest(ctx, staging))
	cancelled := newRequest(t, newDeployment(), envStaging, agent, nil)
	require.NoError(t, repo.CreateApprovalRequest(ctx, cancelled))
	locked, err := repo.GetPendingApprovalRequestForUpdate(ctx, orgA, cancelled.Subject())
	require.NoError(t, err)
	require.NoError(t, locked.Cancel(ada, authorization.NewDecisionID(), "release withdrawn"))
	require.NoError(t, repo.UpdateApprovalRequest(ctx, locked))
	_, err = repo.GetPendingApprovalRequestForUpdate(ctx, orgA, cancelled.Subject())
	assert.ErrorIs(t, err, policy.ErrNotFound, "a cancelled request is no longer pending")

	permissions, err := repo.ListRequiredPermissions(ctx, orgA)
	require.NoError(t, err)
	assert.Equal(t, []string{"application.read", "deployment.create"}, permissions)

	list := func(decider string, state policy.ApprovalState, held map[string][]string) []string {
		t.Helper()
		page, err := repo.ListDecidableApprovalRequests(ctx, policy.ListDecidableQuery{
			OrganizationID: orgA, DeciderID: decider, Held: held, State: state,
		})
		require.NoError(t, err)
		ids := make([]string, len(page.Items))
		for i, r := range page.Items {
			ids[i] = r.ID()
		}
		return ids
	}
	everywhere := map[string][]string{"deployment.create": {orgA}, "application.read": {orgA}}
	assert.Equal(t, []string{prod.ID()}, list(grace, policy.ApprovalPending, everywhere),
		"grace asked for the staging request, so it is left out")
	assert.Equal(t, []string{staging.ID()}, list(ada, policy.ApprovalPending, everywhere),
		"ada's agent session asked for the production request, so it is left out")
	assert.Equal(t, []string{cancelled.ID()}, list(grace, policy.ApprovalCancelled, everywhere), "the filter is the same in every state")

	onlyStaging := map[string][]string{"deployment.create": {envStaging}, "application.read": {project}}
	assert.Empty(t, list(grace, policy.ApprovalPending, onlyStaging), "deploying to staging does not decide production")
	assert.Equal(t, []string{staging.ID()}, list(ada, policy.ApprovalPending, onlyStaging))
	noRead := map[string][]string{"deployment.create": {orgA}}
	assert.Empty(t, list(ada, policy.ApprovalPending, noRead), "every requirement must be held")
}
