package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// Records in other capabilities are plain strings: delivery keeps no
// foreign keys across capabilities.
const (
	orgA  = "org_a"
	orgB  = "org_b"
	wsA   = "wsp_a"
	wsB   = "wsp_b"
	projA = "prj_a"
	projB = "prj_b"
	teamA = "team_a"
	teamB = "team_b"
)

// t0 has microseconds, which PostgreSQL keeps, so round trips check precision.
var t0 = time.Date(2026, 10, 20, 9, 30, 15, 123456000, time.UTC)

// at fixes the domain clock d after t0, until the test ends, and returns it.
func at(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	clock.Fix(t, t0.Add(d))
	return t0.Add(d)
}

// must returns v, panicking on err; it keeps builders short.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type fixture struct {
	ctx        context.Context
	db         *gorm.DB
	tx         *database.Transactor
	envs       delivery.EnvironmentRepository
	lifecycles delivery.LifecycleRepository
	targets    delivery.TargetRepository
	clusters   delivery.ClusterRepository
	registries delivery.RegistryRepository
	apps       delivery.ApplicationRepository
	// clusterIDs remembers the Cluster each (organization, slug) test
	// created, so a second call reuses it.
	clusterIDs map[string]string
}

// newFixture gives the test a fresh database with the delivery schema, and
// fixes the clock at t0. It skips the test without PostgreSQL.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := databasetest.New(t, migrations.FS)
	at(t, 0)
	return &fixture{
		ctx:        context.Background(),
		db:         db,
		tx:         database.NewTransactor(db),
		envs:       NewEnvironmentRepository(db),
		lifecycles: NewLifecycleRepository(db),
		targets:    NewTargetRepository(db),
		clusters:   NewClusterRepository(db),
		registries: NewRegistryRepository(db),
		apps:       NewApplicationRepository(db),
		clusterIDs: map[string]string{},
	}
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Raw(query, args...).Scan(&n).Error)
	return int(n)
}

// assertLocked checks that another connection cannot lock the row now.
func (f *fixture) assertLocked(t *testing.T, table, rowID string) {
	t.Helper()
	err := f.db.Exec("SELECT 1 FROM delivery."+table+" WHERE id = ? FOR UPDATE NOWAIT", rowID).Error
	require.Error(t, err, "the row is locked by the transaction")
	assert.Contains(t, err.Error(), "55P03", "lock_not_available")
}

func (f *fixture) assertNotLocked(t *testing.T, table, rowID string) {
	t.Helper()
	require.NoError(t, f.db.Exec("SELECT 1 FROM delivery."+table+" WHERE id = ? FOR UPDATE NOWAIT", rowID).Error)
}

func newEnv(org, ws, slug string) *delivery.Environment {
	return must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID: org,
		WorkspaceID:    ws,
		Slug:           slug,
		DisplayName:    "Env " + slug,
		Tier:           delivery.TierStaging,
	}, nil))
}

func (f *fixture) createEnv(t *testing.T, org, ws, slug string) *delivery.Environment {
	t.Helper()
	e := newEnv(org, ws, slug)
	require.NoError(t, f.envs.CreateEnvironment(f.ctx, e))
	return e
}

func phase(name string, envIDs ...string) delivery.NewLifecyclePhaseProps {
	return delivery.NewLifecyclePhaseProps{Name: name, EnvironmentIDs: envIDs}
}

func newLifecycle(org, ws, slug string, phases ...delivery.NewLifecyclePhaseProps) *delivery.Lifecycle {
	return must(delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: org,
		WorkspaceID:    ws,
		Slug:           slug,
		DisplayName:    "Lifecycle " + slug,
		Phases:         phases,
	}, nil))
}

func (f *fixture) createLifecycle(t *testing.T, org, ws, slug string, phases ...delivery.NewLifecyclePhaseProps) *delivery.Lifecycle {
	t.Helper()
	l := newLifecycle(org, ws, slug, phases...)
	require.NoError(t, f.lifecycles.CreateLifecycle(f.ctx, l))
	return l
}

func newTarget(clusterID, org, envID, slug string) *delivery.Target {
	return must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org,
		EnvironmentID:  envID,
		ClusterID:      clusterID,
		Slug:           slug,
		DisplayName:    "Target " + slug,
		Runtime:        delivery.RuntimeKubernetes,
		Locator:        map[string]string{delivery.LocatorCluster: "cluster-1", delivery.LocatorNamespace: slug},
	}, nil))
}

// cluster returns the test Cluster for (org, slug), creating it once.
func (f *fixture) cluster(t *testing.T, org, slug string) *delivery.Cluster {
	t.Helper()
	key := org + "/" + slug
	if id, ok := f.clusterIDs[key]; ok {
		c, err := f.clusters.GetCluster(f.ctx, org, id)
		require.NoError(t, err)
		return c
	}
	c := newCluster(org, slug)
	require.NoError(t, f.clusters.CreateCluster(f.ctx, c))
	f.clusterIDs[key] = c.ID()
	return c
}

func (f *fixture) createTarget(t *testing.T, org, envID, slug string) *delivery.Target {
	t.Helper()
	tg := newTarget(f.cluster(t, org, "cluster-1").ID(), org, envID, slug)
	require.NoError(t, f.targets.CreateTarget(f.ctx, tg))
	return tg
}

