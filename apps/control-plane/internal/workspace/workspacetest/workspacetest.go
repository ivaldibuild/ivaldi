// Package workspacetest provides in-memory fakes of the workspace
// capability's dependencies, for the tests of its service sub-packages.
//
// A Store is the Repository, with the uniqueness rules the migrations
// enforce, and also the Transactor, the IAM resource registrar, the Team
// directory, and the setting lock source; Store.Audit and Store.Outbox are
// its recorders. Like the PostgreSQL repository, it stores snapshots and
// returns rebuilt objects, so callers never share an object with it. A
// transaction whose function fails restores everything it stores, as a
// rolled-back database transaction would.
package workspacetest

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
)

// ResourceCall is a call to the IAM resource registrar: "register",
// "archive", or "restore". Archive and restore set only the ID and Organization.
type ResourceCall struct {
	Op string
	iam.Resource
}

// state is what a transaction restores when it fails.
type state struct {
	Workspaces map[string]workspace.WorkspaceSnapshot
	Projects   map[string]workspace.ProjectSnapshot
	Settings   map[string]workspace.SettingSnapshot
	Resources  []ResourceCall
	AuditLog   []audit.Event
	Events     []events.Envelope
}

func (s state) clone() state {
	return state{
		Workspaces: maps.Clone(s.Workspaces),
		Projects:   maps.Clone(s.Projects),
		Settings:   maps.Clone(s.Settings),
		Resources:  slices.Clone(s.Resources),
		AuditLog:   slices.Clone(s.AuditLog),
		Events:     slices.Clone(s.Events),
	}
}

// Store is an in-memory workspace.Repository and the rest of a service's
// dependencies. It is not safe for concurrent use.
type Store struct {
	state
	// Teams holds the active Teams, keyed "organization/team".
	Teams map[string]bool
	// Locks holds the locked setting keys, keyed "organization/workspace",
	// with an empty workspace for the Organization's own locks.
	Locks map[string]map[string]workspace.Level
	// AuditErr, when set, fails every audit record.
	AuditErr error
}

var (
	_ workspace.Repository  = (*Store)(nil)
	_ workspace.LockSource  = (*Store)(nil)
	_ iam.ResourceRegistrar = (*Store)(nil)
)

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		state: state{
			Workspaces: map[string]workspace.WorkspaceSnapshot{},
			Projects:   map[string]workspace.ProjectSnapshot{},
			Settings:   map[string]workspace.SettingSnapshot{},
		},
		Teams: map[string]bool{},
		Locks: map[string]map[string]workspace.Level{},
	}
}

// WithinTx runs fn, and restores the stored state if it fails.
func (s *Store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := s.state.clone()
	if err := fn(ctx); err != nil {
		s.state = saved
		return err
	}
	return nil
}

// Reset forgets the resource calls, audit records, and events recorded so far.
func (s *Store) Reset() {
	s.Resources, s.AuditLog, s.Events = nil, nil, nil
}

// Workspaces.

func (s *Store) checkWorkspace(w workspace.WorkspaceSnapshot) error {
	for _, o := range s.Workspaces {
		if o.ID == w.ID || o.OrganizationID != w.OrganizationID {
			continue
		}
		if o.ArchivedAt == nil && w.ArchivedAt == nil && o.Slug == w.Slug {
			return workspace.ErrSlugTaken
		}
		if o.IsDefault && w.IsDefault {
			return workspace.ErrConflict
		}
	}
	if w.IsDefault && w.ArchivedAt != nil {
		return workspace.ErrDefaultWorkspace
	}
	return nil
}

func (s *Store) CreateWorkspace(_ context.Context, w *workspace.Workspace) error {
	d := w.Snapshot()
	if err := s.checkWorkspace(d); err != nil {
		return err
	}
	s.Workspaces[d.ID] = d
	return nil
}

func (s *Store) GetWorkspace(_ context.Context, organizationID, workspaceID string) (*workspace.Workspace, error) {
	d, ok := s.Workspaces[workspaceID]
	if !ok || d.OrganizationID != organizationID {
		return nil, workspace.ErrNotFound
	}
	return RebuildWorkspace(d), nil
}

