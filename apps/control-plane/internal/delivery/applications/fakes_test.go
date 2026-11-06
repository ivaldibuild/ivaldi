package applications

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

const org = "org_acme"

// state is everything the fakes store. A transaction restores it when its
// function fails, as a rolled-back database transaction would.
type state struct {
	applications map[string]delivery.ApplicationSnapshot
	artifacts    map[string]delivery.ArtifactSnapshot
	channels     map[string]delivery.ChannelSnapshot
	placements   map[string]delivery.PlacementSnapshot
	lifecycles   map[string]delivery.LifecycleSnapshot
	registries   map[string]delivery.RegistrySnapshot
	environments map[string]delivery.EnvironmentSnapshot
	targets      map[string]delivery.TargetSnapshot
	audit        []audit.Event
	events       []events.Envelope
}

func (s state) clone() state {
	return state{
		applications: maps.Clone(s.applications),
		artifacts:    maps.Clone(s.artifacts),
		channels:     maps.Clone(s.channels),
		placements:   maps.Clone(s.placements),
		lifecycles:   maps.Clone(s.lifecycles),
		registries:   maps.Clone(s.registries),
		environments: maps.Clone(s.environments),
		targets:      maps.Clone(s.targets),
		audit:        append([]audit.Event(nil), s.audit...),
		events:       append([]events.Envelope(nil), s.events...),
	}
}

// store fakes the repository, the lookups, the transactor, the Team
// directory, and the Workspaces lookup, with the uniqueness rules the
// migrations enforce. Its auditLog and outboxLog fake the recorders.
type store struct {
	state
	workspaces map[string]*workspace.Workspace
	projects   map[string]*workspace.Project
	// teams says which Teams are active.
	teams map[string]bool
	// inUse names placements ("app/target") that Deployments refer to.
	inUse map[string]bool
	// fail makes the named method return the error.
	fail map[string]error
	// settings holds setting values by key and then by the ID of the
	// Workspace or Project they are set for.
	settings map[string]map[string]json.RawMessage
	// resolved records the scopes settings were resolved for.
	resolved []workspace.Scope
}

