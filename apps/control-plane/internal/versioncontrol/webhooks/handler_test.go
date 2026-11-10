package webhooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const (
	orgID       = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	workspaceID = "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	projectID   = "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

var occurredAt = time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

type fakeTransactor struct{}

func (fakeTransactor) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeConnections struct {
	connection *versioncontrol.Connection
	err        error
	updated    int
}

func (f *fakeConnections) CreateConnection(context.Context, *versioncontrol.Connection) error {
	return nil
}
func (f *fakeConnections) GetConnection(context.Context, string, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f *fakeConnections) GetConnectionForUpdate(context.Context, string, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f *fakeConnections) GetConnectionByWebhookRouteKey(context.Context, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f *fakeConnections) GetConnectionByWebhookRouteKeyForUpdate(context.Context, string) (*versioncontrol.Connection, error) {
	return f.connection, f.err
}
func (f *fakeConnections) ListConnections(context.Context, string, bool) ([]*versioncontrol.Connection, error) {
	return nil, nil
}
func (f *fakeConnections) UpdateConnection(_ context.Context, c *versioncontrol.Connection) error {
	f.updated++
	f.connection = c
	return nil
}

type fakeSources struct {
	source  *versioncontrol.Source
	err     error
	updated int
}

func (f *fakeSources) CreateSource(context.Context, *versioncontrol.Source) error { return nil }
func (f *fakeSources) GetSource(context.Context, string, string) (*versioncontrol.Source, error) {
	return f.source, f.err
}
func (f *fakeSources) GetSourceForUpdate(context.Context, string, string) (*versioncontrol.Source, error) {
	return f.source, f.err
}
func (f *fakeSources) GetSourceByRepositoryKey(context.Context, string, string) (*versioncontrol.Source, error) {
	return f.source, f.err
}
func (f *fakeSources) GetSourceByRepositoryKeyForUpdate(context.Context, string, string) (*versioncontrol.Source, error) {
	return f.source, f.err
}
func (f *fakeSources) ListSources(context.Context, versioncontrol.ListSourcesQuery) (*pagination.Result[*versioncontrol.Source], error) {
	return nil, nil
}
func (f *fakeSources) ConnectedRepositoryKeys(context.Context, string, []string) ([]string, error) {
	return nil, nil
}
func (f *fakeSources) UpdateSource(_ context.Context, s *versioncontrol.Source) error {
	f.updated++
	f.source = s
	return nil
}

type fakeDeliveries struct {
	fresh    bool
	recorded int
	delivery versioncontrol.WebhookDelivery
	err      error
}

func (f *fakeDeliveries) RecordWebhookDelivery(_ context.Context, d versioncontrol.WebhookDelivery) (bool, error) {
	f.recorded++
	f.delivery = d
	return f.fresh, f.err
}

type fakeOutbox struct {
	events []events.Envelope
}

func (f *fakeOutbox) Record(_ context.Context, envelopes ...events.Envelope) error {
	f.events = append(f.events, envelopes...)
	return nil
}

type fakeAudit struct {
	events []audit.Event
}

func (f *fakeAudit) Record(_ context.Context, e audit.Event) error {
	f.events = append(f.events, e)
	return nil
}

type fixture struct {
	handler     *Handler
	connection  *versioncontrol.Connection
	connections *fakeConnections
	sources     *fakeSources
	deliveries  *fakeDeliveries
	outbox      *fakeOutbox
	audit       *fakeAudit
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	connection, err := versioncontrol.NewInstalledConnection(orgID, nil, vcs.Installation{
		Provider: vcs.ProviderGitHub, HostURL: "https://github.com",
		InstallationID: "42", AccountName: "acme",
	})
	require.NoError(t, err)
	connection.PullEvents()

	source, err := versioncontrol.NewSource(versioncontrol.NewSourceProps{
		OrganizationID: orgID,
		ConnectionID:   connection.ID(),
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		RepositoryKey:  connection.RepositoryKey("99"),
		Repository: vcs.Repository{
			ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
		},
	}, nil)
	require.NoError(t, err)
	source.PullEvents()

	f := &fixture{
		connection:  connection,
		connections: &fakeConnections{connection: connection},
		sources:     &fakeSources{source: source},
		deliveries:  &fakeDeliveries{fresh: true},
		outbox:      &fakeOutbox{},
		audit:       &fakeAudit{},
	}
	handler, err := NewHandler(
		WithTransactor(fakeTransactor{}),
		WithConnectionRepo(f.connections),
		WithSourceRepo(f.sources),
		WithDeliveryRepo(f.deliveries),
		WithOutbox(f.outbox),
		WithAudit(f.audit),
		WithLogger(logging.Nop()),
	)
	require.NoError(t, err)
	f.handler = handler
	return f
}

func (f *fixture) repositoryEvent(action vcs.WebhookAction, repository vcs.WebhookRepository) vcs.ProviderRepositoryChanged {
	return vcs.ProviderRepositoryChanged{WebhookEvent: vcs.WebhookEvent{
		DeliveryID: "delivery-1",
		RouteKey:   "github:github.com:42",
		Provider:   vcs.ProviderGitHub,
		HostURL:    "https://github.com",
		Action:     action,
		Repository: &repository,
		OccurredAt: occurredAt,
	}}
}

func (f *fixture) installationEvent(action vcs.WebhookAction, installationID string) vcs.ProviderInstallationChanged {
	return vcs.ProviderInstallationChanged{WebhookEvent: vcs.WebhookEvent{
		DeliveryID:     "delivery-1",
		RouteKey:       "github:github.com:42",
		Provider:       vcs.ProviderGitHub,
		HostURL:        "https://github.com",
		Action:         action,
		InstallationID: installationID,
		OccurredAt:     occurredAt,
	}}
}

func handleRepository(t *testing.T, f *fixture, event vcs.ProviderRepositoryChanged) error {
	t.Helper()
	return f.handler.HandleRepositoryChanged(context.Background(), events.Envelope{}, event)
}

func handleInstallation(t *testing.T, f *fixture, event vcs.ProviderInstallationChanged) error {
	t.Helper()
	return f.handler.HandleInstallationChanged(context.Background(), events.Envelope{}, event)
}

func TestRepositoryRenameUpdatesTheSource(t *testing.T) {
	f := newFixture(t)
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/checkout", DefaultBranch: "trunk", Visibility: "public",
		CloneURL: "https://github.com/acme/checkout.git", WebURL: "https://github.com/acme/checkout",
	})

	require.NoError(t, handleRepository(t, f, event))
	assert.Equal(t, "acme/checkout", f.sources.source.FullName())
	assert.Equal(t, "trunk", f.sources.source.DefaultBranch())
	assert.Equal(t, versioncontrol.VisibilityPublic, f.sources.source.Visibility())
	assert.Equal(t, 1, f.sources.updated)
	assert.Equal(t, 1, f.deliveries.recorded)
	require.Len(t, f.outbox.events, 1)
	assert.Equal(t, "versioncontrol.v1.source.updated", f.outbox.events[0].Type)
	require.Len(t, f.audit.events, 1)
	assert.Equal(t, versioncontrol.EventSourceUpdated, f.audit.events[0].Type)
	assert.Equal(t, identity.ActorSystem, f.audit.events[0].Actor.Kind)
	assert.True(t, f.audit.events[0].OccurredAt.IsZero(),
		"the recorder stamps Ivaldi's time, not the provider's")
}