func (s *Store) GetWorkspaceForUpdate(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error) {
	return s.GetWorkspace(ctx, organizationID, workspaceID)
}

func (s *Store) GetDefaultWorkspace(_ context.Context, organizationID string) (*workspace.Workspace, error) {
	for _, d := range s.Workspaces {
		if d.OrganizationID == organizationID && d.IsDefault {
			return RebuildWorkspace(d), nil
		}
	}
	return nil, workspace.ErrNotFound
}

func (s *Store) ListWorkspaces(_ context.Context, organizationID string, includeArchived bool) ([]*workspace.Workspace, error) {
	var out []*workspace.Workspace
	for _, d := range s.Workspaces {
		if d.OrganizationID == organizationID && (includeArchived || d.ArchivedAt == nil) {
			out = append(out, RebuildWorkspace(d))
		}
	}
	return out, nil
}

func (s *Store) UpdateWorkspace(_ context.Context, w *workspace.Workspace) error {
	d := w.Snapshot()
	if o, ok := s.Workspaces[d.ID]; !ok || o.OrganizationID != d.OrganizationID {
		return workspace.ErrNotFound
	}
	if err := s.checkWorkspace(d); err != nil {
		return err
	}
	s.Workspaces[d.ID] = d
	return nil
}

func (s *Store) CountActiveProjects(_ context.Context, organizationID, workspaceID string) (int, error) {
	n := 0
	for _, d := range s.Projects {
		if d.OrganizationID == organizationID && d.WorkspaceID == workspaceID && d.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

func (s *Store) CountActiveProjectsOwnedBy(_ context.Context, organizationID, teamID string) (int, error) {
	n := 0
	for _, d := range s.Projects {
		if d.OrganizationID == organizationID && d.OwnerTeamID == teamID && d.ArchivedAt == nil {
			n++
		}
	}
	return n, nil
}

// Projects.

func (s *Store) checkProject(p workspace.ProjectSnapshot) error {
	for _, o := range s.Projects {
		if o.ID != p.ID && o.OrganizationID == p.OrganizationID && o.WorkspaceID == p.WorkspaceID &&
			o.ArchivedAt == nil && p.ArchivedAt == nil && o.Slug == p.Slug {
			return workspace.ErrSlugTaken
		}
	}
	return nil
}

func (s *Store) CreateProject(_ context.Context, p *workspace.Project) error {
	d := p.Snapshot()
	if err := s.checkProject(d); err != nil {
		return err
	}
	s.Projects[d.ID] = d
	return nil
}

func (s *Store) GetProject(_ context.Context, organizationID, projectID string) (*workspace.Project, error) {
	d, ok := s.Projects[projectID]
	if !ok || d.OrganizationID != organizationID {
		return nil, workspace.ErrNotFound
	}
	return RebuildProject(d), nil
}

func (s *Store) ListProjects(_ context.Context, organizationID, workspaceID string, includeArchived bool) ([]*workspace.Project, error) {
	var out []*workspace.Project
	for _, d := range s.Projects {
		if d.OrganizationID == organizationID && d.WorkspaceID == workspaceID && (includeArchived || d.ArchivedAt == nil) {
			out = append(out, RebuildProject(d))
		}
	}
	return out, nil
}

func (s *Store) UpdateProject(_ context.Context, p *workspace.Project) error {
	d := p.Snapshot()
	if o, ok := s.Projects[d.ID]; !ok || o.OrganizationID != d.OrganizationID {
		return workspace.ErrNotFound
	}
	if err := s.checkProject(d); err != nil {
		return err
	}
	s.Projects[d.ID] = d
	return nil
}

// Settings.

func settingKey(s workspace.Scope, key string) string {
	return s.OrganizationID + "/" + s.WorkspaceID + "/" + s.ProjectID + "/" + key
}

func (s *Store) GetSetting(_ context.Context, scope workspace.Scope, key string) (*workspace.Setting, error) {
	d, ok := s.Settings[settingKey(scope, key)]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return RebuildSetting(d), nil
}

func (s *Store) ListSettings(_ context.Context, scope workspace.Scope) ([]*workspace.Setting, error) {
	var out []*workspace.Setting
	for _, d := range s.Settings {
		if d.Scope == scope {
			out = append(out, RebuildSetting(d))
		}
	}
	return out, nil
}

func (s *Store) SettingsForResolution(_ context.Context, scope workspace.Scope, key string) ([]*workspace.Setting, error) {
	levels := []workspace.Scope{workspace.OrganizationScope(scope.OrganizationID)}
	if scope.WorkspaceID != "" {
		levels = append(levels, workspace.WorkspaceScope(scope.OrganizationID, scope.WorkspaceID))
	}
	if scope.ProjectID != "" {
		levels = append(levels, scope)
	}
	var out []*workspace.Setting
	for _, l := range levels {
		if d, ok := s.Settings[settingKey(l, key)]; ok {
			out = append(out, RebuildSetting(d))
		}
	}
	return out, nil
}

func (s *Store) InsertSetting(_ context.Context, setting *workspace.Setting) error {
	d := setting.Snapshot()
	k := settingKey(d.Scope, d.Key)
	if _, ok := s.Settings[k]; ok {
		return workspace.ErrConflict
	}
	s.Settings[k] = d
	return nil
}

func (s *Store) UpdateSetting(_ context.Context, setting *workspace.Setting, expectedVersion int64) error {
	d := setting.Snapshot()
	k := settingKey(d.Scope, d.Key)
	if cur, ok := s.Settings[k]; !ok || cur.Version != expectedVersion {
		return workspace.ErrConflict
	}
	s.Settings[k] = d
	return nil
}

func (s *Store) DeleteSetting(_ context.Context, scope workspace.Scope, key string, expectedVersion int64) error {
	k := settingKey(scope, key)
	if cur, ok := s.Settings[k]; !ok || cur.Version != expectedVersion {
		return workspace.ErrConflict
	}
	delete(s.Settings, k)
	return nil
}

// LockedKeys reports the Organization's locks and the Workspace's, with an
// Organization lock winning.
func (s *Store) LockedKeys(_ context.Context, organizationID, workspaceID string) (map[string]workspace.Level, error) {
	out := map[string]workspace.Level{}
	if workspaceID != "" {
		maps.Copy(out, s.Locks[organizationID+"/"+workspaceID])
	}
	maps.Copy(out, s.Locks[organizationID+"/"])
	return out, nil
}

// IAM resources and Teams.

func (s *Store) RegisterResource(_ context.Context, r iam.Resource) error {
	s.Resources = append(s.Resources, ResourceCall{"register", r})
	return nil
}

func (s *Store) ArchiveResource(_ context.Context, organizationID, resourceID string) error {
	s.Resources = append(s.Resources, ResourceCall{"archive", iam.Resource{ID: resourceID, OrganizationID: organizationID}})
	return nil
}

func (s *Store) RestoreResource(_ context.Context, organizationID, resourceID string) error {
	s.Resources = append(s.Resources, ResourceCall{"restore", iam.Resource{ID: resourceID, OrganizationID: organizationID}})
	return nil
}

// IsActiveTeam implements team.Directory.
func (s *Store) IsActiveTeam(_ context.Context, organizationID, teamID string) (bool, error) {
	return s.Teams[organizationID+"/"+teamID], nil
}

// Recorders.

// Audit returns the audit recorder that writes to the Store's AuditLog.
func (s *Store) Audit() audit.Recorder { return auditLog{s} }

// Outbox returns the outbox recorder that writes to the Store's Events.
func (s *Store) Outbox() outbox.Recorder { return outboxLog{s} }

type auditLog struct{ s *Store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	if a.s.AuditErr != nil {
		return a.s.AuditErr
	}
	a.s.AuditLog = append(a.s.AuditLog, e)
	return nil
}

type outboxLog struct{ s *Store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.Events = append(o.s.Events, envs...)
	return nil
}

// EventTypes returns the types of the events recorded, in order.
func (s *Store) EventTypes() []string {
	var out []string
	for _, e := range s.Events {
		out = append(out, e.Type)
	}
	return out
}

// AuditTypes returns the types of the audit records written, in order.
func (s *Store) AuditTypes() []string {
	var out []string
	for _, e := range s.AuditLog {
		out = append(out, e.Type)
	}
	return out
}

// LastAudit returns the last audit record written. It panics if there is none.
func (s *Store) LastAudit() audit.Event {
	return s.AuditLog[len(s.AuditLog)-1]
}

// LastResource returns the last resource call. It panics if there is none.
func (s *Store) LastResource() ResourceCall {
	return s.Resources[len(s.Resources)-1]
}

// Authorizer allows everything unless Decide is set, and records every
// request in Asked. Its default decision's ID is "dec-" and the request's
// resource ID.
type Authorizer struct {
	Asked  []authorization.Request
	Decide func(authorization.Request) authorization.Decision
}

func (a *Authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.Asked = append(a.Asked, req)
	if a.Decide != nil {
		return a.Decide(req), nil
	}
	return authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}, nil
}

// Last returns the last request. It panics if there is none.
func (a *Authorizer) Last() authorization.Request {
	return a.Asked[len(a.Asked)-1]
}

// Deny returns a Decide function that denies every request.
func Deny(authorization.Request) authorization.Decision {
	return authorization.Decision{ID: "dec-deny", Outcome: authorization.OutcomeDeny}
}

// Dependent is a workspace.Dependent that reports Deps, or fails with Err.
type Dependent struct {
	Deps []workspace.Dependency
	Err  error
}

func (d Dependent) ActiveDependents(context.Context, workspace.ArchiveTarget) ([]workspace.Dependency, error) {
	return d.Deps, d.Err
}

// Rebuilding, as the PostgreSQL repository does.

func must[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("workspacetest: rebuild: %v", err))
	}
	return v
}

