package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Service orchestrates the delivery capability: it authorizes, checks rules
// that span several records or capabilities, calls domain methods to change
// state, and persists the result with its audit record. It never sets a
// domain object's state itself.
//
// Environments are IAM resources, so their permissions are checked on the
// Environment, or on its Workspace before it exists or while it is archived.
// Targets follow their Environment. Lifecycles and Registries are checked on
// their Workspace, and Applications and what belongs to them on their Project.
type Service struct {
	environments EnvironmentRepository
	lifecycles   LifecycleRepository
	targets      TargetRepository
	registries   RegistryRepository
	applications ApplicationRepository
	tx           transaction.Transactor
	authz        authorization.Authorizer
	resources    iam.ResourceRegistrar
	teams        team.Directory
	audit        audit.Recorder
	workspaces   Workspaces
}

var (
	_ EnvironmentService  = (*Service)(nil)
	_ LifecycleService    = (*Service)(nil)
	_ RegistryService     = (*Service)(nil)
	_ ApplicationService  = (*Service)(nil)
	_ workspace.Dependent = (*Service)(nil)
)

// Config holds the Service's collaborators. All are required.
type Config struct {
	Environments EnvironmentRepository
	Lifecycles   LifecycleRepository
	Targets      TargetRepository
	Registries   RegistryRepository
	Applications ApplicationRepository
	// Transactor makes a change, its IAM resource, and its audit record commit
	// together, so Resources and Audit must write through the context it passes.
	Transactor transaction.Transactor
	Authorizer authorization.Authorizer
	Resources  iam.ResourceRegistrar
	Teams      team.Directory
	Audit      audit.Recorder
	Workspaces Workspaces
}

// NewService returns a Service, or an error naming a missing collaborator.
func NewService(c Config) (*Service, error) {
	required := []struct {
		name string
		ok   bool
	}{
		{"Environments", c.Environments != nil},
		{"Lifecycles", c.Lifecycles != nil},
		{"Targets", c.Targets != nil},
		{"Registries", c.Registries != nil},
		{"Applications", c.Applications != nil},
		{"Transactor", c.Transactor != nil},
		{"Authorizer", c.Authorizer != nil},
		{"Resources", c.Resources != nil},
		{"Teams", c.Teams != nil},
		{"Audit", c.Audit != nil},
		{"Workspaces", c.Workspaces != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("delivery service: %s is required", r.name)
		}
	}
	return &Service{
		environments: c.Environments,
		lifecycles:   c.Lifecycles,
		targets:      c.Targets,
		registries:   c.Registries,
		applications: c.Applications,
		tx:           c.Transactor,
		authz:        c.Authorizer,
		resources:    c.Resources,
		teams:        c.Teams,
		audit:        c.Audit,
		workspaces:   c.Workspaces,
	}, nil
}

// authorize asks the single authorizer and turns anything but an allow into an error.
func (s *Service) authorize(ctx context.Context, req authorization.Request) (authorization.Decision, error) {
	return authorization.Require(ctx, s.authz, req)
}

// record writes an audit event, stamped with the decision that allowed it.
func (s *Service) record(ctx context.Context, d authorization.Decision, e audit.Event) error {
	e.DecisionID = d.ID
	e.ApprovedByPrincipalID = d.ApprovedByPrincipalID
	e.OccurredAt = clock.Now()
	if err := s.audit.Record(ctx, e); err != nil {
		return fmt.Errorf("record %s: %w", e.Type, err)
	}
	return nil
}

// change is one change to a stored object: load it, apply domain methods to
// it, save it, and record an event with its state before and after.
type change[T any] struct {
	// load reads the object. It runs once to authorize, and again inside
	// the transaction to apply the change to current state.
	load func(ctx context.Context) (T, error)
	// lock, if set, replaces load inside the transaction, to lock the row
	// against changes that check it.
	lock func(ctx context.Context) (T, error)
	// authorize checks the actor may make the change to the loaded object.
	authorize func(ctx context.Context, current T) (authorization.Decision, error)
	// apply changes the object through its methods, and checks rules that
	// span other records. Returning errUnchanged skips saving and the event.
	apply    func(ctx context.Context, current T) error
	save     func(ctx context.Context, current T) error
	snapshot func(T) any
	event    func(current T) audit.Event
}

