package policy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

func newTestRequest(t *testing.T) *ApprovalRequest {
	t.Helper()
	r, err := NewApprovalRequest(NewApprovalRequestProps{
		OrganizationID: testOrg,
		Subject:        Subject{Type: "delivery.deployment", ID: "dpl_1"},
		Decision:       authorization.Decision{ID: authorization.NewDecisionID(), Rule: "builtin.test", Reason: "an agent asked"},
		Action:         "deployment.create", Permission: "deployment.create", ResourceID: "env_1",
		ProposalDigest: strings.Repeat("0", 64), Summary: json.RawMessage(`{}`), ReadResourceID: "prj_1",
		RequestedByPrincipalID: "prn_agent", SessionID: new("ses_1"), OnBehalfOfPrincipalID: new("prn_ada"),
		Requirements: []Requirement{{Permission: "deployment.create", ResourceID: "env_1", ResourcePath: []string{"env_1", testOrg}}},
	}, nil)
	require.NoError(t, err)
	return r
}

func TestApprovalRequestCloses(t *testing.T) {
	clock.Fix(t, t0)
	r := newTestRequest(t)
	assert.Equal(t, []aggregate.Event{ApprovalRequestCreated{r.Snapshot()}}, r.PullEvents())
	assert.True(t, r.RequestedBy("prn_agent"))
	assert.True(t, r.RequestedBy("prn_ada"), "the person the session acts for asked too")
	assert.False(t, r.RequestedBy("prn_grace"))

	assert.ErrorIs(t, r.Reject("prn_grace", authorization.NewDecisionID(), ""), ErrInvalid, "a rejection says why")
	require.NoError(t, r.Reject("prn_grace", authorization.NewDecisionID(), "not during the freeze"))
	assert.Equal(t, ApprovalRejected, r.State())
	assert.Equal(t, []aggregate.Event{ApprovalRequestRejected{r.Snapshot()}}, r.PullEvents())

	assert.ErrorIs(t, r.Approve("prn_grace", authorization.NewDecisionID(), ""), ErrNotPending, "a closed request stays closed")
	assert.ErrorIs(t, r.Cancel("prn_grace", authorization.NewDecisionID(), ""), ErrNotPending)
}

func TestApprovalStateEnumTagMatchesTheDomain(t *testing.T) {
	f, _ := reflect.TypeOf(ApprovalRequestSnapshot{}).FieldByName("State")
	var states []ApprovalState
	for _, v := range strings.Split(f.Tag.Get("enum"), ",") {
		states = append(states, ApprovalState(v))
	}
	assert.ElementsMatch(t, []ApprovalState{ApprovalPending, ApprovalApproved, ApprovalRejected, ApprovalCancelled}, states)
}
