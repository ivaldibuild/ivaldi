package releases

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

const (
	org     = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	project = "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	ws      = "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	team    = "tem_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

var (
	t0  = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}
)

func digest(c string) string { return "sha256:" + strings.Repeat(c, 64) }

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// state is what the fake repositories store, as snapshots, so a failed
// transaction can put it back and every read rebuilds objects as the
// PostgreSQL repositories do.
type state struct {
	builds   map[string]delivery.BuildSnapshot
	releases map[string]delivery.ReleaseSnapshot
	audit    []audit.Event
	events   []events.Envelope
}

func (s state) clone() state {
	return state{
		builds: maps.Clone(s.builds), releases: maps.Clone(s.releases),
		audit: slices.Clone(s.audit), events: slices.Clone(s.events),
	}
}

// store fakes the repositories, the Application lookups, the tag resolver,
// and the transactor. The recorders write into it through auditLog and
// outboxLog.
type store struct {
	state
	app       *delivery.Application
	artifacts []*delivery.Artifact
	channels  []*delivery.Channel
	tags      map[string]string
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

func loadBuild(snap delivery.BuildSnapshot) *delivery.Build {
	return must(delivery.NewBuild(delivery.NewBuildProps{
		OrganizationID: snap.OrganizationID, ArtifactKind: snap.ArtifactKind, ArtifactName: snap.ArtifactName,
		Digest: snap.Digest, Provenance: snap.Provenance,
		Details: delivery.BuildDetails{
			Version: snap.Version, SourceID: snap.SourceID, CommitSHA: snap.CommitSHA, SourceRef: snap.SourceRef,
			CIProvider: snap.CIProvider, CIRunRef: snap.CIRunRef, ReportedTags: snap.ReportedTags, BuiltAt: snap.BuiltAt,
		},
		CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt,
	}, &snap.ID))
}

func loadRelease(snap delivery.ReleaseSnapshot) *delivery.Release {
	props := delivery.NewReleaseProps{
		OrganizationID: snap.OrganizationID, ApplicationID: snap.ApplicationID, ChannelID: snap.ChannelID,
		Version: snap.Version, Status: snap.Status, Source: snap.Source, CreatedByPrincipalID: snap.CreatedByPrincipalID,
		PublishDecisionID: snap.PublishDecisionID, PublishedAt: snap.PublishedAt, WithdrawnAt: snap.WithdrawnAt,
		CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt,
	}
	for _, b := range snap.Builds {
		props.Builds = append(props.Builds, delivery.NewReleaseBuildProps{ArtifactID: b.ArtifactID, BuildID: b.BuildID, CreatedAt: &b.CreatedAt})
	}
	return must(delivery.NewRelease(props, &snap.ID))
}

// Builds.

func (s *store) CreateBuild(_ context.Context, b *delivery.Build) (bool, error) {
	n := b.Snapshot()
	for _, o := range s.builds {
		if o.OrganizationID == n.OrganizationID && o.ArtifactName == n.ArtifactName && o.Digest == n.Digest {
			return false, nil
		}
	}
	s.builds[n.ID] = n
	return true, nil
}

func (s *store) GetBuild(_ context.Context, organizationID, buildID string) (*delivery.Build, error) {
	snap, ok := s.builds[buildID]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return loadBuild(snap), nil
}

func (s *store) GetBuildByDigestForUpdate(_ context.Context, organizationID, artifactName, d string) (*delivery.Build, error) {
	for _, snap := range s.builds {
		if snap.OrganizationID == organizationID && snap.ArtifactName == artifactName && snap.Digest == d {
			return loadBuild(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) GetBuilds(_ context.Context, organizationID string, ids []string) ([]*delivery.Build, error) {
	var out []*delivery.Build
	for _, id := range ids {
		if snap, ok := s.builds[id]; ok && snap.OrganizationID == organizationID {
			out = append(out, loadBuild(snap))
		}
	}
	return out, nil
}

func (s *store) UpdateBuild(_ context.Context, b *delivery.Build) error {
	s.builds[b.ID()] = b.Snapshot()
	return nil
}

// Releases.

func (s *store) CreateRelease(_ context.Context, r *delivery.Release) error {
	n := r.Snapshot()
	for _, o := range s.releases {
		if o.ApplicationID == n.ApplicationID && o.Version == n.Version {
			return delivery.ErrAlreadyExists
		}
	}
	s.releases[n.ID] = n
	return nil
}

func (s *store) GetRelease(_ context.Context, organizationID, releaseID string) (*delivery.Release, error) {
	snap, ok := s.releases[releaseID]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return loadRelease(snap), nil
}

func (s *store) GetReleaseForUpdate(ctx context.Context, organizationID, releaseID string) (*delivery.Release, error) {
	return s.GetRelease(ctx, organizationID, releaseID)
}

func (s *store) ListReleases(_ context.Context, q delivery.ListReleasesQuery) (*pagination.Result[*delivery.Release], error) {
	var items []*delivery.Release
	for _, snap := range s.releases {
		if snap.ApplicationID != q.ApplicationID || (q.ChannelID != "" && snap.ChannelID != q.ChannelID) || (q.Status != "" && snap.Status != q.Status) {
			continue
		}
		items = append(items, loadRelease(snap))
	}
	slices.SortFunc(items, func(a, b *delivery.Release) int { return cmp.Compare(a.Version(), b.Version()) })
	return &pagination.Result[*delivery.Release]{Items: items, TotalCount: len(items)}, nil
}

func (s *store) UpdateRelease(_ context.Context, r *delivery.Release) error {
	s.releases[r.ID()] = r.Snapshot()
	return nil
}

// Applications.

func (s *store) GetApplication(_ context.Context, organizationID, applicationID string) (*delivery.Application, error) {
	if s.app == nil || s.app.ID() != applicationID || organizationID != org {
		return nil, delivery.ErrNotFound
	}
	return s.app, nil
}

func (s *store) ListArtifacts(_ context.Context, _, _ string, includeArchived bool) ([]*delivery.Artifact, error) {
	var out []*delivery.Artifact
	for _, a := range s.artifacts {
		if includeArchived || !a.Archived() {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *store) ListChannels(_ context.Context, _, _ string, includeArchived bool) ([]*delivery.Channel, error) {
	var out []*delivery.Channel
	for _, c := range s.channels {
		if includeArchived || !c.Archived() {
			out = append(out, c)
		}
	}
	return out, nil
}

// Tags.

type tagResolver struct{ *store }

func (t tagResolver) ResolveTag(_ context.Context, _ string, a *delivery.Artifact, tag string) (string, error) {
	d, ok := t.tags[a.ArtifactName()+":"+tag]
	if !ok {
		return "", delivery.ErrTagsUnsupported
	}
	return d, nil
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.audit = append(a.s.audit, e)
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.events = append(o.s.events, envs...)
	return nil
}

func (s *store) eventTypes() []string {
	var out []string
	for _, e := range s.events {
		out = append(out, e.Type)
	}
	return out
}

func (s *store) auditTypes() []string {
	var out []string
	for _, e := range s.audit {
		out = append(out, e.Type)
	}
	return out
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's action.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.Action, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
		d.Reason = "denied by the test"
	}
	return d, nil
}

func (a *authorizer) permissions() []string {
	var out []string
	for _, r := range a.asked {
		out = append(out, r.Permission)
	}
	return out
}

// world is an Application with a required api image, an optional chart, and
// an archived migrate image; a default main Channel for versions without a
// pre-release tag, a beta Channel for betas, and an archived old Channel.
type world struct {
	*store
	svc             delivery.ReleaseService
	authz           *authorizer
	api, chart      *delivery.Artifact
	main, beta, old *delivery.Channel
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock.Fix(t, t0)
	app := must(delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID: org, WorkspaceID: ws, ProjectID: project, OwnerTeamID: team, DisplayName: "Payments API",
	}, nil))
	artifact := func(name string, kind delivery.ArtifactKind, required bool) *delivery.Artifact {
		return must(delivery.NewArtifact(delivery.NewArtifactProps{
			OrganizationID: org, ApplicationID: app.ID(), Name: name, ArtifactKind: kind,
			ArtifactName: "ghcr.io/acme/" + name, Required: required,
		}, nil))
	}
	api := artifact("api", delivery.ArtifactContainerImage, true)
	chart := artifact("chart", delivery.ArtifactHelmChart, false)
	migrate := artifact("migrate", delivery.ArtifactContainerImage, true)
	migrate.Archive()

	lifecycle := "lfc_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	channel := func(slug string, isDefault bool, rules ...string) *delivery.Channel {
		props := delivery.NewChannelProps{OrganizationID: org, ApplicationID: app.ID(), LifecycleID: lifecycle, Slug: slug, DisplayName: slug, IsDefault: isDefault}
		for _, r := range rules {
			props.Rules = append(props.Rules, delivery.NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(r)})
		}
		return must(delivery.NewChannel(props, nil))
	}
	main := channel("main", true, `{"range":"0.0","pre_release":"^$"}`)
	beta := channel("beta", false, `{"range":"0.0","pre_release":"^beta"}`)
	old := channel("old", false)
	require.NoError(t, old.Archive())

	s := &store{
		state:     state{builds: map[string]delivery.BuildSnapshot{}, releases: map[string]delivery.ReleaseSnapshot{}},
		app:       app,
		artifacts: []*delivery.Artifact{api, chart, migrate},
		channels:  []*delivery.Channel{main, beta, old},
		tags:      map[string]string{},
	}
	authz := &authorizer{}
	svc, err := NewReleaseService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, svc: svc, authz: authz, api: api, chart: chart, main: main, beta: beta, old: old}
}

func (s *store) options(authz authorization.Authorizer) []ReleaseServiceOption {
	return []ReleaseServiceOption{
		WithBuildRepo(s), WithReleaseRepo(s), WithApplications(s), WithTagResolver(tagResolver{s}),
		WithAuthorizer(authz), WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// apiBuild is a Build of the api image as a tag pipeline reports it.
func apiBuild(c string) delivery.BuildInput {
	return delivery.BuildInput{
		Artifact: "api", Digest: digest(c),
		Details: delivery.BuildDetails{
			Version: new("1.4.0"), CommitSHA: new(strings.Repeat(c, 40)), SourceRef: new("refs/tags/v1.4.0"),
			CIProvider: new("github_actions"), CIRunRef: new("https://github.com/acme/api/actions/runs/1"),
		},
	}
}

func (w *world) create(t *testing.T, version string, builds ...delivery.BuildInput) *delivery.ReleaseDetail {
	t.Helper()
	d, err := w.svc.CreateRelease(context.Background(), ada, delivery.CreateReleaseInput{
		OrganizationID: org, ApplicationID: w.app.ID(), Version: version, Builds: builds,
	})
	require.NoError(t, err)
	return d
}
