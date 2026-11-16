// Package applications implements delivery.ApplicationService:
// Applications, their artifacts, their Channels, their placements on
// Targets, and their HTTP API. The domain types stay in delivery; this
// package orchestrates them.
//
// NewApplicationService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller is checked
// the same way. The service below assumes that check has happened and
// records the decision that allowed each change in its audit record.
package applications

import (
	"context"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// LifecycleLookup reads the Lifecycle a Channel follows, locked so it cannot
// be archived while the Channel starts following it.
// delivery.LifecycleRepository provides it.
type LifecycleLookup interface {
	GetLifecycleForUpdate(ctx context.Context, organizationID, lifecycleID string) (*delivery.Lifecycle, error)
}

// RegistryLookup reads the Registry an artifact is looked up in.
// delivery.RegistryRepository provides it.
type RegistryLookup interface {
	GetRegistry(ctx context.Context, organizationID, registryID string) (*delivery.Registry, error)
}

// TargetLookup reads the Target an Application is placed on.
// delivery.TargetRepository provides it.
type TargetLookup interface {
	GetTarget(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
}

// EnvironmentLookup reads a Target's Environment, which says its Workspace,
// and a Workspace's Environments for a grid's columns.
// delivery.EnvironmentRepository provides it.
type EnvironmentLookup interface {
	GetEnvironment(ctx context.Context, organizationID, environmentID string) (*delivery.Environment, error)
	ListActiveEnvironments(ctx context.Context, organizationID, workspaceID string) ([]*delivery.Environment, error)
}

// VariableUsage counts the variables with a value scoped to a Channel, which
// keep it from being archived. delivery.VariableRepository provides it.
type VariableUsage interface {
	CountActiveVariablesWithChannel(ctx context.Context, organizationID, channelID string) (int, error)
}

// VariableSet reads the variables a Project and its Applications deploy
// with, for the checks a restore runs before an Application's variables come
// back. delivery.VariableRepository provides it.
type VariableSet interface {
	ListVariables(ctx context.Context, organizationID string, owner delivery.VariableOwner) ([]*delivery.Variable, error)
	ListVariablesInProject(ctx context.Context, organizationID, projectID string) ([]*delivery.Variable, map[string][]*delivery.Variable, error)
}

// ProjectLookup reads the Projects Applications belong to, for labelling a
// "what runs where" read with Project names. workspace.NewLookups provides
// it.
type ProjectLookup interface {
	LookupProjects(ctx context.Context, organizationID string, projectIDs []string) ([]*workspace.Project, error)
}

type ApplicationServiceOption func(*options)

type options struct {
	applications delivery.ApplicationRepository
	lifecycles   LifecycleLookup
	registries   RegistryLookup
	targets      TargetLookup
	environments EnvironmentLookup
	projects     ProjectLookup
	variables    VariableUsage
	variableSet  VariableSet
	scopes       *delivery.VariableScopeChecks
	workspaces   delivery.Workspaces
	settings     workspace.SettingResolver
	teams        team.Directory
	locker       transaction.Locker
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithApplicationRepo(r delivery.ApplicationRepository) ApplicationServiceOption {
	return func(o *options) { o.applications = r }
}
func WithLifecycleLookup(l LifecycleLookup) ApplicationServiceOption {
	return func(o *options) { o.lifecycles = l }
}
func WithRegistryLookup(r RegistryLookup) ApplicationServiceOption {
	return func(o *options) { o.registries = r }
}
func WithTargetLookup(t TargetLookup) ApplicationServiceOption {
	return func(o *options) { o.targets = t }
}
func WithEnvironmentLookup(e EnvironmentLookup) ApplicationServiceOption {
	return func(o *options) { o.environments = e }
}

// WithProjectLookup labels a "what runs where" read with Project names.
func WithVariableUsage(v VariableUsage) ApplicationServiceOption {
	return func(o *options) { o.variables = v }
}

// WithVariableSet makes a restore check the variables the Application brings
// back against its Project's, as a save does.
func WithVariableSet(v VariableSet) ApplicationServiceOption {
	return func(o *options) { o.variableSet = v }
}

// WithVariableScopes checks what a restored Application's variables are
// scoped to, under the rows' locks, as a save does.
func WithVariableScopes(c *delivery.VariableScopeChecks) ApplicationServiceOption {
	return func(o *options) { o.scopes = c }
}

// WithLocker holds a Project's variables while a restore checks them, as a
// save does.
func WithLocker(l transaction.Locker) ApplicationServiceOption {
	return func(o *options) { o.locker = l }
}
func WithProjectLookup(p ProjectLookup) ApplicationServiceOption {
	return func(o *options) { o.projects = p }
}
func WithWorkspaces(w delivery.Workspaces) ApplicationServiceOption {
	return func(o *options) { o.workspaces = w }
}

// WithSettingResolver resolves delivery.default_lifecycle for a new
// Application whose creator names no Lifecycle (workspace.NewSettingResolver,
// over the Definitions delivery.RegisterSettings registered in).
func WithSettingResolver(r workspace.SettingResolver) ApplicationServiceOption {
	return func(o *options) { o.settings = r }
}

// WithTeamDirectory checks the Teams that own Applications.
func WithTeamDirectory(t team.Directory) ApplicationServiceOption {
	return func(o *options) { o.teams = t }
}
func WithAuthorizer(a authorization.Authorizer) ApplicationServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repositories, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) ApplicationServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ApplicationServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ApplicationServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewApplicationService returns the ApplicationService, authorizing every
// operation, or an error naming a missing dependency.
func NewApplicationService(opts ...ApplicationServiceOption) (delivery.ApplicationService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"ApplicationRepo", o.applications != nil},
		{"LifecycleLookup", o.lifecycles != nil},
		{"RegistryLookup", o.registries != nil},
		{"TargetLookup", o.targets != nil},
		{"EnvironmentLookup", o.environments != nil},
		{"ProjectLookup", o.projects != nil},
		{"VariableUsage", o.variables != nil},
		{"VariableSet", o.variableSet != nil},
		{"VariableScopes", o.scopes != nil},
		{"Workspaces", o.workspaces != nil},
		{"SettingResolver", o.settings != nil},
		{"TeamDirectory", o.teams != nil},
		{"Locker", o.locker != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("application service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("application service: %w", err)
	}
	s := &service{
		applications: o.applications,
		lifecycles:   o.lifecycles,
		registries:   o.registries,
		targets:      o.targets,
		environments: o.environments,
		projects:     o.projects,
		variables:    o.variables,
		variableSet:  o.variableSet,
		scopes:       o.scopes,
		workspaces:   o.workspaces,
		settings:     o.settings,
		teams:        o.teams,
		locker:       o.locker,
		w:            o.writer,
	}
	return &authorized{next: s, authz: o.authz, applications: o.applications}, nil
}

// service carries out Application operations once they are authorized. It
// checks rules that span records, changes objects only through their
// methods, and commits each change with its events and audit record.
type service struct {
	applications delivery.ApplicationRepository
	lifecycles   LifecycleLookup
	registries   RegistryLookup
	targets      TargetLookup
	environments EnvironmentLookup
	projects     ProjectLookup
	variables    VariableUsage
	variableSet  VariableSet
	scopes       *delivery.VariableScopeChecks
	workspaces   delivery.Workspaces
	settings     workspace.SettingResolver
	teams        team.Directory
	locker       transaction.Locker
	w            change.Writer
}

var _ delivery.ApplicationService = (*service)(nil)

// CreateApplication creates an Application in an active Project, with its
// default Channel. The Application's Workspace is copied from the Project.
// The default Channel follows the Lifecycle the input names, or else the
// one delivery.default_lifecycle resolves to for the Project.
func (s *service) CreateApplication(ctx context.Context, actor identity.Actor, in delivery.CreateApplicationInput) (*delivery.Application, *delivery.Channel, error) {
	// NewApplication needs the Project's Workspace, so it runs in the
	// transaction; this refuses bad input before it starts.
	if err := in.Validate(); err != nil {
		return nil, nil, err
	}
	var app *delivery.Application
	var ch *delivery.Channel
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := delivery.ActiveProject(ctx, s.workspaces, in.OrganizationID, in.ProjectID)
		if err != nil {
			return err
		}
		owner := in.OwnerTeamID
		if owner == "" {
			owner = p.OwnerTeamID()
		}
		if err := s.checkTeam(ctx, in.OrganizationID, owner); err != nil {
			return err
		}
		app, err = delivery.NewApplication(delivery.NewApplicationProps{
			OrganizationID:   in.OrganizationID,
			WorkspaceID:      p.WorkspaceID(),
			ProjectID:        p.ID(),
			OwnerTeamID:      owner,
			Slug:             in.Slug,
			DisplayName:      in.DisplayName,
			SourceID:         &in.SourceID,
			WorkloadSelector: in.WorkloadSelector,
		}, nil)
		if err != nil {
			return err
		}
		lifecycleID, err := s.defaultLifecycleID(ctx, p, in.DefaultLifecycleID)
		if err != nil {
			return err
		}
		l, err := s.lifecycleFor(ctx, in.OrganizationID, app.WorkspaceID(), lifecycleID)
		if err != nil {
			return err
		}
		ch, err = delivery.NewChannel(delivery.NewChannelProps{
			OrganizationID: in.OrganizationID,
			ApplicationID:  app.ID(),
			LifecycleID:    l.ID(),
			Slug:           delivery.DefaultChannelSlug,
			DisplayName:    delivery.DefaultChannelName,
			IsDefault:      true,
		}, nil)
		if err != nil {
			return err
		}
		if err := s.applications.CreateApplication(ctx, app); err != nil {
			return fmt.Errorf("create application: %w", err)
		}
		if err := s.applications.CreateChannel(ctx, ch); err != nil {
			return fmt.Errorf("create default channel: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateApplication, app.ID(), app); err != nil {
			return err
		}
		if err := s.w.Events(ctx, delivery.AggregateChannel, ch.ID(), ch); err != nil {
			return err
		}
		if err := s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventApplicationCreated, OrganizationID: in.OrganizationID, ResourceID: app.ID(), Actor: actor, After: app.Snapshot()}); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventChannelCreated, OrganizationID: in.OrganizationID, ResourceID: ch.ID(), Actor: actor, After: ch.Snapshot()})
	})
	if err != nil {
		return nil, nil, err
	}
	return app, ch, nil
}