// errUnchanged tells run that apply had nothing to do.
var errUnchanged = errors.New("unchanged")

// run authorizes before the transaction, so a slow policy decision holds no
// locks, and applies the change to the object as it is inside the transaction.
func run[T any](ctx context.Context, s *Service, actor identity.Actor, c change[T]) (T, error) {
	var zero T
	current, err := c.load(ctx)
	if err != nil {
		return zero, err
	}
	d, err := c.authorize(ctx, current)
	if err != nil {
		return zero, err
	}
	reload := c.load
	if c.lock != nil {
		reload = c.lock
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		current, err = reload(ctx)
		if err != nil {
			return err
		}
		before := c.snapshot(current)
		if err := c.apply(ctx, current); err != nil {
			return err
		}
		if err := c.save(ctx, current); err != nil {
			return err
		}
		e := c.event(current)
		e.Actor = actor
		e.Before = before
		e.After = c.snapshot(current)
		return s.record(ctx, d, e)
	})
	if errors.Is(err, errUnchanged) {
		return current, nil
	}
	if err != nil {
		return zero, err
	}
	return current, nil
}

func (s *Service) checkTeam(ctx context.Context, organizationID, teamID string) error {
	if teamID == "" {
		return NewErrInvalidInput("an application needs an owning team")
	}
	ok, err := s.teams.IsActiveTeam(ctx, organizationID, teamID)
	if err != nil {
		return fmt.Errorf("check team %s: %w", teamID, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrInactiveTeam, teamID)
	}
	return nil
}

// activeWorkspace checks that a Workspace exists and is active.
func (s *Service) activeWorkspace(ctx context.Context, organizationID, workspaceID string) error {
	w, err := s.workspaces.LookupWorkspace(ctx, organizationID, workspaceID)
	if errors.Is(err, workspace.ErrNotFound) {
		return fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}
	if err != nil {
		return fmt.Errorf("look up workspace %s: %w", workspaceID, err)
	}
	if w.Archived() {
		return fmt.Errorf("%w: workspace %s", ErrArchived, workspaceID)
	}
	return nil
}

// activeProject returns a Project if it exists and is active.
func (s *Service) activeProject(ctx context.Context, organizationID, projectID string) (*workspace.Project, error) {
	p, err := s.workspaces.LookupProject(ctx, organizationID, projectID)
	if errors.Is(err, workspace.ErrNotFound) {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("look up project %s: %w", projectID, err)
	}
	if p.Archived() {
		return nil, fmt.Errorf("%w: project %s", ErrArchived, projectID)
	}
	return p, nil
}

// ActiveDependents implements workspace.Dependent: a Workspace cannot be
// archived while it has active Environments, Lifecycles, or Registries, nor
// a Project while it has active Applications.
func (s *Service) ActiveDependents(ctx context.Context, target workspace.ArchiveTarget) ([]workspace.Dependency, error) {
	if target.ProjectID != "" {
		n, err := s.applications.CountActiveApplications(ctx, target.OrganizationID, target.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("count applications: %w", err)
		}
		return []workspace.Dependency{{Capability: "delivery", Kind: "Application", Count: n}}, nil
	}
	counts := []struct {
		kind  string
		count func(ctx context.Context, organizationID, workspaceID string) (int, error)
	}{
		{"Environment", s.environments.CountActiveEnvironments},
		{"Lifecycle", s.lifecycles.CountActiveLifecycles},
		{"Registry", s.registries.CountActiveRegistries},
	}
	var deps []workspace.Dependency
	for _, c := range counts {
		n, err := c.count(ctx, target.OrganizationID, target.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("count %s records: %w", c.kind, err)
		}
		deps = append(deps, workspace.Dependency{Capability: "delivery", Kind: c.kind, Count: n})
	}
	return deps, nil
}
