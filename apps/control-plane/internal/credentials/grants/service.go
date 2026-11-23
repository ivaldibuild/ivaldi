// Package grants grants a Cluster's agent access to the Secret Stores a
// Deployment reads (ADR 0017). The agent names only the Deployment: the
// control plane works out which stores its secrets are in, checks them
// again, and grants each store in a mode that needs one. Every grant is
// recorded and audited; what is granted never is.
package grants

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/federation"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Assignments reads one Deployment's assignment for its Cluster's agent,
// with its stores checked again. delivery.DeploymentService provides it.
type Assignments interface {
	GetDeploymentAssignment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*delivery.DeploymentAssignment, error)
}

// Applications reads the Application a Deployment is of, for its Project.
// delivery.ApplicationRepository provides it.
type Applications interface {
	GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error)
}

// Grant is access to one store, for one Deployment. JWT is a credential:
// it goes to the agent and nowhere else.
type Grant struct {
	StoreID   string
	Mode      credentials.AccessMode
	ExpiresAt time.Time
	JWT       string
}

// Service grants the Secret Store access a Deployment needs.
type Service interface {
	// Grant grants access to every store in a mode that needs a grant
	// among those the Deployment's secrets are in, or to none: the caller
	// must be the agent of the Cluster the Deployment's Target is in, and
	// the Deployment applying. It returns no grant for a Deployment whose
	// stores need none.
	Grant(ctx context.Context, caller identity.Caller, deploymentID string) ([]Grant, error)
}

type Option func(*service)

func WithAssignments(a Assignments) Option   { return func(s *service) { s.assignments = a } }
func WithApplications(a Applications) Option { return func(s *service) { s.applications = a } }
func WithGrantRepo(r credentials.SecretStoreGrantRepository) Option {
	return func(s *service) { s.grants = r }
}
func WithIssuer(i federation.Issuer) Option           { return func(s *service) { s.issuer = i } }
func WithTransactor(tx transaction.Transactor) Option { return func(s *service) { s.w.Tx = tx } }
func WithAuditRecorder(r audit.Recorder) Option       { return func(s *service) { s.w.Audit = r } }

// NewService returns the grants service, or an error naming a missing
// dependency. Without an issuer, a federated store is never granted.
func NewService(opts ...Option) (Service, error) {
	s := &service{}
	for _, opt := range opts {
		opt(s)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Assignments", s.assignments != nil},
		{"Applications", s.applications != nil},
		{"SecretStoreGrantRepository", s.grants != nil},
		{"Transactor", s.w.Tx != nil},
		{"Audit Recorder", s.w.Audit != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("secret store grants: %s is required", r.name)
		}
	}
	return s, nil
}

type service struct {
	assignments  Assignments
	applications Applications
	grants       credentials.SecretStoreGrantRepository
	issuer       federation.Issuer
	// w records audit records; grants raise no event, so it has no outbox.
	w change.Writer
}

func (s *service) Grant(ctx context.Context, caller identity.Caller, deploymentID string) ([]Grant, error) {
	if caller.Actor.Kind != identity.ActorWorkload {
		return nil, delivery.ErrNotClusterAgent
	}
	// Built as the agent's assignment is, so the stores are the ones its
	// secrets name, checked again: still active, allowed for the
	// Environment, and usable on this installation.
	a, err := s.assignments.GetDeploymentAssignment(ctx, caller.Actor, caller.OrganizationID, deploymentID)
	if err != nil {
		return nil, err
	}
	if a.Deployment.Status != release.DeploymentApplying {
		return nil, fmt.Errorf("%w: deployment %s is %s, not applying", delivery.ErrDeploymentStatus, deploymentID, a.Deployment.Status)
	}
	var stores []string
	for _, secret := range a.Apply.Secrets {
		if secret.Store.Access.Mode == delivery.StoreAccessFederated && !slices.Contains(stores, secret.Store.ID) {
			stores = append(stores, secret.Store.ID)
		}
	}
	if len(stores) == 0 {
		return []Grant{}, nil
	}
	if s.issuer == nil {
		return nil, delivery.NewErrInvalidInput("the deployment's secrets are in a federated secret store: " + credentials.ErrNoFederation.Error())
	}
	app, err := s.applications.GetApplication(ctx, caller.OrganizationID, a.Deployment.ApplicationID)
	if err != nil {
		return nil, fmt.Errorf("get application %s: %w", a.Deployment.ApplicationID, err)
	}

	out := make([]Grant, 0, len(stores))
	records := make([]*credentials.SecretStoreGrant, 0, len(stores))
	for _, storeID := range stores {
		tok, err := s.issuer.Issue(federation.Grant{
			OrganizationID: caller.OrganizationID, WorkspaceID: a.Environment.WorkspaceID, ProjectID: app.ProjectID(),
			ApplicationID: a.Deployment.ApplicationID, EnvironmentID: a.Deployment.EnvironmentID,
			EnvironmentTier: string(a.Environment.Tier), TargetID: a.Deployment.TargetID, ClusterID: a.Target.ClusterID,
			DeploymentID: deploymentID, ReleaseID: a.Deployment.ReleaseID, StoreID: storeID,
		})
		if err != nil {
			return nil, err
		}
		record, err := credentials.NewSecretStoreGrant(credentials.NewSecretStoreGrantProps{
			OrganizationID: caller.OrganizationID, StoreID: storeID, DeploymentID: deploymentID,
			Mode: credentials.AccessFederated, TokenID: tok.ID, ExpiresAt: tok.ExpiresAt,
		}, nil)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
		out = append(out, Grant{StoreID: storeID, Mode: credentials.AccessFederated, ExpiresAt: tok.ExpiresAt, JWT: tok.Value})
	}

	// The agent is authenticated by its workload credential, not a grant
	// of IAM's, but each record still carries the decision that allowed it.
	decision := authorization.Decision{
		ID: authorization.NewDecisionID(), Outcome: authorization.OutcomeAllow,
		Reason: "the cluster's agent asked for the secret stores its applying deployment reads",
	}
	ctx = authorization.WithDecision(ctx, "", decision)
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.grants.CreateSecretStoreGrants(ctx, records); err != nil {
			return fmt.Errorf("record grants: %w", err)
		}
		for _, r := range records {
			if err := s.w.AuditRecord(ctx, audit.Event{
				Type: credentials.EventSecretStoreAccessGranted, OrganizationID: caller.OrganizationID,
				ResourceID: deploymentID, Actor: caller.Actor, After: r.Snapshot(),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