func newCluster(org, slug string) *delivery.Cluster {
	return must(delivery.NewCluster(delivery.NewClusterProps{
		OrganizationID: org,
		Slug:           slug,
		DisplayName:    "Cluster " + slug,
		Runtime:        delivery.RuntimeKubernetes,
	}, nil))
}

func (f *fixture) createCluster(t *testing.T, org, slug string) *delivery.Cluster {
	t.Helper()
	c := newCluster(org, slug)
	require.NoError(t, f.clusters.CreateCluster(f.ctx, c))
	return c
}

func newRegistry(org, ws, slug string) *delivery.Registry {
	return must(delivery.NewRegistry(delivery.NewRegistryProps{
		OrganizationID: org,
		WorkspaceID:    ws,
		Slug:           slug,
		DisplayName:    "Registry " + slug,
		Kind:           delivery.RegistryOCI,
		URL:            "https://" + slug + ".example.com",
	}, nil))
}

func (f *fixture) createRegistry(t *testing.T, org, ws, slug string) *delivery.Registry {
	t.Helper()
	r := newRegistry(org, ws, slug)
	require.NoError(t, f.registries.CreateRegistry(f.ctx, r))
	return r
}

func newApp(org, ws, project, team, slug string) *delivery.Application {
	return must(delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID: org,
		WorkspaceID:    ws,
		ProjectID:      project,
		OwnerTeamID:    team,
		Slug:           slug,
		DisplayName:    "App " + slug,
	}, nil))
}

func (f *fixture) createApp(t *testing.T, org, ws, project, slug string) *delivery.Application {
	t.Helper()
	a := newApp(org, ws, project, teamA, slug)
	require.NoError(t, f.apps.CreateApplication(f.ctx, a))
	return a
}

func newArtifact(org, appID, name string) *delivery.Artifact {
	return must(delivery.NewArtifact(delivery.NewArtifactProps{
		OrganizationID: org,
		ApplicationID:  appID,
		Name:           name,
		ArtifactKind:   delivery.ArtifactContainerImage,
		ArtifactName:   "ghcr.io/acme/" + name,
		Required:       true,
	}, nil))
}

func (f *fixture) createArtifact(t *testing.T, org, appID, name string) *delivery.Artifact {
	t.Helper()
	a := newArtifact(org, appID, name)
	require.NoError(t, f.apps.CreateArtifact(f.ctx, a))
	return a
}

func gitRefRule(patterns ...string) delivery.NewChannelRuleProps {
	config := must(json.Marshal(release.GitRefRule{Patterns: patterns}))
	return delivery.NewChannelRuleProps{Kind: release.RuleGitRef, Config: config}
}

func newChannel(org, appID, lifecycleID, slug string, isDefault bool, rules ...delivery.NewChannelRuleProps) *delivery.Channel {
	return must(delivery.NewChannel(delivery.NewChannelProps{
		OrganizationID: org,
		ApplicationID:  appID,
		LifecycleID:    lifecycleID,
		Slug:           slug,
		DisplayName:    "Channel " + slug,
		IsDefault:      isDefault,
		Rules:          rules,
	}, nil))
}

func (f *fixture) createChannel(t *testing.T, org, appID, lifecycleID, slug string, isDefault bool, rules ...delivery.NewChannelRuleProps) *delivery.Channel {
	t.Helper()
	c := newChannel(org, appID, lifecycleID, slug, isDefault, rules...)
	require.NoError(t, f.apps.CreateChannel(f.ctx, c))
	return c
}

func newPlacement(org, appID, targetID string, config *delivery.ApplyConfig) *delivery.Placement {
	return must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: org,
		ApplicationID:  appID,
		TargetID:       targetID,
		ApplyConfig:    config,
	}))
}

// idsOf returns the IDs of objects in order.
func idsOf[T interface{ ID() string }](items []T) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID()
	}
	return out
}

// walk pages forward through a list from the first page and returns every
// page's IDs, checking the flags and cursors of each page.
func walk[T interface{ ID() string }](t *testing.T, q pagination.Query, list func(pagination.Query) (*pagination.Result[T], error)) ([][]string, []*pagination.Result[T]) {
	t.Helper()
	var pages [][]string
	var results []*pagination.Result[T]
	for {
		res, err := list(q)
		require.NoError(t, err)
		require.LessOrEqual(t, len(pages), 100, "walk does not end")
		assert.Equal(t, len(pages) > 0, res.HasPrevious, "page %d HasPrevious", len(pages))
		assert.Equal(t, res.HasPrevious, res.PreviousCursor != nil, "page %d has a previous cursor exactly when it has a previous page", len(pages))
		assert.Equal(t, res.HasNext, res.NextCursor != nil, "page %d has a next cursor exactly when it has a next page", len(pages))
		pages = append(pages, idsOf(res.Items))
		results = append(results, res)
		if !res.HasNext {
			return pages, results
		}
		q.NextCursor, q.PreviousCursor = res.NextCursor, nil
	}
}

func flatten(pages [][]string) []string {
	var out []string
	for _, p := range pages {
		out = append(out, p...)
	}
	return out
}