// defaultLifecycleID returns the Lifecycle a new Application's default
// Channel follows: the one named, or the delivery.default_lifecycle setting
// resolved for its Project.
func (s *service) defaultLifecycleID(ctx context.Context, p *workspace.Project, named string) (string, error) {
	if named != "" {
		return named, nil
	}
	resolved, err := s.settings.ResolveSetting(ctx, workspace.ProjectScope(p.OrganizationID(), p.WorkspaceID(), p.ID()), delivery.SettingDefaultLifecycle)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", delivery.SettingDefaultLifecycle, err)
	}
	lifecycleID, err := delivery.DefaultLifecycleID(resolved)
	if err != nil {
		return "", err
	}
	if lifecycleID == "" {
		return "", delivery.NewErrInvalidInput(fmt.Sprintf(
			"an application needs the lifecycle its default channel follows: name one, or set %s for the project or its workspace", delivery.SettingDefaultLifecycle))
	}
	return lifecycleID, nil
}

func (s *service) checkTeam(ctx context.Context, organizationID, teamID string) error {
	if teamID == "" {
		return delivery.NewErrInvalidInput("an application needs an owning team")
	}
	ok, err := s.teams.IsActiveTeam(ctx, organizationID, teamID)
	if err != nil {
		return fmt.Errorf("check team %s: %w", teamID, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s", delivery.ErrInactiveTeam, teamID)
	}
	return nil
}

// lifecycleFor returns an active Lifecycle in the Workspace, locked so it
// cannot be archived while a Channel starts following it.
func (s *service) lifecycleFor(ctx context.Context, organizationID, workspaceID, lifecycleID string) (*delivery.Lifecycle, error) {
	l, err := s.lifecycles.GetLifecycleForUpdate(ctx, organizationID, lifecycleID)
	if err != nil {
		return nil, err
	}
	if l.WorkspaceID() != workspaceID {
		return nil, fmt.Errorf("%w: lifecycle %s is not in workspace %s", delivery.ErrWorkspaceMismatch, lifecycleID, workspaceID)
	}
	if l.Archived() {
		return nil, fmt.Errorf("%w: lifecycle %s", delivery.ErrArchived, lifecycleID)
	}
	return l, nil
}

// registryFor returns an active Registry in the Workspace.
func (s *service) registryFor(ctx context.Context, organizationID, workspaceID, registryID string) (*delivery.Registry, error) {
	r, err := s.registries.GetRegistry(ctx, organizationID, registryID)
	if err != nil {
		return nil, err
	}
	if r.WorkspaceID() != workspaceID {
		return nil, fmt.Errorf("%w: registry %s is not in workspace %s", delivery.ErrWorkspaceMismatch, registryID, workspaceID)
	}
	if r.Archived() {
		return nil, fmt.Errorf("%w: registry %s", delivery.ErrArchived, registryID)
	}
	return r, nil
}

// activeApplication returns an active Application.
func (s *service) activeApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error) {
	a, err := s.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return nil, err
	}
	if a.Archived() {
		return nil, fmt.Errorf("%w: application %s", delivery.ErrArchived, applicationID)
	}
	return a, nil
}

