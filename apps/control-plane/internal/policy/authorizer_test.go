package policy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

type iamAnswer authorization.Outcome

func (o iamAnswer) Authorize(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{ID: authorization.NewDecisionID(), Outcome: authorization.Outcome(o)}, nil
}

type requestReader struct{ r *ApprovalRequest }

func (f requestReader) GetApprovalRequest(context.Context, string, string) (*ApprovalRequest, error) {
	return f.r, nil
}

func TestAuthorizerApproverRule(t *testing.T) {
	r := newTestRequest(t)
	grace := identity.Actor{PrincipalID: "prn_grace", Kind: identity.ActorUser}
	decide := func(iam authorization.Outcome, actor identity.Actor, action string) authorization.Decision {
		t.Helper()
		d, err := NewAuthorizer(iamAnswer(iam), requestReader{r}).Authorize(context.Background(), authorization.Request{
			Actor: actor, Action: action, Proposal: DecisionProposal{ApprovalRequestID: r.ID()},
		})
		require.NoError(t, err)
		return d
	}

	approved := decide(authorization.OutcomeAllow, grace, ActionApprovalApprove)
	assert.Equal(t, authorization.OutcomeAllow, approved.Outcome)
	assert.Equal(t, "prn_grace", approved.ApprovedByPrincipalID, "an approval names the approver")
	rejected := decide(authorization.OutcomeAllow, grace, ActionApprovalReject)
	assert.Equal(t, authorization.OutcomeAllow, rejected.Outcome)
	assert.Empty(t, rejected.ApprovedByPrincipalID)

	for name, c := range map[string]struct {
		iam   authorization.Outcome
		actor identity.Actor
	}{
		"the person the session acts for": {authorization.OutcomeAllow, identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}},
		"a service account":               {authorization.OutcomeAllow, identity.Actor{PrincipalID: "prn_ci", Kind: identity.ActorServiceAccount}},
		"a decision that would wait":      {authorization.OutcomeRequiresApproval, grace},
	} {
		t.Run(name, func(t *testing.T) {
			d := decide(c.iam, c.actor, ActionApprovalApprove)
			assert.Equal(t, authorization.OutcomeDeny, d.Outcome)
			assert.Equal(t, RuleApprover, d.Rule)
		})
	}
	assert.Equal(t, authorization.OutcomeDeny, decide(authorization.OutcomeDeny, grace, ActionApprovalApprove).Outcome,
		"IAM's denial stands")
}
