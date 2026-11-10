package webhooks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	auditmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/migrations"
	auditpg "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	outboxmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/migrations"
	outboxpg "github.com/ivaldibuild/ivaldi-control-plane/internal/outbox/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	versioncontrolmigrations "github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/migrations"
	versioncontrolpg "github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

type pgFixture struct {
	db          *gorm.DB
	handler     *Handler
	connections versioncontrol.ConnectionRepository
	sources     versioncontrol.SourceRepository
	deliveries  versioncontrol.WebhookDeliveryRepository
	outbox      outbox.Store
	connection  *versioncontrol.Connection
	source      *versioncontrol.Source
}

func newPGFixture(t *testing.T, wrap func(versioncontrol.SourceRepository) versioncontrol.SourceRepository) *pgFixture {
	t.Helper()
	db := databasetest.New(t,
		versioncontrolmigrations.FS,
		outboxmigrations.FS,
		auditmigrations.FS,
	)
	ctx := context.Background()
	connections := versioncontrolpg.NewConnectionRepository(db)
	sources := versioncontrolpg.NewSourceRepository(db)
	deliveries := versioncontrolpg.NewWebhookDeliveryRepository(db)
	store := outboxpg.NewRepository(db)

	sourceRepo := versioncontrol.SourceRepository(sources)
	if wrap != nil {
		sourceRepo = wrap(sourceRepo)
	}

	connection, err := versioncontrol.NewInstalledConnection(orgID, nil, vcs.Installation{
		Provider: vcs.ProviderGitHub, HostURL: "https://github.com",
		InstallationID: "42", AccountName: "acme",
	})
	require.NoError(t, err)
	connection.PullEvents()
	require.NoError(t, connections.CreateConnection(ctx, connection))

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
	require.NoError(t, sources.CreateSource(ctx, source))

	handler, err := NewHandler(
		WithTransactor(database.NewTransactor(db)),
		WithConnectionRepo(connections),
		WithSourceRepo(sourceRepo),
		WithDeliveryRepo(deliveries),
		WithOutbox(store),
		WithAudit(auditpg.NewRecorder(db)),
		WithLogger(logging.Nop()),
	)
	require.NoError(t, err)
	return &pgFixture{
		db: db, handler: handler,
		connections: connections, sources: sources, deliveries: deliveries, outbox: store,
		connection: connection, source: source,
	}
}

func (f *pgFixture) repositoryEvent(t *testing.T) vcs.ProviderRepositoryChanged {
	t.Helper()
	return vcs.ProviderRepositoryChanged{WebhookEvent: vcs.WebhookEvent{
		DeliveryID: "delivery-1",
		RouteKey:   "github:github.com:42",
		Provider:   vcs.ProviderGitHub,
		HostURL:    "https://github.com",
		Action:     vcs.WebhookRepositoryChanged,
		Repository: &vcs.WebhookRepository{
			ExternalID: "99", FullName: "acme/checkout", DefaultBranch: "trunk", Visibility: "public",
			CloneURL: "https://github.com/acme/checkout.git", WebURL: "https://github.com/acme/checkout",
		},
		OccurredAt: occurredAt,
	}}
}

func (f *pgFixture) outboxEvents(t *testing.T) []outbox.Entry {
	t.Helper()
	entries, err := f.outbox.Claim(context.Background(), clock.Now(), 10, time.Minute)
	require.NoError(t, err)
	return entries
}

func TestWebhookChangeCommitsWithItsDeliveryAndEvents(t *testing.T) {
	f := newPGFixture(t, nil)
	ctx := context.Background()

	require.NoError(t, f.handler.HandleRepositoryChanged(ctx, events.Envelope{}, f.repositoryEvent(t)))

	loaded, err := f.sources.GetSourceByRepositoryKey(ctx, orgID, f.source.RepositoryKey())
	require.NoError(t, err)
	assert.Equal(t, "acme/checkout", loaded.FullName())
	assert.Equal(t, "trunk", loaded.DefaultBranch())

	entries := f.outboxEvents(t)
	require.Len(t, entries, 1)
	assert.Equal(t, "versioncontrol.v1.source.updated", entries[0].Envelope.Type)

	fresh, err := f.deliveries.RecordWebhookDelivery(ctx, versioncontrol.WebhookDelivery{
		Provider: vcs.ProviderGitHub, RouteKey: "github:github.com:42",
		DeliveryID: "delivery-1", OrganizationID: orgID,
	})
	require.NoError(t, err)
	assert.False(t, fresh, "the delivery committed with the change")

	var audits int64
	require.NoError(t, f.db.Table("audit.records").Count(&audits).Error)
	assert.EqualValues(t, 1, audits)
}

type failingSourceRepo struct {
	versioncontrol.SourceRepository
}

func (failingSourceRepo) UpdateSource(context.Context, *versioncontrol.Source) error {
	return errors.New("update failed")
}

func TestARolledBackWebhookLeavesNoDeliveryRow(t *testing.T) {
	f := newPGFixture(t, func(repo versioncontrol.SourceRepository) versioncontrol.SourceRepository {
		return failingSourceRepo{SourceRepository: repo}
	})
	ctx := context.Background()

	err := f.handler.HandleRepositoryChanged(ctx, events.Envelope{}, f.repositoryEvent(t))
	require.Error(t, err)

	var deliveries int64
	require.NoError(t, f.db.Table("version_control.webhook_deliveries").
		Where("delivery_id = ?", "delivery-1").Count(&deliveries).Error)
	assert.Zero(t, deliveries, "the delivery row rolled back with the change")
	assert.Empty(t, f.outboxEvents(t))
}

func TestConcurrentDuplicateDeliveriesApplyOnce(t *testing.T) {
	f := newPGFixture(t, nil)
	event := f.repositoryEvent(t)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- f.handler.HandleRepositoryChanged(context.Background(), events.Envelope{}, event)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.Len(t, f.outboxEvents(t), 1, "the duplicate applied nothing")
}
