// Package grants grants a Cluster's agent access to the Secret Stores a
// Deployment reads (ADR 0017). The agent names only the Deployment: the
// control plane works out which stores its secrets are in, checks them
// again, and grants each store in a mode that needs one. Every grant is
// recorded and audited; what is granted never is.
package grants

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/federation"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/binding"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// NewService returns the grants service, or an error naming a missing
// dependency. Without an issuer, a federated store is never granted, and
// without a broker and a keeper, a brokered one is not.
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
		{"SecretStoreRepository", s.stores != nil},
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
	stores       credentials.SecretStoreRepository
	issuer       federation.Issuer
	broker       credentials.Broker
	keeper       credentials.CredentialKeeper
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
	federated, brokered := storesToGrant(a.Apply.Secrets)
	if len(federated) == 0 && len(brokered.ids) == 0 {
		return []Grant{}, nil
	}

	out := make([]Grant, 0, len(federated)+len(brokered.ids))
	records := make([]*credentials.SecretStoreGrant, 0, cap(out))
	if len(federated) > 0 {
		grants, recs, err := s.federate(ctx, caller, a, federated)
		if err != nil {
			return nil, err
		}
		out, records = append(out, grants...), append(records, recs...)
	}
	var minted []revocation
	if len(brokered.ids) > 0 {
		grants, recs, done, err := s.mint(ctx, caller.OrganizationID, deploymentID, brokered)
		minted = done
		if err != nil {
			s.revoke(ctx, minted)
			return nil, err
		}
		out, records = append(out, grants...), append(records, recs...)
	}

	if err := s.record(ctx, caller, deploymentID, records); err != nil {
		// Nothing granted is answered, so nothing minted may outlive it.
		s.revoke(ctx, minted)
		return nil, err
	}
	return out, nil
}

// brokeredStores are the brokered stores a Deployment reads, in the order
// its secrets name them, with the paths it reads in each.
type brokeredStores struct {
	ids   []string
	paths map[string][]string
}

// storesToGrant returns the stores among the secrets' that need a grant: the
// federated ones, and the brokered ones with the paths read in each.
func storesToGrant(secrets []delivery.AssignmentSecret) ([]string, brokeredStores) {
	var federated []string
	brokered := brokeredStores{paths: map[string][]string{}}
	seen := map[string]bool{}
	for _, secret := range secrets {
		id := secret.Store.ID
		switch secret.Store.Access.Mode {
		case delivery.StoreAccessFederated:
			if !seen[id] {
				federated = append(federated, id)
			}
		case delivery.StoreAccessBrokered:
			if !seen[id] {
				brokered.ids = append(brokered.ids, id)
			}
			// The assignment escapes the path it has filled in; what the
			// store holds is the path unescaped.
			path, err := binding.Substitute(secret.Ref.Path, func(string) (string, error) { return "", nil })
			if err == nil {
				brokered.paths[id] = append(brokered.paths[id], path)
			}
		}
		seen[id] = true
	}
	return federated, brokered
}

// federate signs a JWT for each federated store.
func (s *service) federate(ctx context.Context, caller identity.Caller, a *delivery.DeploymentAssignment, stores []string) ([]Grant, []*credentials.SecretStoreGrant, error) {
	if s.issuer == nil {
		return nil, nil, delivery.NewErrInvalidInput("the deployment's secrets are in a federated secret store: " + credentials.ErrNoFederation.Error())
	}
	app, err := s.applications.GetApplication(ctx, caller.OrganizationID, a.Deployment.ApplicationID)
	if err != nil {
		return nil, nil, fmt.Errorf("get application %s: %w", a.Deployment.ApplicationID, err)
	}
	var out []Grant
	var records []*credentials.SecretStoreGrant
	for _, storeID := range stores {
		tok, err := s.issuer.Issue(federation.Grant{
			OrganizationID: caller.OrganizationID, WorkspaceID: a.Environment.WorkspaceID, ProjectID: app.ProjectID(),
			ApplicationID: a.Deployment.ApplicationID, EnvironmentID: a.Deployment.EnvironmentID,
			EnvironmentTier: string(a.Environment.Tier), TargetID: a.Deployment.TargetID, ClusterID: a.Target.ClusterID,
			DeploymentID: a.Deployment.ID, ReleaseID: a.Deployment.ReleaseID, StoreID: storeID,
		})
		if err != nil {
			return nil, nil, err
		}
		record, err := credentials.NewSecretStoreGrant(credentials.NewSecretStoreGrantProps{
			OrganizationID: caller.OrganizationID, StoreID: storeID, DeploymentID: a.Deployment.ID,
			Mode: credentials.AccessFederated, TokenID: tok.ID, ExpiresAt: tok.ExpiresAt,
		}, nil)
		if err != nil {
			return nil, nil, err
		}
		records = append(records, record)
		out = append(out, Grant{StoreID: storeID, Mode: credentials.AccessFederated, ExpiresAt: tok.ExpiresAt, JWT: tok.Value})
	}
	return out, records, nil
}