func TestRepositoryArchivedSetsTheUpstreamTimestamp(t *testing.T) {
	f := newFixture(t)
	event := f.repositoryEvent(vcs.WebhookRepositoryArchived, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private", Archived: true,
	})

	require.NoError(t, handleRepository(t, f, event))
	require.NotNil(t, f.sources.source.UpstreamArchivedAt())
	assert.Equal(t, occurredAt, *f.sources.source.UpstreamArchivedAt())
	assert.Equal(t, 1, f.sources.updated)
}

func TestRepositoryUnarchivedClearsTheUpstreamTimestamp(t *testing.T) {
	f := newFixture(t)
	archive := f.repositoryEvent(vcs.WebhookRepositoryArchived, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private", Archived: true,
	})
	require.NoError(t, handleRepository(t, f, archive))

	unarchive := f.repositoryEvent(vcs.WebhookRepositoryUnarchived, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})
	unarchive.DeliveryID = "delivery-2"
	unarchive.OccurredAt = occurredAt.Add(time.Hour)
	require.NoError(t, handleRepository(t, f, unarchive))
	assert.Nil(t, f.sources.source.UpstreamArchivedAt())
}

func TestRepositoryChangedCarriesTheArchiveState(t *testing.T) {
	f := newFixture(t)
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private", Archived: true,
	})
	require.NoError(t, handleRepository(t, f, event))
	require.NotNil(t, f.sources.source.UpstreamArchivedAt())

	event.DeliveryID = "delivery-2"
	event.Repository.Archived = false
	event.OccurredAt = occurredAt.Add(time.Hour)
	require.NoError(t, handleRepository(t, f, event))
	assert.Nil(t, f.sources.source.UpstreamArchivedAt())
}

