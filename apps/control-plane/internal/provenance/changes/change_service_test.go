package changes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const (
	orgID   = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	source  = "src_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	actorID = "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

var actor = identity.Actor{PrincipalID: actorID, Kind: identity.ActorUser}

type allow struct {
	asked authorization.Request
}

func (a *allow) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = req
	return authorization.Decision{ID: "dcn_test", Outcome: authorization.OutcomeAllow}, nil
}

type recorder struct {
	events int
	audits []audit.Event
}

func (r *recorder) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (r *recorder) Record(_ context.Context, envelopes ...events.Envelope) error {
	r.events += len(envelopes)
	return nil
}

type changeStore struct {
	created *provenance.Change
}

func (s *changeStore) CreateChange(_ context.Context, c *provenance.Change) error {
	s.created = c
	return nil
}
func (s *changeStore) GetChange(context.Context, string, string) (*provenance.Change, error) {
	return s.created, nil
}
func (s *changeStore) ChangesForCommits(context.Context, string, string, []string) ([]*provenance.Change, error) {
	return nil, nil
}
func (s *changeStore) ListChanges(context.Context, provenance.ListChangesQuery) (*pagination.Result[*provenance.Change], error) {
	return nil, nil
}

type auditRecorder struct{ rec *recorder }

func (a auditRecorder) Record(ctx context.Context, e audit.Event) error {
	a.rec.audits = append(a.rec.audits, e)
	return nil
}

func TestRecordChange(t *testing.T) {
	sessionID := "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	authz := &allow{}
	rec := &recorder{}
	store := &changeStore{}
	service, err := NewChangeService(
		WithChangeRepo(store),
		WithAuthorizer(authz),
		WithTransactor(rec),
		WithOutboxRecorder(rec),
		WithAuditRecorder(auditRecorder{rec}),
	)
	require.NoError(t, err)

	change, err := service.RecordChange(context.Background(), actor, provenance.RecordChangeInput{
		OrganizationID: orgID, SourceID: source, SessionID: &sessionID,
		Title: "Add checkout", CommitSHAs: []string{"aaaaaaa"},
	})
	require.NoError(t, err)

	assert.Equal(t, provenance.PermissionChangeRecord, authz.asked.Permission)
	assert.Equal(t, source, authz.asked.ResourceID)
	assert.Equal(t, store.created, change)
	assert.Equal(t, 1, rec.events)
	require.Len(t, rec.audits, 1)
	assert.Equal(t, provenance.EventChangeRecorded, rec.audits[0].Type)
}
