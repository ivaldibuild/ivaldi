// Package authorizer implements authorization.Authorizer from IAM's grants:
// it allows a request when the actor holds a role, on the resource or one of
// its ancestors, that includes the requested permission. It follows the
// evaluation in docs/design/iam-data-model.md.
//
// This is IAM's half of the single authorizer (ADR 0003). Policy, the other
// half, does not exist yet, so nothing requires approval: a request that
// carries an ApprovalID is denied, since no approval could have been given.
// Agent Sessions are denied until their delegator and scope are evaluated.
//
// A platform request, on authorization.PlatformResource with no Organization,
// is allowed only for an active platform workload whose component holds the
// permission (ADR 0015).
package authorizer

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

type Option func(*options)

type options struct {
	access    iam.AccessRepository
	workloads PlatformWorkloads
	sessions  AgentSessions
	resources ResourceAncestry
	logger    logging.Logger
}

// PlatformWorkloads reads platform workloads. iam.WorkloadRepository
// provides it.
type PlatformWorkloads interface {
	GetPlatform(ctx context.Context, principalID string) (*iam.Workload, error)
}

// AgentSessions reads the agent Sessions that act as actors.
// iam.AgentSessionRepository implements it.
type AgentSessions interface {
	Get(ctx context.Context, organizationID, principalID string) (*iam.AgentSession, error)
}

// ResourceAncestry reads a resource's path up the hierarchy, to check it
// against a Session's scope. iam.ResourceRepository implements it.
type ResourceAncestry interface {
	ResourceAncestors(ctx context.Context, organizationID, resourceID string) ([]string, error)
}

func WithAccessRepo(r iam.AccessRepository) Option { return func(o *options) { o.access = r } }

func WithAgentSessions(r AgentSessions) Option { return func(o *options) { o.sessions = r } }

func WithResourceAncestry(r ResourceAncestry) Option { return func(o *options) { o.resources = r } }

// WithPlatformWorkloads reads the platform workloads that platform requests
// come from.
func WithPlatformWorkloads(r PlatformWorkloads) Option { return func(o *options) { o.workloads = r } }

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
	case o.workloads == nil:
		return nil, errors.New("authorizer: PlatformWorkloads is required")
	case o.sessions == nil:
		return nil, errors.New("authorizer: AgentSessions is required")
	case o.resources == nil:
		return nil, errors.New("authorizer: ResourceAncestry is required")
	case o.logger == nil:
		return nil, errors.New("authorizer: Logger is required")
	}
	return &authorizer{options: o}, nil
}

type authorizer struct {
	options
}

// Authorize decides req. It returns an error only for a malformed request or
// a failure to read access; every refusal is a deny decision with a reason.
func (a *authorizer) Authorize(ctx context.Context, req authorization.Request) (authorization.Decision, error) {
	if err := check(req); err != nil {
		return authorization.Decision{}, err
	}
	d := authorization.Decision{ID: authorization.NewDecisionID()}
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
	if req.ResourceID == authorization.PlatformResource {
		return a.platformRefusal(ctx, req)
	}
	// An agent Session acts with its delegator's access, narrowed to the
	// Session's scope (ADR 0003).
	holder, holderName := req.Actor.PrincipalID, "the actor"
	var session *iam.AgentSession
	switch req.Actor.Kind {
	case identity.ActorUser, identity.ActorServiceAccount:
	case identity.ActorAgentSession:
		var reason string
		var err error
		if session, reason, err = a.usableSession(ctx, req); err != nil || reason != "" {
			return reason, err
		}
		holder, holderName = session.DelegatorPrincipalID(), "the agent session's delegator"
	default:
		return fmt.Sprintf("a %q actor holds no grants", req.Actor.Kind), nil
	}
	if req.ApprovalID != "" {
		return "approvals are not available yet", nil
	}
	reason, err := a.grantRefusal(ctx, req, holder, holderName)
	if err != nil || reason != "" || session == nil {
		return reason, err
	}
	return a.scopeRefusal(ctx, req, session)
}

