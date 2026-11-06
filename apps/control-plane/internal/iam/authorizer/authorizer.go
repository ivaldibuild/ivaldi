// Package authorizer implements authorization.Authorizer from IAM's grants:
// it allows a request when the actor holds a role, on the resource or one of
// its ancestors, that includes the requested permission. It follows the
// evaluation in docs/design/iam-data-model.md.
//
// This is IAM's half of the single authorizer (ADR 0003). Policy, the other
// half, does not exist yet, so nothing requires approval: a request that
// carries an ApprovalID is denied, since no approval could have been given.
// Agent Sessions are denied until their delegator and scope are evaluated.
package authorizer

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// decisionPrefix starts every decision's ID. Audit records carry it, so a
// change can be traced to the decision that allowed it.
const decisionPrefix = "dcn"

func init() { id.MustRegister(decisionPrefix, "authorization.Decision") }

type Option func(*options)

type options struct {
	access iam.AccessRepository
	logger logging.Logger
}

func WithAccessRepo(r iam.AccessRepository) Option { return func(o *options) { o.access = r } }

// WithLogger logs each denial at debug level, with its decision ID.
func WithLogger(l logging.Logger) Option { return func(o *options) { o.logger = l } }

// New returns the authorizer, or an error naming a missing dependency.
func New(opts ...Option) (authorization.Authorizer, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.access == nil:
		return nil, errors.New("authorizer: AccessRepo is required")
	case o.logger == nil:
		return nil, errors.New("authorizer: Logger is required")
	}
	return &authorizer{access: o.access, logger: o.logger}, nil
}

type authorizer struct {
	access iam.AccessRepository
	logger logging.Logger
}

// Authorize decides req. It returns an error only for a malformed request or
// a failure to read access; every refusal is a deny decision with a reason.
func (a *authorizer) Authorize(ctx context.Context, req authorization.Request) (authorization.Decision, error) {
	if err := check(req); err != nil {
		return authorization.Decision{}, err
	}
	d := authorization.Decision{ID: id.New(decisionPrefix)}
	reason, err := a.refusal(ctx, req)
	if err != nil {
		return authorization.Decision{}, err
	}
	if reason != "" {
		d.Outcome, d.Reason = authorization.OutcomeDeny, reason
		a.logger.Debug("authorization denied",
			logging.F("decision_id", d.ID),
			logging.F("action", req.Action),
			logging.F("permission", req.Permission),
			logging.F("principal_id", req.Actor.PrincipalID),
			logging.F("resource_id", req.ResourceID),
			logging.F("reason", reason))
		return d, nil
	}
	d.Outcome = authorization.OutcomeAllow
	return d, nil
}

// refusal returns why req is refused, or "" when it is allowed.
func (a *authorizer) refusal(ctx context.Context, req authorization.Request) (string, error) {
	switch req.Actor.Kind {
	case identity.ActorUser, identity.ActorServiceAccount:
	case identity.ActorAgentSession:
		return "agent sessions cannot be authorized yet", nil
	default:
		return fmt.Sprintf("a %q actor holds no grants", req.Actor.Kind), nil
	}
	if req.ApprovalID != "" {
		return "approvals are not available yet", nil
	}

	access, err := a.access.Access(ctx, iam.AccessQuery{
		OrganizationID: req.OrganizationID,
		PrincipalID:    req.Actor.PrincipalID,
		ResourceID:     req.ResourceID,
		At:             clock.Now(),
	})
	if err != nil {
		return "", fmt.Errorf("authorize %s: read access: %w", req.Action, err)
	}
	switch {
	case !access.OrganizationActive:
		return "the organization is not active", nil
	case access.PrincipalStatus == "":
		return "the actor is not a member of the organization", nil
	case access.PrincipalStatus != iam.PrincipalActive:
		return fmt.Sprintf("the actor is %s", access.PrincipalStatus), nil
	case !access.ResourceFound:
		return "the resource does not exist in the organization", nil
	}
	for _, roleID := range access.RoleIDs {
		if key, ok := iam.BuiltInRoleKey(roleID); ok && iam.BuiltInRoleAllows(key, req.Permission) {
			return "", nil
		}
	}
	return fmt.Sprintf("no role granted on the resource or above it includes %s", req.Permission), nil
}

// check refuses a request missing what every decision needs: that is a bug in
// the caller, not something to deny.
func check(req authorization.Request) error {
	var missing []error
	for _, f := range []struct{ name, value string }{
		{"permission", req.Permission},
		{"organization ID", req.OrganizationID},
		{"resource ID", req.ResourceID},
		{"action", req.Action},
		{"actor principal ID", req.Actor.PrincipalID},
	} {
		if f.value == "" {
			missing = append(missing, fmt.Errorf("%s is required", f.name))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("authorize %q: %w", req.Action, errors.Join(missing...))
	}
	return nil
}
