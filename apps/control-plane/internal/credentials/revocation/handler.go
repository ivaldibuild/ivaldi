// Package revocation revokes brokered Secret Store grants when the
// Deployment they were made for leaves applying (ADR 0017): its secrets are
// read by then, so nothing minted for it is needed. It reads the outbox
// stream with its own consumer group, as notifications does.
package revocation

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// Handler revokes a Deployment's brokered grants. It can run again for the
// same event: a revoked grant is not revoked twice.
type Handler struct {
	grants credentials.SecretStoreGrantRepository
	stores credentials.SecretStoreRepository
	broker credentials.Broker
	keeper credentials.CredentialKeeper
	w      change.Writer
	logger logging.Logger
}

// NewHandler returns the Handler, or an error naming a missing dependency.
func NewHandler(opts ...Option) (*Handler, error) {
	h := &Handler{}
	for _, opt := range opts {
		opt(h)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"SecretStoreGrantRepository", h.grants != nil},
		{"SecretStoreRepository", h.stores != nil},
		{"Broker", h.broker != nil},
		{"CredentialKeeper", h.keeper != nil},
		{"Transactor", h.w.Tx != nil},
		{"Audit Recorder", h.w.Audit != nil},
		{"Logger", h.logger != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("grant revocation: %s is required", r.name)
		}
	}
	return h, nil
}

// Register subscribes the handler to Deployments' status changes.
func (h *Handler) Register(r *events.Router) {
	events.On(r, h.statusChanged)
}

func (h *Handler) statusChanged(ctx context.Context, _ events.Envelope, ev delivery.DeploymentStatusChanged) error {
	d := ev.Deployment
	if d.Status == release.DeploymentApplying {
		return nil
	}
	grants, err := h.grants.ListDeploymentGrants(ctx, d.OrganizationID, d.ID)
	if err != nil {
		return fmt.Errorf("list grants of deployment %s: %w", d.ID, err)
	}
	var failed error
	for _, g := range grants {
		if g.Mode() != credentials.AccessBrokered || g.Revoked() || !g.ExpiresAt().After(clock.Now()) {
			continue
		}
		if err := h.revoke(ctx, g); err != nil {
			failed = errors.Join(failed, err)
		}
	}
	// A store that could not be reached is tried again with the event.
	return failed
}

func (h *Handler) revoke(ctx context.Context, g *credentials.SecretStoreGrant) error {
	store, err := h.stores.GetSecretStore(ctx, g.OrganizationID(), g.StoreID())
	if err != nil {
		return fmt.Errorf("get secret store %s: %w", g.StoreID(), err)
	}
	var minting credentials.MintingCredential
	if store.NeedsCredential() {
		minting, err = h.keeper.Get(ctx, store)
		if errors.Is(err, credentials.ErrNoCredential) {
			// Removed since, or the store moved: what it minted expires.
			h.logger.Info("grant left to expire: the store has no minting credential",
				logging.F("grant_id", g.ID()), logging.F("secret_store_id", store.ID()))
			return nil
		}
		if err != nil {
			return err
		}
	}
	revoked, err := h.broker.Revoke(ctx, credentials.RevokeRequest{Store: store, Credential: minting, ProviderID: g.TokenID()})
	switch {
	case errors.Is(err, credentials.ErrStoreUnavailable):
		return err
	case err != nil:
		// Trying again would be refused again; it expires.
		h.logger.Warn("grant left to expire: the store refused to revoke it",
			logging.F("grant_id", g.ID()), logging.F("secret_store_id", store.ID()), logging.F("error", err.Error()))
		return nil
	case !revoked:
		return nil
	}
	g.Revoke()
	ctx = authorization.WithDecision(ctx, "", authorization.Decision{
		ID: authorization.NewDecisionID(), Outcome: authorization.OutcomeAllow,
		Reason: "the deployment the grant was made for left applying",
	})
	return h.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.grants.UpdateSecretStoreGrant(ctx, g); err != nil {
			return fmt.Errorf("record revocation of grant %s: %w", g.ID(), err)
		}
		return h.w.AuditRecord(ctx, audit.Event{
			Type: credentials.EventSecretStoreAccessRevoked, OrganizationID: g.OrganizationID(), ResourceID: g.DeploymentID(),
			Actor: identity.Actor{Kind: identity.ActorSystem}, After: g.Snapshot(),
		})
	})
}
