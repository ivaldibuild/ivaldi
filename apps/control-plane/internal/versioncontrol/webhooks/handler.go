// Package webhooks applies the normalized provider events the gateway
// publishes. Each delivery is recorded and applied in one transaction, so a
// provider's redelivery is recognized only after the change committed.
package webhooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// Option configures a Handler.
type Option func(*options)

type options struct {
	tx          transaction.Transactor
	connections versioncontrol.ConnectionRepository
	sources     versioncontrol.SourceRepository
	deliveries  versioncontrol.WebhookDeliveryRepository
	outbox      outbox.Recorder
	audit       audit.Recorder
	logger      logging.Logger
}

func WithTransactor(tx transaction.Transactor) Option {
	return func(o *options) { o.tx = tx }
}

func WithConnectionRepo(r versioncontrol.ConnectionRepository) Option {
	return func(o *options) { o.connections = r }
}

func WithSourceRepo(r versioncontrol.SourceRepository) Option {
	return func(o *options) { o.sources = r }
}

func WithDeliveryRepo(r versioncontrol.WebhookDeliveryRepository) Option {
	return func(o *options) { o.deliveries = r }
}

func WithOutbox(r outbox.Recorder) Option {
	return func(o *options) { o.outbox = r }
}

func WithAudit(r audit.Recorder) Option {
	return func(o *options) { o.audit = r }
}

func WithLogger(l logging.Logger) Option {
	return func(o *options) { o.logger = l }
}

// Handler applies normalized provider webhook events.
type Handler struct {
	tx          transaction.Transactor
	connections versioncontrol.ConnectionRepository
	sources     versioncontrol.SourceRepository
	deliveries  versioncontrol.WebhookDeliveryRepository
	outbox      outbox.Recorder
	audit       audit.Recorder
	logger      logging.Logger
}

// NewHandler returns a Handler, or an error naming a missing dependency.
func NewHandler(opts ...Option) (*Handler, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Transactor", o.tx != nil},
		{"ConnectionRepo", o.connections != nil},
		{"SourceRepo", o.sources != nil},
		{"DeliveryRepo", o.deliveries != nil},
		{"Outbox", o.outbox != nil},
		{"Audit", o.audit != nil},
		{"Logger", o.logger != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("webhook handler: %s is required", r.name)
		}
	}
	return &Handler{
		tx:          o.tx,
		connections: o.connections,
		sources:     o.sources,
		deliveries:  o.deliveries,
		outbox:      o.outbox,
		audit:       o.audit,
		logger:      o.logger,
	}, nil
}

// HandleRepositoryChanged applies a repository event to the Source that
// connects the repository.
func (h *Handler) HandleRepositoryChanged(ctx context.Context, _ events.Envelope, event vcs.ProviderRepositoryChanged) error {
	e := event.WebhookEvent
	if err := validateCommon(e); err != nil {
		return err
	}
	if e.Repository == nil {
		return permanent("webhook %s: a repository event needs a repository", e.DeliveryID)
	}
	if e.Repository.ExternalID == "" {
		return permanent("webhook %s: a repository event needs an external repository ID", e.DeliveryID)
	}
	switch e.Action {
	case vcs.WebhookRepositoryChanged, vcs.WebhookRepositoryArchived, vcs.WebhookRepositoryUnarchived:
	default:
		return permanent("webhook %s: %q is not a repository action", e.DeliveryID, e.Action)
	}

	return h.withConnection(ctx, e, func(ctx context.Context, connection *versioncontrol.Connection) error {
		repositoryKey := vcs.ProviderKey(e.Provider, e.HostURL, e.Repository.ExternalID)
		source, err := h.sources.GetSourceByRepositoryKeyForUpdate(ctx, connection.OrganizationID(), repositoryKey)
		if errors.Is(err, versioncontrol.ErrNotFound) {
			h.logger.Info("webhook repository is not a connected source",
				logging.F("delivery_id", e.DeliveryID),
				logging.F("route_key", e.RouteKey),
				logging.F("repository_key", repositoryKey))
			return nil
		}
		if err != nil {
			return fmt.Errorf("load source: %w", err)
		}

		before := source.Snapshot()
		repository := vcs.Repository{
			Provider:      e.Provider,
			HostURL:       e.HostURL,
			ExternalID:    e.Repository.ExternalID,
			FullName:      e.Repository.FullName,
			DefaultBranch: e.Repository.DefaultBranch,
			Visibility:    e.Repository.Visibility,
			CloneURL:      e.Repository.CloneURL,
			WebURL:        e.Repository.WebURL,
		}
		applied, err := source.ApplyProviderView(repository, archivedFrom(e), e.OccurredAt)
		if err != nil {
			if errors.Is(err, versioncontrol.ErrArchived) {
				h.logger.Info("webhook repository change does not apply",
					logging.F("delivery_id", e.DeliveryID),
					logging.F("source_id", source.ID()),
					logging.F("error", err.Error()))
				return nil
			}
			if errors.Is(err, versioncontrol.ErrInvalid) {
				// The provider's metadata breaks a Source rule; it will
				// not pass on a retry either.
				return fmt.Errorf("%w: apply provider view: %w", events.ErrPermanent, err)
			}
			return fmt.Errorf("apply provider view: %w", err)
		}
		if !applied {
			h.logger.Info("webhook repository event is older than one already applied",
				logging.F("delivery_id", e.DeliveryID),
				logging.F("source_id", source.ID()),
				logging.F("occurred_at", e.OccurredAt))
			return nil
		}

		// Save even when nothing else changed: the Source moved its provider
		// timestamp, which refuses older events that arrive later.
		if err := h.sources.UpdateSource(ctx, source); err != nil {
			return fmt.Errorf("update source: %w", err)
		}
		changes := source.PullEvents()
		if len(changes) == 0 {
			return nil
		}
		if err := h.recordEvents(ctx, events.Source{
			OrganizationID: source.OrganizationID(),
			AggregateType:  versioncontrol.AggregateSource,
			AggregateID:    source.ID(),
		}, changes); err != nil {
			return err
		}
		return h.recordAudit(ctx, audit.Event{
			Type:           versioncontrol.EventSourceUpdated,
			OrganizationID: source.OrganizationID(),
			ResourceID:     source.ID(),
			Before:         before,
			After:          source.Snapshot(),
		})
	})
}