func (s *service) GetApplication(ctx context.Context, _ identity.Actor, organizationID, applicationID string) (*delivery.Application, error) {
	return s.applications.GetApplication(ctx, organizationID, applicationID)
}

func (s *service) ListApplications(ctx context.Context, _ identity.Actor, query delivery.ListApplicationsQuery) (*pagination.Result[*delivery.Application], error) {
	return s.applications.ListApplications(ctx, query)
}

// changeApplication applies a change to an Application of an active Project.
// prelock, when set, runs in the transaction before the Application is read:
// a restore holds the Project's variables, whose name is known without
// reading the Application, before it holds the Application itself.
func (s *service) changeApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID, event string, prelock func(context.Context) error, apply func(context.Context, *delivery.Application) error) (*delivery.Application, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Application]{
		AggregateType: delivery.AggregateApplication,
		AggregateID:   func(a *delivery.Application) string { return a.ID() },
		Load: func(ctx context.Context) (*delivery.Application, error) {
			if prelock != nil {
				if err := prelock(ctx); err != nil {
					return nil, err
				}
			}
			return s.applications.GetApplicationForUpdate(ctx, organizationID, applicationID)
		},
		Apply:    apply,
		Save:     s.applications.UpdateApplication,
		Snapshot: func(a *delivery.Application) any { return a.Snapshot() },
		Event: func(a *delivery.Application) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: a.ID()}
		},
	})
}

