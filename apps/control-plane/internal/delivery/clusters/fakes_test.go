package clusters

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

const org = "org_acme"

// state is everything the fakes store. A transaction restores it when its
// function fails, as a rolled-back database transaction would.
type state struct {
	clusters map[string]delivery.ClusterSnapshot
	audit    []audit.Event
	events   []events.Envelope
	revoked  []string
	// activeTargets is what CountActiveTargets reports for every Cluster.
	activeTargets int
}

func (s state) clone() state {
	return state{
		clusters: maps.Clone(s.clusters),
		audit:    append([]audit.Event(nil), s.audit...),
		events:   append([]events.Envelope(nil), s.events...),
		revoked:  append([]string(nil), s.revoked...),
	}
}

// store fakes the Cluster repository and the transactor. auditLog,
// outboxLog, and revoker fake the recorders and the agent revoker.
type store struct {
	state
}

func newStore() *store {
	return &store{state: state{clusters: map[string]delivery.ClusterSnapshot{}}}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

func rebuildCluster(snapshot delivery.ClusterSnapshot) *delivery.Cluster {
	return must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID:    snapshot.OrganizationID,
		Slug:              snapshot.Slug,
		DisplayName:       snapshot.DisplayName,
		Runtime:           snapshot.Runtime,
		AgentPrincipalID:  snapshot.AgentPrincipalID,
		AgentCapabilities: snapshot.AgentCapabilities,
		AgentLastFailure:  snapshot.AgentLastFailure,
		LastReportedAt:    snapshot.LastReportedAt,
		CreatedAt:         &snapshot.CreatedAt,
		UpdatedAt:         &snapshot.UpdatedAt,
		DeletedAt:         snapshot.DeletedAt,
	}, &snapshot.ID))
}

func (s *store) CreateCluster(_ context.Context, c *delivery.Cluster) error {
	snapshot := c.Snapshot()
	for _, other := range s.clusters {
		if other.OrganizationID == snapshot.OrganizationID && other.Slug == snapshot.Slug && other.DeletedAt == nil {
			return delivery.ErrSlugTaken
		}
	}
	s.clusters[snapshot.ID] = snapshot
	return nil
}

func (s *store) GetCluster(_ context.Context, organizationID, clusterID string) (*delivery.Cluster, error) {
	snapshot, ok := s.clusters[clusterID]
	if !ok || snapshot.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildCluster(snapshot), nil
}

func (s *store) GetClusterForUpdate(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error) {
	return s.GetCluster(ctx, organizationID, clusterID)
}

func (s *store) GetClusterBySlug(_ context.Context, organizationID, slug string) (*delivery.Cluster, error) {
	for _, snapshot := range s.clusters {
		if snapshot.OrganizationID == organizationID && snapshot.Slug == slug && snapshot.DeletedAt == nil {
			return rebuildCluster(snapshot), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) GetClusterByAgentPrincipalID(_ context.Context, organizationID, principalID string) (*delivery.Cluster, error) {
	for _, snapshot := range s.clusters {
		if snapshot.OrganizationID == organizationID && snapshot.DeletedAt == nil &&
			snapshot.AgentPrincipalID != nil && *snapshot.AgentPrincipalID == principalID {
			return rebuildCluster(snapshot), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) ListClusters(_ context.Context, query delivery.ListClustersQuery) (*pagination.Result[*delivery.Cluster], error) {
	var out []*delivery.Cluster
	for _, snapshot := range s.clusters {
		if snapshot.OrganizationID != query.OrganizationID {
			continue
		}
		if snapshot.DeletedAt != nil && !query.IncludeArchived {
			continue
		}
		out = append(out, rebuildCluster(snapshot))
	}
	return &pagination.Result[*delivery.Cluster]{Items: out, TotalCount: len(out)}, nil
}

func (s *store) CountActiveTargets(_ context.Context, _, _ string) (int, error) {
	return s.activeTargets, nil
}

func (s *store) UpdateCluster(_ context.Context, c *delivery.Cluster) error {
	snapshot := c.Snapshot()
	if _, ok := s.clusters[snapshot.ID]; !ok {
		return delivery.ErrNotFound
	}
	for id, other := range s.clusters {
		if id != snapshot.ID && other.OrganizationID == snapshot.OrganizationID &&
			other.Slug == snapshot.Slug && other.DeletedAt == nil && snapshot.DeletedAt == nil {
			return delivery.ErrSlugTaken
		}
	}
	s.clusters[snapshot.ID] = snapshot
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envelopes ...events.Envelope) error {
	o.s.events = append(o.s.events, envelopes...)
	return nil
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.audit = append(a.s.audit, e)
	return nil
}

type revoker struct{ s *store }

func (r revoker) RevokeAgent(_ context.Context, _, principalID string) error {
	r.s.revoked = append(r.s.revoked, principalID)
	return nil
}

// authorizer allows everything except what deny refuses, and remembers what
// it was asked. Each decision's ID is "dec-" and the request's resource ID.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
	}
	return d, nil
}

type world struct {
	*store
	authz *authorizer
	svc   delivery.ClusterService
}

func newWorld(t *testing.T) *world {
	t.Helper()
	s := newStore()
	authz := &authorizer{}
	svc, err := NewClusterService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, authz: authz, svc: svc}
}

func (s *store) options(authz authorization.Authorizer) []ClusterServiceOption {
	return []ClusterServiceOption{
		WithClusterRepo(s), WithAgentRevoker(revoker{s}), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// cluster stores a Cluster directly, bypassing the service.
func (w *world) cluster(t *testing.T, slug, agent string, archived bool) *delivery.Cluster {
	t.Helper()
	props := delivery.NewClusterProps{
		OrganizationID: org, Slug: slug, DisplayName: slug, Runtime: delivery.RuntimeKubernetes,
	}
	if agent != "" {
		props.AgentPrincipalID = &agent
	}
	c := must(delivery.NewCluster(props, nil))
	if archived {
		c.Archive()
	}
	c.PullEvents()
	require.NoError(t, w.CreateCluster(context.Background(), c))
	return c
}

func (w *world) eventTypes() []string {
	var out []string
	for _, e := range w.events {
		out = append(out, e.Type)
	}
	return out
}

// reset forgets what was recorded so far.
func (w *world) reset() {
	w.audit, w.events, w.revoked, w.authz.asked = nil, nil, nil, nil
}