// HandleInstallationChanged applies an installation event to the Connection
// that owns the installation.
func (h *Handler) HandleInstallationChanged(ctx context.Context, _ events.Envelope, event vcs.ProviderInstallationChanged) error {
	e := event.WebhookEvent
	if err := validateCommon(e); err != nil {
		return err
	}
	if e.InstallationID == "" {
		return permanent("webhook %s: an installation event needs an installation ID", e.DeliveryID)
	}
	var (
		status    versioncontrol.Status
		eventType string
	)
	switch e.Action {
	case vcs.WebhookInstallationSuspended:
		status, eventType = versioncontrol.StatusSuspended, versioncontrol.EventConnectionSuspended
	case vcs.WebhookInstallationUnsuspended:
		status, eventType = versioncontrol.StatusActive, versioncontrol.EventConnectionReactivated
	case vcs.WebhookInstallationDeleted:
		status, eventType = versioncontrol.StatusRevoked, versioncontrol.EventConnectionRevoked
	default:
		return permanent("webhook %s: %q is not an installation action", e.DeliveryID, e.Action)
	}

	return h.withConnection(ctx, e, func(ctx context.Context, connection *versioncontrol.Connection) error {
		if e.InstallationID != connection.ExternalAccountID() {
			h.logger.Warn("webhook installation does not match the route's connection",
				logging.F("delivery_id", e.DeliveryID),
				logging.F("route_key", e.RouteKey),
				logging.F("installation_id", e.InstallationID),
				logging.F("connection_id", connection.ID()))
			return nil
		}

		before := connection.Snapshot()
		applied, err := connection.ApplyProviderStatus(status, e.OccurredAt)
		if err != nil {
			if errors.Is(err, versioncontrol.ErrArchived) || errors.Is(err, versioncontrol.ErrInvalid) {
				h.logger.Info("webhook installation change does not apply",
					logging.F("delivery_id", e.DeliveryID),
					logging.F("connection_id", connection.ID()),
					logging.F("error", err.Error()))
				return nil
			}
			return fmt.Errorf("apply installation status: %w", err)
		}
		if !applied {
			h.logger.Info("webhook installation event is older than one already applied",
				logging.F("delivery_id", e.DeliveryID),
				logging.F("connection_id", connection.ID()),
				logging.F("occurred_at", e.OccurredAt))
			return nil
		}

		// Save even when the status did not change: the Connection moved
		// its provider timestamp, which refuses older events that arrive
		// later.
		if err := h.connections.UpdateConnection(ctx, connection); err != nil {
			return fmt.Errorf("update connection: %w", err)
		}
		changes := connection.PullEvents()
		if len(changes) == 0 {
			return nil
		}
		if err := h.recordEvents(ctx, events.Source{
			OrganizationID: connection.OrganizationID(),
			AggregateType:  versioncontrol.AggregateConnection,
			AggregateID:    connection.ID(),
		}, changes); err != nil {
			return err
		}
		return h.recordAudit(ctx, audit.Event{
			Type:           eventType,
			OrganizationID: connection.OrganizationID(),
			ResourceID:     connection.ID(),
			Before:         before,
			After:          connection.Snapshot(),
		})
	})
}