// UpdateApplication changes an active Application's slug, name, or workload selector.
func (s *service) UpdateApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string, props delivery.UpdateApplicationProps) (*delivery.Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, delivery.EventApplicationUpdated, nil,
		func(_ context.Context, a *delivery.Application) error { return a.Update(props) })
}

// LinkApplicationSource records the Source the Application is built from,
// or removes it when sourceID is empty.
func (s *service) LinkApplicationSource(ctx context.Context, actor identity.Actor, organizationID, applicationID, sourceID string) (*delivery.Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, delivery.EventApplicationUpdated, nil,
		func(_ context.Context, a *delivery.Application) error {
			if sourceID == "" {
				return a.UnlinkSource()
			}
			return a.LinkSource(sourceID)
		})
}

// TransferApplication hands an Application to another active Team.
func (s *service) TransferApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID, ownerTeamID string) (*delivery.Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, delivery.EventApplicationOwnerChanged, nil,
		func(ctx context.Context, a *delivery.Application) error {
			if err := s.checkTeam(ctx, organizationID, ownerTeamID); err != nil {
				return err
			}
			return a.TransferTo(ownerTeamID)
		})
}

// ArchiveApplication archives an Application. Its Channels, artifacts, and
// placements stay as they are, so restoring it brings them back; until then
// they cannot be changed. Archiving an archived one does nothing.
func (s *service) ArchiveApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*delivery.Application, error) {
	return s.changeApplication(ctx, actor, organizationID, applicationID, delivery.EventApplicationArchived, nil,
		func(_ context.Context, a *delivery.Application) error {
			if a.Archived() {
				return change.ErrUnchanged
			}
			a.Archive()
			return nil
		})
}