func TestUnknownRepositoryRecordsTheDeliveryAndDropsTheEvent(t *testing.T) {
	f := newFixture(t)
	f.sources.err = versioncontrol.ErrNotFound
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})

	require.NoError(t, handleRepository(t, f, event))
	assert.Equal(t, 1, f.deliveries.recorded)
	assert.Zero(t, f.sources.updated)
	assert.Empty(t, f.outbox.events)
	assert.Empty(t, f.audit.events)
}

func TestDuplicateDeliveryAppliesNothing(t *testing.T) {
	f := newFixture(t)
	f.deliveries.fresh = false
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/checkout", DefaultBranch: "trunk", Visibility: "public",
	})

	require.NoError(t, handleRepository(t, f, event))
	assert.Equal(t, 1, f.deliveries.recorded)
	assert.Zero(t, f.sources.updated)
	assert.Empty(t, f.outbox.events)
	assert.Empty(t, f.audit.events)
}

func TestProviderOrHostMismatchIsDropped(t *testing.T) {
	f := newFixture(t)
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})
	event.Provider = vcs.ProviderGitLab

	require.NoError(t, handleRepository(t, f, event))
	assert.Zero(t, f.deliveries.recorded, "a mismatched delivery is not recorded")
	assert.Zero(t, f.sources.updated)
}

func TestUnknownRouteReturnsNil(t *testing.T) {
	f := newFixture(t)
	f.connections.err = versioncontrol.ErrNotFound
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})

	require.NoError(t, handleRepository(t, f, event))
	assert.Zero(t, f.deliveries.recorded)
}

func TestMissingRepositoryOrInstallationIsAPoisonEvent(t *testing.T) {
	f := newFixture(t)
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})
	event.Repository = nil
	assert.ErrorIs(t, handleRepository(t, f, event), events.ErrPermanent)

	installation := f.installationEvent(vcs.WebhookInstallationSuspended, "")
	assert.ErrorIs(t, handleInstallation(t, f, installation), events.ErrPermanent)
}

func TestInvalidRepositoryMetadataIsAPoisonEvent(t *testing.T) {
	f := newFixture(t)
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "", DefaultBranch: "main", Visibility: "private",
	})
	assert.ErrorIs(t, handleRepository(t, f, event), events.ErrPermanent)
}

func TestInstallationTransitions(t *testing.T) {
	tests := []struct {
		name      string
		action    vcs.WebhookAction
		before    versioncontrol.Status
		want      versioncontrol.Status
		eventType string
		auditType string
	}{
		{
			name: "suspend", action: vcs.WebhookInstallationSuspended,
			before: versioncontrol.StatusActive, want: versioncontrol.StatusSuspended,
			eventType: "versioncontrol.v1.connection.suspended", auditType: versioncontrol.EventConnectionSuspended,
		},
		{
			name: "unsuspend", action: vcs.WebhookInstallationUnsuspended,
			before: versioncontrol.StatusSuspended, want: versioncontrol.StatusActive,
			eventType: "versioncontrol.v1.connection.reactivated", auditType: versioncontrol.EventConnectionReactivated,
		},
		{
			name: "delete", action: vcs.WebhookInstallationDeleted,
			before: versioncontrol.StatusActive, want: versioncontrol.StatusRevoked,
			eventType: "versioncontrol.v1.connection.revoked", auditType: versioncontrol.EventConnectionRevoked,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.before == versioncontrol.StatusSuspended {
				require.NoError(t, f.connection.Suspend())
				f.connection.PullEvents()
			}
			require.NoError(t, handleInstallation(t, f, f.installationEvent(tt.action, "42")))
			assert.Equal(t, tt.want, f.connection.Status())
			assert.Equal(t, 1, f.connections.updated)
			require.Len(t, f.outbox.events, 1)
			assert.Equal(t, tt.eventType, f.outbox.events[0].Type)
			require.Len(t, f.audit.events, 1)
			assert.Equal(t, tt.auditType, f.audit.events[0].Type)
			assert.Equal(t, identity.ActorSystem, f.audit.events[0].Actor.Kind)
		})
	}
}