// usableSession reads the acting agent Session, and refuses one that is
// revoked, expired, or no longer active.
func (a *authorizer) usableSession(ctx context.Context, req authorization.Request) (*iam.AgentSession, string, error) {
	session, err := a.sessions.Get(ctx, req.OrganizationID, req.Actor.PrincipalID)
	if errors.Is(err, iam.ErrNotFound) {
		return nil, "the actor is not an agent session in the organization", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("authorize %s: read agent session: %w", req.Action, err)
	}
	if err := session.CheckUsable(); err != nil {
		return nil, err.Error(), nil
	}
	return session, "", nil
}

// grantRefusal checks that holder, a member of the Organization, holds the
// permission through a role granted on the resource or above it.
func (a *authorizer) grantRefusal(ctx context.Context, req authorization.Request, holder, holderName string) (string, error) {
	access, err := a.access.Access(ctx, iam.AccessQuery{
		OrganizationID: req.OrganizationID,
		PrincipalID:    holder,
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
		return holderName + " is not a member of the organization", nil
	case access.PrincipalStatus != iam.PrincipalActive:
		return fmt.Sprintf("%s is %s", holderName, access.PrincipalStatus), nil
	case !access.ResourceFound:
		return "the resource does not exist in the organization", nil
	}
	for _, roleID := range access.RoleIDs {
		if key, ok := iam.BuiltInRoleKey(roleID); ok && iam.BuiltInRoleAllows(key, req.Permission) {
			return "", nil
		}
	}
	return fmt.Sprintf("no role granted to %s on the resource or above it includes %s", holderName, req.Permission), nil
}

// scopeRefusal refuses a resource outside the Session's scope: neither it
// nor any resource above it is one the Session was limited to.
func (a *authorizer) scopeRefusal(ctx context.Context, req authorization.Request, session *iam.AgentSession) (string, error) {
	path, err := a.resources.ResourceAncestors(ctx, req.OrganizationID, req.ResourceID)
	if err != nil {
		return "", fmt.Errorf("authorize %s: read resource path: %w", req.Action, err)
	}
	if !session.Covers(path) {
		return "the resource is outside the agent session's scope", nil
	}
	return "", nil
}

func (a *authorizer) platformRefusal(ctx context.Context, req authorization.Request) (string, error) {
	if req.Actor.Kind != identity.ActorWorkload {
		return fmt.Sprintf("a %q actor cannot make a platform request", req.Actor.Kind), nil
	}
	if req.ApprovalID != "" {
		return "approvals are not available yet", nil
	}
	w, err := a.workloads.GetPlatform(ctx, req.Actor.PrincipalID)
	if errors.Is(err, iam.ErrNotFound) {
		return "the actor is not a platform workload", nil
	}
	if err != nil {
		return "", fmt.Errorf("authorize %s: read platform workload: %w", req.Action, err)
	}
	if !w.Active() {
		return fmt.Sprintf("the platform workload is %s", w.Status()), nil
	}
	if !iam.PlatformComponentAllows(w.Component(), req.Permission) {
		return fmt.Sprintf("a platform %s does not hold %s", w.Component(), req.Permission), nil
	}
	return "", nil
}

// check refuses a request missing what every decision needs: that is a bug in
// the caller, not something to deny. A platform request has no Organization,
// and any other request must not use the platform resource without one.
func check(req authorization.Request) error {
	var missing []error
	platform := req.ResourceID == authorization.PlatformResource
	switch {
	case platform && req.OrganizationID != "":
		missing = append(missing, errors.New("a platform request has no organization ID"))
	case !platform && req.OrganizationID == "":
		missing = append(missing, errors.New("organization ID is required"))
	}
	for _, f := range []struct{ name, value string }{
		{"permission", req.Permission},
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