// RestoreApplication restores an archived Application in an active Project
// whose active Channels' Lifecycles are all active and whose variables can
// still deploy. Its variables go with it while it is archived, so they are
// checked again here, under the Project's variables lock and as a save would
// check them: a shadow or an overlapping values path that the Project stopped
// allowing, or a scope whose record was archived meanwhile, refuses the
// restore rather than surfacing when a Release is created.
func (s *service) RestoreApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*delivery.Application, error) {
	// Read to name the Project whose variables are held before the
	// Application's own row: a stored Application's Project never changes.
	found, err := s.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return nil, err
	}
	projectID := found.ProjectID()
	return s.changeApplication(ctx, actor, organizationID, applicationID, delivery.EventApplicationRestored,
		func(ctx context.Context) error {
			return s.locker.Lock(ctx, delivery.VariablesLock(projectID))
		},
		func(ctx context.Context, a *delivery.Application) error {
			if !a.Archived() {
				return change.ErrUnchanged
			}
			if _, err := delivery.ActiveProject(ctx, s.workspaces, organizationID, a.ProjectID()); err != nil {
				return err
			}
			// Lifecycles can be archived while the Application is, so its
			// Channels' Lifecycles must be restored first.
			channels, err := s.applications.ListChannels(ctx, organizationID, a.ID(), false)
			if err != nil {
				return fmt.Errorf("list channels: %w", err)
			}
			for _, c := range channels {
				if _, err := s.lifecycleFor(ctx, organizationID, a.WorkspaceID(), c.LifecycleID()); err != nil {
					return fmt.Errorf("channel %s: %w", c.ID(), err)
				}
			}
			if err := s.checkVariableSet(ctx, a); err != nil {
				return err
			}
			a.Restore()
			return nil
		})
}

// checkVariableSet checks the variables an archived Application brings back
// against its Project's: each may still be deployed with them, and what they
// are scoped to, and the Secret Stores they are kept in, may still be used.
// The caller holds the Project's variables lock.
func (s *service) checkVariableSet(ctx context.Context, a *delivery.Application) error {
	project, _, err := s.variableSet.ListVariablesInProject(ctx, a.OrganizationID(), a.ProjectID())
	if err != nil {
		return fmt.Errorf("list the project's variables: %w", err)
	}
	own, err := s.variableSet.ListVariables(ctx, a.OrganizationID(), delivery.VariableOwner{ApplicationID: a.ID()})
	if err != nil {
		return fmt.Errorf("list the application's variables: %w", err)
	}
	if len(own) > delivery.MaxVariablesPerOwner {
		return delivery.NewErrInvalidInput(fmt.Sprintf("an application can have at most %d variables", delivery.MaxVariablesPerOwner))
	}
	if err := delivery.CheckDeployable(project, own); err != nil {
		return err
	}
	return s.scopes.Check(ctx, a.WorkspaceID(), own...)
}
