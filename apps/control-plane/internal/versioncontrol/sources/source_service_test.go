package sources

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const (
	orgID   = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	agentID = "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

var actor = identity.Actor{PrincipalID: agentID, Kind: identity.ActorUser}

type allow struct{}

func (allow) Authorize(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{ID: "dcn_test", Outcome: authorization.OutcomeAllow}, nil
}

type recorder struct {
	resources []iam.Resource
	events    int
	audits    []audit.Event
}

func (r *recorder) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (r *recorder) Record(_ context.Context, envelopes ...events.Envelope) error {
	r.events += len(envelopes)
	return nil
}
func (r *recorder) RecordAudit(_ context.Context, e audit.Event) error {
	r.audits = append(r.audits, e)
	return nil
}
func (r *recorder) RegisterResource(_ context.Context, resource iam.Resource) error {
	r.resources = append(r.resources, resource)
	return nil
}
func (r *recorder) ArchiveResource(context.Context, string, string) error { return nil }
func (r *recorder) RestoreResource(context.Context, string, string) error { return nil }

type sourceStore struct {
	created *versioncontrol.Source
}

func (s *sourceStore) CreateSource(_ context.Context, source *versioncontrol.Source) error {
	s.created = source
	return nil
}
func (s *sourceStore) GetSource(context.Context, string, string) (*versioncontrol.Source, error) {
	return s.created, nil
}
func (s *sourceStore) GetSourceForUpdate(context.Context, string, string) (*versioncontrol.Source, error) {
	return s.created, nil
}
func (s *sourceStore) GetSourceByRepositoryKey(context.Context, string, string) (*versioncontrol.Source, error) {
	return s.created, nil
}
func (s *sourceStore) GetSourceByRepositoryKeyForUpdate(context.Context, string, string) (*versioncontrol.Source, error) {
	return s.created, nil
}
func (s *sourceStore) ListSources(context.Context, versioncontrol.ListSourcesQuery) (*pagination.Result[*versioncontrol.Source], error) {
	return nil, nil
}
func (s *sourceStore) ConnectedRepositoryKeys(context.Context, string, []string) ([]string, error) {
	return nil, nil
}
func (s *sourceStore) UpdateSource(context.Context, *versioncontrol.Source) error { return nil }

type connectionStore struct {
	connection *versioncontrol.Connection
}

func (s connectionStore) CreateConnection(context.Context, *versioncontrol.Connection) error {
	return nil
}
func (s connectionStore) GetConnection(context.Context, string, string) (*versioncontrol.Connection, error) {
	return s.connection, nil
}
func (s connectionStore) GetConnectionForUpdate(context.Context, string, string) (*versioncontrol.Connection, error) {
	return s.connection, nil
}
func (s connectionStore) GetConnectionByWebhookRouteKey(context.Context, string) (*versioncontrol.Connection, error) {
	return s.connection, nil
}
func (s connectionStore) GetConnectionByWebhookRouteKeyForUpdate(context.Context, string) (*versioncontrol.Connection, error) {
	return s.connection, nil
}
func (s connectionStore) ListConnections(context.Context, string, bool) ([]*versioncontrol.Connection, error) {
	return nil, nil
}
func (s connectionStore) UpdateConnection(context.Context, *versioncontrol.Connection) error {
	return nil
}

type projectLookup struct {
	project *workspace.Project
}

func (p projectLookup) LookupProject(context.Context, string, string) (*workspace.Project, error) {
	return p.project, nil
}

type provider struct {
	repository vcs.Repository
}

func (p provider) Repository(context.Context, vcs.Endpoint, string) (vcs.Repository, error) {
	return p.repository, nil
}
func (p provider) Repositories(context.Context, vcs.Endpoint) ([]vcs.Repository, error) {
	return []vcs.Repository{p.repository}, nil
}
func (p provider) Compare(context.Context, vcs.Endpoint, vcs.Repository, string, string) (vcs.Comparison, error) {
	return vcs.Comparison{}, nil
}

type fixture struct {
	service    versioncontrol.SourceService
	store      *sourceStore
	rec        *recorder
	project    *workspace.Project
	connection *versioncontrol.Connection
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	project, err := workspace.NewProject(workspace.NewProjectProps{
		OrganizationID: orgID,
		WorkspaceID:    "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		OwnerTeamID:    "tem_01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Slug:           "payments",
		DisplayName:    "Payments",
	}, nil)
	require.NoError(t, err)

	connection, err := versioncontrol.NewConnection(versioncontrol.NewConnectionProps{
		OrganizationID:    orgID,
		Provider:          vcs.ProviderGitHub,
		HostURL:           "https://github.com",
		AuthKind:          versioncontrol.AuthKindAppInstallation,
		ExternalAccountID: "42",
		LookupFrom:        vcs.LookupFromControlPlane,
		Status:            versioncontrol.StatusActive,
	}, nil)
	require.NoError(t, err)

	rec := &recorder{}
	store := &sourceStore{}
	service, err := NewSourceService(
		WithSourceRepo(store),
		WithConnectionRepo(connectionStore{connection: connection}),
		WithProvider(provider{repository: vcs.Repository{
			ExternalID: "99", FullName: "acme/payments", DefaultBranch: "main", Visibility: "private",
		}}),
		WithWorkspaces(projectLookup{project: project}),
		WithResourceRegistrar(rec),
		WithAuthorizer(allow{}),
		WithTransactor(rec),
		WithOutboxRecorder(rec),
		WithAuditRecorder(auditRecorder{rec}),
	)
	require.NoError(t, err)
	return fixture{service: service, store: store, rec: rec, project: project, connection: connection}
}

func (f fixture) createSource(t *testing.T) *versioncontrol.Source {
	t.Helper()
	source, err := f.service.CreateSource(context.Background(), actor, versioncontrol.CreateSourceInput{
		OrganizationID:       orgID,
		ProjectID:            f.project.ID(),
		ConnectionID:         f.connection.ID(),
		ExternalRepositoryID: "99",
	})
	require.NoError(t, err)
	return source
}

func TestCreateSourceRecordsTheRepositoryAndItsResource(t *testing.T) {
	f := newFixture(t)
	source := f.createSource(t)

	assert.Equal(t, "acme/payments", source.FullName())
	assert.Equal(t, f.project.WorkspaceID(), source.WorkspaceID())
	assert.Equal(t, f.store.created, source)
	require.Len(t, f.rec.resources, 1)
	assert.Equal(t, iam.ResourceSource, f.rec.resources[0].Type)
	assert.Equal(t, f.project.ID(), f.rec.resources[0].ParentID)
	assert.Equal(t, 1, f.rec.events)
	require.Len(t, f.rec.audits, 1)
	assert.Equal(t, versioncontrol.EventSourceCreated, f.rec.audits[0].Type)
}

func TestRestoreSourceRefusesAnArchivedProject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	source := f.createSource(t)
	_, err := f.service.ArchiveSource(ctx, actor, orgID, source.ID())
	require.NoError(t, err)
	f.project.Archive()

	_, err = f.service.RestoreSource(ctx, actor, orgID, source.ID())
	assert.ErrorIs(t, err, versioncontrol.ErrArchived)
	assert.True(t, source.Archived())
}

type auditRecorder struct{ rec *recorder }

func (a auditRecorder) Record(ctx context.Context, e audit.Event) error {
	return a.rec.RecordAudit(ctx, e)
}