func TestInstallationMismatchIsDropped(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, handleInstallation(t, f, f.installationEvent(vcs.WebhookInstallationSuspended, "43")))
	assert.Equal(t, 1, f.deliveries.recorded)
	assert.Zero(t, f.connections.updated)
	assert.Equal(t, versioncontrol.StatusActive, f.connection.Status())
}

func TestInvalidInstallationTransitionIsDropped(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.connection.Revoke())
	f.connection.PullEvents()

	require.NoError(t, handleInstallation(t, f, f.installationEvent(vcs.WebhookInstallationUnsuspended, "42")))
	assert.Zero(t, f.connections.updated)
	assert.Equal(t, versioncontrol.StatusRevoked, f.connection.Status())
	assert.Empty(t, f.outbox.events)
	assert.Empty(t, f.audit.events)
}

func TestNoOpInstallationTransitionKeepsOnlyItsTime(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, handleInstallation(t, f, f.installationEvent(vcs.WebhookInstallationUnsuspended, "42")))
	assert.Equal(t, 1, f.connections.updated, "the Connection keeps the event's time to refuse older ones")
	assert.Equal(t, occurredAt, *f.connections.connection.Snapshot().ProviderUpdatedAt)
	assert.Empty(t, f.outbox.events)
	assert.Empty(t, f.audit.events)
}

func TestOlderInstallationEventIsDropped(t *testing.T) {
	// The unsuspend was handled before the older suspension it followed.
	f := newFixture(t)
	require.NoError(t, handleInstallation(t, f, f.installationEvent(vcs.WebhookInstallationUnsuspended, "42")))
	f.connections.updated = 0

	suspend := f.installationEvent(vcs.WebhookInstallationSuspended, "42")
	suspend.DeliveryID = "delivery-2"
	suspend.OccurredAt = occurredAt.Add(-time.Hour)
	require.NoError(t, handleInstallation(t, f, suspend))
	assert.Equal(t, versioncontrol.StatusActive, f.connections.connection.Status())
	assert.Zero(t, f.connections.updated)
	assert.Empty(t, f.outbox.events)
	assert.Empty(t, f.audit.events)
}

func TestOlderRepositoryEventIsDropped(t *testing.T) {
	// The unarchive was handled before the older archive it followed.
	f := newFixture(t)
	unarchive := f.repositoryEvent(vcs.WebhookRepositoryUnarchived, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})
	require.NoError(t, handleRepository(t, f, unarchive))
	f.sources.updated = 0

	archive := f.repositoryEvent(vcs.WebhookRepositoryArchived, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private", Archived: true,
	})
	archive.DeliveryID = "delivery-2"
	archive.OccurredAt = occurredAt.Add(-time.Hour)
	require.NoError(t, handleRepository(t, f, archive))
	assert.Nil(t, f.sources.source.UpstreamArchivedAt())
	assert.Zero(t, f.sources.updated)
	assert.Empty(t, f.outbox.events)
	assert.Empty(t, f.audit.events)
}

func TestTransientErrorsAreReturned(t *testing.T) {
	f := newFixture(t)
	f.deliveries.err = errors.New("database unavailable")
	event := f.repositoryEvent(vcs.WebhookRepositoryChanged, vcs.WebhookRepository{
		ExternalID: "99", FullName: "acme/api", DefaultBranch: "main", Visibility: "private",
	})
	err := handleRepository(t, f, event)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, events.ErrPermanent, "a transient error is retried")
}