// RebuildWorkspace returns the stored Workspace s describes.
func RebuildWorkspace(s workspace.WorkspaceSnapshot) *workspace.Workspace {
	return must(workspace.NewWorkspace(workspace.NewWorkspaceProps{
		OrganizationID:       s.OrganizationID,
		Slug:                 s.Slug,
		DisplayName:          s.DisplayName,
		Description:          s.Description,
		IsDefault:            s.IsDefault,
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		CreatedAt:            &s.CreatedAt,
		UpdatedAt:            &s.UpdatedAt,
		ArchivedAt:           s.ArchivedAt,
	}, &s.ID))
}

// RebuildProject returns the stored Project s describes.
func RebuildProject(s workspace.ProjectSnapshot) *workspace.Project {
	return must(workspace.NewProject(workspace.NewProjectProps{
		OrganizationID:       s.OrganizationID,
		WorkspaceID:          s.WorkspaceID,
		OwnerTeamID:          s.OwnerTeamID,
		Slug:                 s.Slug,
		DisplayName:          s.DisplayName,
		Description:          s.Description,
		CreatedByPrincipalID: s.CreatedByPrincipalID,
		CreatedAt:            &s.CreatedAt,
		UpdatedAt:            &s.UpdatedAt,
		ArchivedAt:           s.ArchivedAt,
	}, &s.ID))
}

// RebuildSetting returns the stored Setting s describes.
func RebuildSetting(s workspace.SettingSnapshot) *workspace.Setting {
	return must(workspace.NewSetting(workspace.NewSettingProps{
		Scope:                s.Scope,
		Key:                  s.Key,
		Value:                s.Value,
		UpdatedByPrincipalID: s.UpdatedByPrincipalID,
		PolicyDecisionID:     s.PolicyDecisionID,
		Version:              s.Version,
		CreatedAt:            &s.CreatedAt,
		UpdatedAt:            &s.UpdatedAt,
	}))
}

// Put stores a Workspace directly, bypassing any service, and forgets its events.
func (s *Store) Put(w *workspace.Workspace) *workspace.Workspace {
	w.PullEvents()
	s.Workspaces[w.ID()] = w.Snapshot()
	return w
}

// PutProject stores a Project directly, bypassing any service, and forgets its events.
func (s *Store) PutProject(p *workspace.Project) *workspace.Project {
	p.PullEvents()
	s.Projects[p.ID()] = p.Snapshot()
	return p
}