// revocation is something minted in this call, to revoke if the call fails.
type revocation struct {
	store      *credentials.SecretStore
	credential credentials.MintingCredential
	providerID string
}

// mint mints a credential for each brokered store, and returns what it
// minted even when it fails part way, for the caller to revoke.
func (s *service) mint(ctx context.Context, organizationID, deploymentID string, stores brokeredStores) ([]Grant, []*credentials.SecretStoreGrant, []revocation, error) {
	if s.broker == nil || s.keeper == nil {
		return nil, nil, nil, delivery.NewErrInvalidInput("the deployment's secrets are in a brokered secret store: " + credentials.ErrNoBrokering.Error())
	}
	loaded, err := s.stores.GetSecretStores(ctx, organizationID, stores.ids)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get secret stores: %w", err)
	}
	byID := make(map[string]*credentials.SecretStore, len(loaded))
	for _, store := range loaded {
		byID[store.ID()] = store
	}
	var out []Grant
	var records []*credentials.SecretStoreGrant
	var done []revocation
	for _, storeID := range stores.ids {
		store, ok := byID[storeID]
		if !ok {
			return out, records, done, fmt.Errorf("%w: secret store %s", credentials.ErrNotFound, storeID)
		}
		var minting credentials.MintingCredential
		if store.NeedsCredential() {
			if minting, err = s.keeper.Get(ctx, store); err != nil {
				return out, records, done, err
			}
		}
		m, err := s.broker.Mint(ctx, credentials.MintRequest{
			Store: store, Credential: minting, DeploymentID: deploymentID, Paths: stores.paths[storeID],
		})
		if err != nil {
			return out, records, done, err
		}
		done = append(done, revocation{store: store, credential: minting, providerID: m.ProviderID})
		record, err := credentials.NewSecretStoreGrant(credentials.NewSecretStoreGrantProps{
			OrganizationID: organizationID, StoreID: storeID, DeploymentID: deploymentID,
			Mode: credentials.AccessBrokered, TokenID: m.ProviderID, ExpiresAt: m.ExpiresAt,
		}, nil)
		if err != nil {
			return out, records, done, err
		}
		records = append(records, record)
		minted := m.Credential
		out = append(out, Grant{StoreID: storeID, Mode: credentials.AccessBrokered, ExpiresAt: m.ExpiresAt, Credential: &minted})
	}
	return out, records, done, nil
}

// revoke revokes what a failed call minted, where the store can. A failure
// is left to the credential's expiry: the agent was given nothing.
func (s *service) revoke(ctx context.Context, minted []revocation) {
	for _, r := range minted {
		_, _ = s.broker.Revoke(context.WithoutCancel(ctx), credentials.RevokeRequest{
			Store: r.store, Credential: r.credential, ProviderID: r.providerID,
		})
	}
}

// record saves the grants and an audit record for each, never what was
// granted.
func (s *service) record(ctx context.Context, caller identity.Caller, deploymentID string, records []*credentials.SecretStoreGrant) error {
	// The agent is authenticated by its workload credential, not a grant
	// of IAM's, but each record still carries the decision that allowed it.
	decision := authorization.Decision{
		ID: authorization.NewDecisionID(), Outcome: authorization.OutcomeAllow,
		Reason: "the cluster's agent asked for the secret stores its applying deployment reads",
	}
	ctx = authorization.WithDecision(ctx, "", decision)
	return s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
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
}
