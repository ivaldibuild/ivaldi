// Package authorization is the contract for the single authorizer (ADR 0003).
//
// Callers do not choose between IAM and policy: they send one Request, the
// authorizer checks IAM (may this actor touch this resource at all?) and then
// policy (should this happen now?), and returns one Decision. The control
// plane's capabilities, the gateway, and workers all use this contract. Each
// capability declares its own permission names and actions.
package authorization

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Outcome is the authorizer's answer.
type Outcome string

const (
	OutcomeAllow            Outcome = "allow"
	OutcomeDeny             Outcome = "deny"
	OutcomeRequiresApproval Outcome = "requires_approval"
)

// Request asks whether Actor may use Permission on a resource.
type Request struct {
	Actor          identity.Actor
	Permission     string
	OrganizationID string
	// ResourceID is the IAM resource the permission is checked on, such as an
	// Organization, Workspace, or Project ID. Grants on its ancestors apply too.
	ResourceID string
	// Action names what is being done, such as "setting.change".
	Action string
	// Proposal describes the change for policy and for anyone asked to approve it.
	Proposal any
	// ApprovalID is the decision returned by an earlier request that required
	// approval. The authorizer allows the action only if that decision was
	// approved for exactly this proposal.
	ApprovalID string
}

// Decision is the authorizer's decision.
type Decision struct {
	ID      string
	Outcome Outcome
	Reason  string
	// ApprovedByPrincipalID is the human who approved the action, if it needed approval.
	ApprovedByPrincipalID string
}

// Authorizer is the single authorizer, implemented by IAM and policy together.
type Authorizer interface {
	Authorize(ctx context.Context, req Request) (Decision, error)
}

// Require asks a for a decision and returns it, with an error unless the
// action is allowed: an ApprovalRequiredError when it needs approval, and a
// DeniedError otherwise, including for an outcome it does not recognize.
func Require(ctx context.Context, a Authorizer, req Request) (Decision, error) {
	d, err := a.Authorize(ctx, req)
	if err != nil {
		return Decision{}, fmt.Errorf("authorize %s: %w", req.Action, err)
	}
	switch d.Outcome {
	case OutcomeAllow:
		return d, nil
	case OutcomeRequiresApproval:
		return d, &ApprovalRequiredError{DecisionID: d.ID}
	default:
		return d, &DeniedError{DecisionID: d.ID, Reason: d.Reason}
	}
}