// withConnection resolves the delivery's route, verifies that the event is
// for that Connection, records the delivery, and calls apply. Errors that a
// retry cannot fix are acknowledged by returning nil.
func (h *Handler) withConnection(ctx context.Context, event vcs.WebhookEvent, apply func(context.Context, *versioncontrol.Connection) error) error {
	return h.tx.WithinTx(ctx, func(ctx context.Context) error {
		connection, err := h.connections.GetConnectionByWebhookRouteKeyForUpdate(ctx, event.RouteKey)
		if errors.Is(err, versioncontrol.ErrNotFound) {
			h.logger.Info("webhook route is unknown",
				logging.F("delivery_id", event.DeliveryID),
				logging.F("route_key", event.RouteKey))
			return nil
		}
		if err != nil {
			return fmt.Errorf("load connection: %w", err)
		}
		if event.Provider != connection.Provider() ||
			vcs.ProviderKey(event.Provider, event.HostURL, "") != vcs.ProviderKey(connection.Provider(), connection.HostURL(), "") {
			h.logger.Warn("webhook provider or host does not match the route",
				logging.F("delivery_id", event.DeliveryID),
				logging.F("route_key", event.RouteKey),
				logging.F("provider", string(event.Provider)),
				logging.F("host_url", event.HostURL))
			return nil
		}

		fresh, err := h.deliveries.RecordWebhookDelivery(ctx, versioncontrol.WebhookDelivery{
			Provider:       event.Provider,
			RouteKey:       event.RouteKey,
			DeliveryID:     event.DeliveryID,
			OrganizationID: connection.OrganizationID(),
		})
		if err != nil {
			return fmt.Errorf("record webhook delivery: %w", err)
		}
		if !fresh {
			h.logger.Debug("webhook delivery was already handled",
				logging.F("delivery_id", event.DeliveryID),
				logging.F("route_key", event.RouteKey))
			return nil
		}
		return apply(ctx, connection)
	})
}

// recordEvents writes events an aggregate raised to the outbox in the
// caller's transaction.
func (h *Handler) recordEvents(ctx context.Context, source events.Source, raised []aggregate.Event) error {
	envelopes, err := events.Wrap(ctx, source, raised...)
	if err != nil {
		return fmt.Errorf("wrap %s events: %w", source.AggregateType, err)
	}
	if err := h.outbox.Record(ctx, envelopes...); err != nil {
		return fmt.Errorf("record %s events: %w", source.AggregateType, err)
	}
	return nil
}

// recordAudit writes the system's audit record for the change. Its time is
// when Ivaldi applied the change: the provider's timestamp can be much older,
// such as installation.updated_at on an uninstall.
func (h *Handler) recordAudit(ctx context.Context, e audit.Event) error {
	e.Actor = identity.Actor{Kind: identity.ActorSystem}
	if err := h.audit.Record(ctx, e); err != nil {
		return fmt.Errorf("record audit %s: %w", e.Type, err)
	}
	return nil
}

// validateCommon rejects an event that cannot be routed or deduplicated.
func validateCommon(event vcs.WebhookEvent) error {
	for _, f := range []struct {
		name  string
		value string
	}{
		{"delivery ID", event.DeliveryID},
		{"route key", event.RouteKey},
		{"host URL", event.HostURL},
	} {
		if f.value == "" {
			return permanent("webhook event has no %s", f.name)
		}
	}
	if !event.Provider.Valid() {
		return permanent("webhook event has invalid provider %q", event.Provider)
	}
	return nil
}

// permanent returns an error that no retry can fix, so the consumer
// dead-letters the event at once.
func permanent(format string, args ...any) error {
	return fmt.Errorf("%w: %s", events.ErrPermanent, fmt.Sprintf(format, args...))
}

// archivedFrom returns the upstream archival state a repository action
// reports. A generic change carries the repository's current state.
func archivedFrom(event vcs.WebhookEvent) *bool {
	switch event.Action {
	case vcs.WebhookRepositoryArchived:
		archived := true
		return &archived
	case vcs.WebhookRepositoryUnarchived:
		archived := false
		return &archived
	case vcs.WebhookRepositoryChanged:
		return &event.Repository.Archived
	default:
		return nil
	}
}