func newStore() *store {
	return &store{
		state: state{
			applications: map[string]delivery.ApplicationSnapshot{},
			artifacts:    map[string]delivery.ArtifactSnapshot{},
			channels:     map[string]delivery.ChannelSnapshot{},
			placements:   map[string]delivery.PlacementSnapshot{},
			lifecycles:   map[string]delivery.LifecycleSnapshot{},
			registries:   map[string]delivery.RegistrySnapshot{},
			environments: map[string]delivery.EnvironmentSnapshot{},
			targets:      map[string]delivery.TargetSnapshot{},
		},
		workspaces: map[string]*workspace.Workspace{},
		projects:   map[string]*workspace.Project{},
		teams:      map[string]bool{},
		inUse:      map[string]bool{},
		fail:       map[string]error{},
		settings:   map[string]map[string]json.RawMessage{},
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type txKey struct{}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(context.WithValue(ctx, txKey{}, true)); err != nil {
		s.state = saved
		return err
	}
	return nil
}

// requireTx is what FOR UPDATE needs: a transaction.
func requireTx(ctx context.Context, method string) error {
	if ctx.Value(txKey{}) == nil {
		return fmt.Errorf("%s called outside a transaction", method)
	}
	return nil
}

// sorted returns the values that keep accepts, by creation time and then key.
func sorted[S any](m map[string]S, keep func(S) bool, created func(S) time.Time) []S {
	keys := slices.Collect(maps.Keys(m))
	slices.SortFunc(keys, func(a, b string) int {
		if c := created(m[a]).Compare(created(m[b])); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	var out []S
	for _, k := range keys {
		if keep(m[k]) {
			out = append(out, m[k])
		}
	}
	return out
}

// Rebuilding, as the PostgreSQL mappers do.

func rebuildApplication(s delivery.ApplicationSnapshot) *delivery.Application {
	return must(delivery.NewApplication(delivery.NewApplicationProps{
		OrganizationID: s.OrganizationID, WorkspaceID: s.WorkspaceID, ProjectID: s.ProjectID, OwnerTeamID: s.OwnerTeamID,
		Slug: s.Slug, DisplayName: s.DisplayName, SourceID: s.SourceID, WorkloadSelector: s.WorkloadSelector,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, ArchivedAt: s.ArchivedAt,
	}, &s.ID))
}

func rebuildArtifact(s delivery.ArtifactSnapshot) *delivery.Artifact {
	return must(delivery.NewArtifact(delivery.NewArtifactProps{
		OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, Name: s.Name, ArtifactKind: s.ArtifactKind,
		ArtifactName: s.ArtifactName, RegistryID: s.RegistryID, Required: s.Required, Source: s.Source,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, ArchivedAt: s.ArchivedAt,
	}, &s.ID))
}

func rebuildChannel(s delivery.ChannelSnapshot) *delivery.Channel {
	rules := make([]delivery.NewChannelRuleProps, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = delivery.NewChannelRuleProps{ID: &r.ID, Kind: r.Kind, ArtifactID: r.ArtifactID, Config: r.Config, CreatedAt: &r.CreatedAt, UpdatedAt: &r.UpdatedAt}
	}
	return must(delivery.NewChannel(delivery.NewChannelProps{
		OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, LifecycleID: s.LifecycleID, Slug: s.Slug,
		DisplayName: s.DisplayName, Description: s.Description, IsDefault: s.IsDefault, Rules: rules,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, ArchivedAt: s.ArchivedAt,
	}, &s.ID))
}

func rebuildPlacement(s delivery.PlacementSnapshot) *delivery.Placement {
	return must(delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: s.OrganizationID, ApplicationID: s.ApplicationID, TargetID: s.TargetID, Source: s.Source,
		ApplyConfig: s.ApplyConfig, CurrentDeploymentID: s.CurrentDeploymentID, CurrentReleaseID: s.CurrentReleaseID,
		CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt,
	}))
}

func rebuildLifecycle(s delivery.LifecycleSnapshot) *delivery.Lifecycle {
	phases := make([]delivery.NewLifecyclePhaseProps, len(s.Phases))
	for i, p := range s.Phases {
		phases[i] = delivery.NewLifecyclePhaseProps{ID: &p.ID, Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments, EnvironmentIDs: p.EnvironmentIDs, CreatedAt: &p.CreatedAt, UpdatedAt: &p.UpdatedAt}
	}
	return must(delivery.NewLifecycle(delivery.NewLifecycleProps{
		OrganizationID: s.OrganizationID, WorkspaceID: s.WorkspaceID, Slug: s.Slug, DisplayName: s.DisplayName,
		Description: s.Description, Phases: phases, CreatedAt: &s.CreatedAt, UpdatedAt: &s.UpdatedAt, DeletedAt: s.DeletedAt,
	}, &s.ID))
}

// Applications.

func (s *store) failed(method string) error { return s.fail[method] }

func (s *store) checkApplication(snap delivery.ApplicationSnapshot) error {
	for _, o := range s.applications {
		if o.ID != snap.ID && o.ProjectID == snap.ProjectID && o.ArchivedAt == nil && snap.ArchivedAt == nil && o.Slug == snap.Slug {
			return delivery.ErrSlugTaken
		}
	}
	return nil
}

func (s *store) CreateApplication(_ context.Context, a *delivery.Application) error {
	if err := s.failed("CreateApplication"); err != nil {
		return err
	}
	snap := a.Snapshot()
	if err := s.checkApplication(snap); err != nil {
		return err
	}
	s.applications[snap.ID] = snap
	return nil
}

func (s *store) GetApplication(_ context.Context, organizationID, id string) (*delivery.Application, error) {
	snap, ok := s.applications[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildApplication(snap), nil
}

func (s *store) GetApplicationForUpdate(ctx context.Context, organizationID, id string) (*delivery.Application, error) {
	if err := requireTx(ctx, "GetApplicationForUpdate"); err != nil {
		return nil, err
	}
	return s.GetApplication(ctx, organizationID, id)
}

func (s *store) GetApplicationBySlug(_ context.Context, organizationID, projectID, slug string) (*delivery.Application, error) {
	for _, snap := range s.applications {
		if snap.OrganizationID == organizationID && snap.ProjectID == projectID && snap.Slug == slug && snap.ArchivedAt == nil {
			return rebuildApplication(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) ListApplications(_ context.Context, q delivery.ListApplicationsQuery) (*pagination.Result[*delivery.Application], error) {
	out := &pagination.Result[*delivery.Application]{}
	for _, snap := range sorted(s.applications, func(a delivery.ApplicationSnapshot) bool {
		return a.OrganizationID == q.OrganizationID &&
			(q.ProjectID == "" || a.ProjectID == q.ProjectID) &&
			(q.WorkspaceID == "" || a.WorkspaceID == q.WorkspaceID) &&
			(q.OwnerTeamID == "" || a.OwnerTeamID == q.OwnerTeamID) &&
			(q.IncludeArchived || a.ArchivedAt == nil)
	}, func(a delivery.ApplicationSnapshot) time.Time { return a.CreatedAt }) {
		out.Items = append(out.Items, rebuildApplication(snap))
	}
	out.TotalCount = len(out.Items)
	return out, nil
}

func (s *store) UpdateApplication(_ context.Context, a *delivery.Application) error {
	if _, ok := s.applications[a.ID()]; !ok {
		return delivery.ErrNotFound
	}
	snap := a.Snapshot()
	if err := s.checkApplication(snap); err != nil {
		return err
	}
	s.applications[a.ID()] = snap
	return nil
}

func (s *store) CountActiveApplications(_ context.Context, organizationID, projectID string) (int, error) {
	n := 0
	for _, snap := range s.applications {
		if snap.OrganizationID == organizationID && snap.ProjectID == projectID && snap.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

func (s *store) CountActiveApplicationsOwnedBy(_ context.Context, organizationID, teamID string) (int, error) {
	n := 0
	for _, snap := range s.applications {
		if snap.OrganizationID == organizationID && snap.OwnerTeamID == teamID && snap.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

// Artifacts.

func (s *store) checkArtifact(snap delivery.ArtifactSnapshot) error {
	if _, ok := s.applications[snap.ApplicationID]; !ok {
		return fmt.Errorf("%w: foreign key: application %s", delivery.ErrNotFound, snap.ApplicationID)
	}
	for _, o := range s.artifacts {
		if o.ID != snap.ID && o.ApplicationID == snap.ApplicationID && o.ArchivedAt == nil && snap.ArchivedAt == nil && o.Name == snap.Name {
			return delivery.ErrSlugTaken
		}
	}
	return nil
}

func (s *store) CreateArtifact(_ context.Context, a *delivery.Artifact) error {
	snap := a.Snapshot()
	if err := s.checkArtifact(snap); err != nil {
		return err
	}
	s.artifacts[snap.ID] = snap
	return nil
}

func (s *store) GetArtifact(_ context.Context, organizationID, id string) (*delivery.Artifact, error) {
	snap, ok := s.artifacts[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildArtifact(snap), nil
}

func (s *store) ListArtifacts(_ context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Artifact, error) {
	var out []*delivery.Artifact
	for _, snap := range sorted(s.artifacts, func(a delivery.ArtifactSnapshot) bool {
		return a.OrganizationID == organizationID && a.ApplicationID == applicationID && (includeArchived || a.ArchivedAt == nil)
	}, func(a delivery.ArtifactSnapshot) time.Time { return a.CreatedAt }) {
		out = append(out, rebuildArtifact(snap))
	}
	return out, nil
}

func (s *store) UpdateArtifact(_ context.Context, a *delivery.Artifact) error {
	if _, ok := s.artifacts[a.ID()]; !ok {
		return delivery.ErrNotFound
	}
	snap := a.Snapshot()
	if err := s.checkArtifact(snap); err != nil {
		return err
	}
	s.artifacts[a.ID()] = snap
	return nil
}

// Channels.

func (s *store) checkChannel(snap delivery.ChannelSnapshot) error {
	if _, ok := s.applications[snap.ApplicationID]; !ok {
		return fmt.Errorf("%w: foreign key: application %s", delivery.ErrNotFound, snap.ApplicationID)
	}
	if _, ok := s.lifecycles[snap.LifecycleID]; !ok {
		return fmt.Errorf("%w: foreign key: lifecycle %s", delivery.ErrNotFound, snap.LifecycleID)
	}
	for _, o := range s.channels {
		if o.ID == snap.ID || o.ApplicationID != snap.ApplicationID {
			continue
		}
		if o.ArchivedAt == nil && snap.ArchivedAt == nil && o.Slug == snap.Slug {
			return delivery.ErrSlugTaken
		}
		if o.IsDefault && snap.IsDefault {
			return delivery.ErrConflict
		}
	}
	return nil
}

func (s *store) CreateChannel(_ context.Context, c *delivery.Channel) error {
	if err := s.failed("CreateChannel"); err != nil {
		return err
	}
	snap := c.Snapshot()
	if err := s.checkChannel(snap); err != nil {
		return err
	}
	s.channels[snap.ID] = snap
	return nil
}

func (s *store) GetChannel(_ context.Context, organizationID, id string) (*delivery.Channel, error) {
	snap, ok := s.channels[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildChannel(snap), nil
}

func (s *store) GetDefaultChannel(_ context.Context, organizationID, applicationID string) (*delivery.Channel, error) {
	for _, snap := range s.channels {
		if snap.OrganizationID == organizationID && snap.ApplicationID == applicationID && snap.IsDefault {
			return rebuildChannel(snap), nil
		}
	}
	return nil, delivery.ErrNotFound
}

func (s *store) ListChannels(_ context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Channel, error) {
	var out []*delivery.Channel
	for _, snap := range sorted(s.channels, func(c delivery.ChannelSnapshot) bool {
		return c.OrganizationID == organizationID && c.ApplicationID == applicationID && (includeArchived || c.ArchivedAt == nil)
	}, func(c delivery.ChannelSnapshot) time.Time { return c.CreatedAt }) {
		out = append(out, rebuildChannel(snap))
	}
	return out, nil
}

func (s *store) UpdateChannel(_ context.Context, c *delivery.Channel) error {
	if _, ok := s.channels[c.ID()]; !ok {
		return delivery.ErrNotFound
	}
	snap := c.Snapshot()
	if err := s.checkChannel(snap); err != nil {
		return err
	}
	s.channels[c.ID()] = snap
	return nil
}

func (s *store) CountActiveChannelsWithLifecycle(_ context.Context, organizationID, lifecycleID string) (int, error) {
	n := 0
	for _, snap := range s.channels {
		app, ok := s.applications[snap.ApplicationID]
		if snap.OrganizationID == organizationID && snap.LifecycleID == lifecycleID && snap.ArchivedAt == nil && ok && app.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

// Placements.

func placementKey(applicationID, targetID string) string { return applicationID + "/" + targetID }

func (s *store) CreatePlacement(_ context.Context, p *delivery.Placement) error {
	snap := p.Snapshot()
	k := placementKey(snap.ApplicationID, snap.TargetID)
	if _, ok := s.placements[k]; ok {
		return delivery.ErrAlreadyExists
	}
	s.placements[k] = snap
	return nil
}

func (s *store) GetPlacement(_ context.Context, organizationID, applicationID, targetID string) (*delivery.Placement, error) {
	snap, ok := s.placements[placementKey(applicationID, targetID)]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildPlacement(snap), nil
}

func (s *store) ListPlacements(_ context.Context, organizationID, applicationID string) ([]*delivery.Placement, error) {
	var out []*delivery.Placement
	for _, snap := range sorted(s.placements, func(p delivery.PlacementSnapshot) bool {
		return p.OrganizationID == organizationID && p.ApplicationID == applicationID
	}, func(p delivery.PlacementSnapshot) time.Time { return p.CreatedAt }) {
		out = append(out, rebuildPlacement(snap))
	}
	return out, nil
}

func (s *store) UpdatePlacement(_ context.Context, p *delivery.Placement) error {
	k := placementKey(p.ApplicationID(), p.TargetID())
	if _, ok := s.placements[k]; !ok {
		return delivery.ErrNotFound
	}
	s.placements[k] = p.Snapshot()
	return nil
}

func (s *store) DeletePlacement(_ context.Context, organizationID, applicationID, targetID string) error {
	k := placementKey(applicationID, targetID)
	if o, ok := s.placements[k]; !ok || o.OrganizationID != organizationID {
		return delivery.ErrNotFound
	}
	if s.inUse[k] {
		return fmt.Errorf("%w: deployments refer to application %s on target %s", delivery.ErrInUse, applicationID, targetID)
	}
	delete(s.placements, k)
	return nil
}

// Lookups other capabilities' records.

func (s *store) GetLifecycleForUpdate(ctx context.Context, organizationID, id string) (*delivery.Lifecycle, error) {
	if err := requireTx(ctx, "GetLifecycleForUpdate"); err != nil {
		return nil, err
	}
	snap, ok := s.lifecycles[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuildLifecycle(snap), nil
}

func (s *store) GetRegistry(_ context.Context, organizationID, id string) (*delivery.Registry, error) {
	snap, ok := s.registries[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return must(delivery.NewRegistry(delivery.NewRegistryProps{
		OrganizationID: snap.OrganizationID, WorkspaceID: snap.WorkspaceID, Slug: snap.Slug, DisplayName: snap.DisplayName,
		Kind: snap.Kind, URL: snap.URL, CredentialRef: snap.CredentialRef, LookupFrom: snap.LookupFrom, LookupPool: snap.LookupPool,
		CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt, DeletedAt: snap.DeletedAt,
	}, &snap.ID)), nil
}

func (s *store) GetTarget(_ context.Context, organizationID, id string) (*delivery.Target, error) {
	snap, ok := s.targets[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: snap.OrganizationID, EnvironmentID: snap.EnvironmentID, Slug: snap.Slug, DisplayName: snap.DisplayName,
		Runtime: snap.Runtime, Locator: snap.Locator, AgentPrincipalID: snap.AgentPrincipalID, LastReportedAt: snap.LastReportedAt,
		CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt, DeletedAt: snap.DeletedAt,
	}, &snap.ID)), nil
}

func (s *store) GetEnvironment(_ context.Context, organizationID, id string) (*delivery.Environment, error) {
	snap, ok := s.environments[id]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
		OrganizationID: snap.OrganizationID, WorkspaceID: snap.WorkspaceID, Slug: snap.Slug, DisplayName: snap.DisplayName,
		Tier: snap.Tier, ExecutionEnabled: snap.ExecutionEnabled,
		CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt, DeletedAt: snap.DeletedAt,
	}, &snap.ID)), nil
}

func (s *store) LookupWorkspace(_ context.Context, _, workspaceID string) (*workspace.Workspace, error) {
	w, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return w, nil
}

func (s *store) LookupProject(_ context.Context, _, projectID string) (*workspace.Project, error) {
	p, ok := s.projects[projectID]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return p, nil
}

// ResolveSetting resolves a setting as workspace.SettingResolver does, from
// the values set for the scope's Project, then its Workspace. A "resolve"
// entry in fail makes it fail.
func (s *store) ResolveSetting(_ context.Context, scope workspace.Scope, key string) (workspace.Resolved, error) {
	s.resolved = append(s.resolved, scope)
	if err := s.fail["resolve"]; err != nil {
		return workspace.Resolved{}, err
	}
	values := s.settings[key]
	if v, ok := values[scope.ProjectID]; ok && scope.ProjectID != "" {
		return workspace.Resolved{Key: key, Value: v, Level: workspace.LevelProject}, nil
	}
	if v, ok := values[scope.WorkspaceID]; ok && scope.WorkspaceID != "" {
		return workspace.Resolved{Key: key, Value: v, Level: workspace.LevelWorkspace}, nil
	}
	return workspace.Resolved{Key: key}, nil
}

// setDefaultLifecycle sets delivery.default_lifecycle for a Workspace or Project.
func (s *store) setDefaultLifecycle(resourceID, lifecycleID string) {
	if s.settings[delivery.SettingDefaultLifecycle] == nil {
		s.settings[delivery.SettingDefaultLifecycle] = map[string]json.RawMessage{}
	}
	s.settings[delivery.SettingDefaultLifecycle][resourceID] = json.RawMessage(`"` + lifecycleID + `"`)
}

func (s *store) IsActiveTeam(_ context.Context, _, teamID string) (bool, error) {
	return s.teams[teamID], nil
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

func (s *store) lastAudit() audit.Event {
	return s.audit[len(s.audit)-1]
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's
// resource ID. outcome, when set, replaces the allow.
type authorizer struct {
	asked   []authorization.Request
	deny    func(authorization.Request) bool
	outcome authorization.Outcome
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}
	if a.outcome != "" {
		d.Outcome = a.outcome
	}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
	}
	return d, nil
}

// The fixture.

const (
	wsActive   = "wsp_01JZ0000000000000000000001"
	wsOther    = "wsp_01JZ0000000000000000000002"
	prjActive  = "prj_01JZ0000000000000000000001"
	prjOther   = "prj_01JZ0000000000000000000002"
	teamOwner  = "tem_owner"
	teamOther  = "tem_other"
	teamGone   = "tem_gone"
	missingApp = "app_01JZ0000000000000000000099"
)

var ada = identity.Actor{PrincipalID: "prn_ada", Kind: identity.ActorUser}

// world is a store with two Workspaces, each with a Project owned by
// teamOwner, and the ApplicationService over it.
type world struct {
	*store
	authz *authorizer
	svc   delivery.ApplicationService
}

func newWorld(t *testing.T) *world {
	t.Helper()
	s := newStore()
	for _, ws := range []struct{ id, slug, projectID, projectSlug string }{
		{wsActive, "platform", prjActive, "checkout"},
		{wsOther, "data", prjOther, "etl"},
	} {
		wsID, prjID := ws.id, ws.projectID
		s.workspaces[wsID] = must(workspace.NewWorkspace(workspace.NewWorkspaceProps{OrganizationID: org, Slug: ws.slug, DisplayName: ws.slug}, &wsID))
		s.projects[prjID] = must(workspace.NewProject(workspace.NewProjectProps{
			OrganizationID: org, WorkspaceID: wsID, OwnerTeamID: teamOwner, Slug: ws.projectSlug, DisplayName: ws.projectSlug,
		}, &prjID))
	}
	s.teams = map[string]bool{teamOwner: true, teamOther: true, teamGone: false}
	authz := &authorizer{}
	svc, err := NewApplicationService(s.options(authz)...)
	require.NoError(t, err)
	return &world{store: s, authz: authz, svc: svc}
}

func (s *store) options(authz authorization.Authorizer) []ApplicationServiceOption {
	return []ApplicationServiceOption{
		WithApplicationRepo(s), WithLifecycleLookup(s), WithRegistryLookup(s), WithTargetLookup(s),
		WithEnvironmentLookup(s), WithWorkspaces(s), WithSettingResolver(s), WithTeamDirectory(s), WithAuthorizer(authz),
		WithTransactor(s), WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	}
}

// reset forgets what was recorded so far.
func (w *world) reset() {
	w.audit, w.events, w.authz.asked = nil, nil, nil
	w.authz.deny, w.authz.outcome = nil, ""
}

// Records other packages own are stored directly.

func (w *world) lifecycle(t *testing.T, workspaceID, slug string, archived bool) *delivery.Lifecycle {
	t.Helper()
	l := must(delivery.NewLifecycle(delivery.NewLifecycleProps{OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug}, nil))
	if archived {
		l.Archive()
	}
	w.setLifecycle(l)
	return l
}

func (w *world) setLifecycle(l *delivery.Lifecycle) {
	l.PullEvents()
	w.lifecycles[l.ID()] = l.Snapshot()
}

func (w *world) registry(t *testing.T, workspaceID, slug string, archived bool) *delivery.Registry {
	t.Helper()
	r := must(delivery.NewRegistry(delivery.NewRegistryProps{OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Kind: delivery.RegistryOCI, URL: "https://ghcr.io"}, nil))
	if archived {
		r.Archive()
	}
	r.PullEvents()
	w.registries[r.ID()] = r.Snapshot()
	return r
}

func (w *world) env(t *testing.T, workspaceID, slug string) *delivery.Environment {
	t.Helper()
	e := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Tier: delivery.TierStaging}, nil))
	e.PullEvents()
	w.environments[e.ID()] = e.Snapshot()
	return e
}

func (w *world) target(t *testing.T, e *delivery.Environment, slug string) *delivery.Target {
	t.Helper()
	tg := must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: e.ID(), Slug: slug, DisplayName: slug,
		Runtime: delivery.RuntimeKubernetes, Locator: map[string]string{delivery.LocatorCluster: "prod-1", delivery.LocatorNamespace: slug},
	}, nil))
	tg.PullEvents()
	w.targets[tg.ID()] = tg.Snapshot()
	return tg
}

// Records this package owns are made through the service.

func (w *world) app(t *testing.T, projectID, slug string, l *delivery.Lifecycle) (*delivery.Application, *delivery.Channel) {
	t.Helper()
	a, c, err := w.svc.CreateApplication(context.Background(), ada, delivery.CreateApplicationInput{
		OrganizationID: org, ProjectID: projectID, Slug: slug, DisplayName: slug, DefaultLifecycleID: l.ID(),
	})
	require.NoError(t, err)
	return a, c
}

func (w *world) artifact(t *testing.T, a *delivery.Application, name string) *delivery.Artifact {
	t.Helper()
	art, err := w.svc.CreateArtifact(context.Background(), ada, delivery.CreateArtifactInput{
		OrganizationID: org, ApplicationID: a.ID(), Name: name, ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: "ghcr.io/acme/" + name,
	})
	require.NoError(t, err)
	return art
}

func (w *world) channel(t *testing.T, a *delivery.Application, l *delivery.Lifecycle, slug string, rules ...delivery.ChannelRuleProps) *delivery.Channel {
	t.Helper()
	c, err := w.svc.CreateChannel(context.Background(), ada, delivery.CreateChannelInput{
		OrganizationID: org, ApplicationID: a.ID(), LifecycleID: l.ID(), Slug: slug, DisplayName: slug, Rules: rules,
	})
	require.NoError(t, err)
	return c
}

func (w *world) placement(t *testing.T, a *delivery.Application, tg *delivery.Target, config *delivery.ApplyConfig) *delivery.Placement {
	t.Helper()
	p, err := w.svc.CreatePlacement(context.Background(), ada, delivery.CreatePlacementInput{
		OrganizationID: org, ApplicationID: a.ID(), TargetID: tg.ID(), ApplyConfig: config,
	})
	require.NoError(t, err)
	return p
}

func (w *world) archiveApp(t *testing.T, a *delivery.Application) {
	t.Helper()
	_, err := w.svc.ArchiveApplication(context.Background(), ada, org, a.ID())
	require.NoError(t, err)
}

func (w *world) archiveProject(id string) {
	w.projects[id].Archive()
}

// gitRule is a valid rule, scoped to an artifact when artifactID is set.
func gitRule(artifactID *string) delivery.ChannelRuleProps {
	return delivery.ChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: artifactID, Config: []byte(`{"patterns":["refs/heads/main"]}`)}
}

func ptr[T any](v T) *T { return &v }

var errBoom = errors.New("boom")
