package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// ServiceOption supplies one of the Service's collaborators. All are required.
type ServiceOption func(*service)

type service struct {
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
	outbox       outbox.Recorder
}

var _ Service = (*service)(nil)

// WithEnvironments sets the EnvironmentRepository for the service.
func WithEnvironments(r EnvironmentRepository) ServiceOption {
	return func(s *service) {
		s.environments = r
	}
}

// WithLifecycles sets the LifecycleRepository for the service.
func WithLifecycles(r LifecycleRepository) ServiceOption {
	return func(s *service) {
		s.lifecycles = r
	}
}

// WithTargets sets the TargetRepository for the service.
func WithTargets(r TargetRepository) ServiceOption {
	return func(s *service) {
		s.targets = r
	}
}

// WithRegistries sets the RegistryRepository for the service.
func WithRegistries(r RegistryRepository) ServiceOption {
	return func(s *service) {
		s.registries = r
	}
}

// WithApplications sets the ApplicationRepository for the service.
func WithApplications(r ApplicationRepository) ServiceOption {
	return func(s *service) {
		s.applications = r
	}
}

// WithTransactor sets the Transactor for the service. It makes a change, its
// IAM resource, its audit record, and its events commit together, so the
// ResourceRegistrar, Audit Recorder, and Outbox Recorder must write through
// the context it passes.
func WithTransactor(tx transaction.Transactor) ServiceOption {
	return func(s *service) {
		s.tx = tx
	}
}

// WithAuthorizer sets the Authorizer for the service.
func WithAuthorizer(authz authorization.Authorizer) ServiceOption {
	return func(s *service) {
		s.authz = authz
	}
}

// WithResourceRegistrar sets the ResourceRegistrar for the service.
func WithResourceRegistrar(r iam.ResourceRegistrar) ServiceOption {
	return func(s *service) {
		s.resources = r
	}
}

// WithTeamDirectory sets the Team Directory for the service.
func WithTeamDirectory(t team.Directory) ServiceOption {
	return func(s *service) {
		s.teams = t
	}
}

// WithAuditRecorder sets the Audit Recorder for the service.
func WithAuditRecorder(a audit.Recorder) ServiceOption {
	return func(s *service) {
		s.audit = a
	}
}

// WithWorkspaces sets the Workspaces for the service.
func WithWorkspaces(w Workspaces) ServiceOption {
	return func(s *service) {
		s.workspaces = w
	}
}

// WithOutboxRecorder sets the Outbox Recorder for the service.
func WithOutboxRecorder(o outbox.Recorder) ServiceOption {
	return func(s *service) {
		s.outbox = o
	}
}

// NewService returns a Service, or an error naming a missing dependency.
func NewService(opts ...ServiceOption) (Service, error) {
	c := &service{}
	for _, o := range opts {
		o(c)
	}

	required := []struct {
		name string
		ok   bool
	}{
		{"Environments", c.environments != nil},
		{"Lifecycles", c.lifecycles != nil},
		{"Targets", c.targets != nil},
		{"Registries", c.registries != nil},
		{"Applications", c.applications != nil},
		{"Transactor", c.tx != nil},
		{"Authorizer", c.authz != nil},
		{"Resources", c.resources != nil},
		{"Teams", c.teams != nil},
		{"Audit", c.audit != nil},
		{"Workspaces", c.workspaces != nil},
		{"Outbox", c.outbox != nil},
	}

	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("delivery service: %s is required", r.name)
		}
	}

	return c, nil
}

// authorize asks the single authorizer and turns anything but an allow into an error.
func (s *service) authorize(ctx context.Context, req authorization.Request) (authorization.Decision, error) {
	return authorization.Require(ctx, s.authz, req)
}

// record writes an audit event, stamped with the decision that allowed it.
func (s *service) record(ctx context.Context, d authorization.Decision, e audit.Event) error {
	e.DecisionID = d.ID
	e.ApprovedByPrincipalID = d.ApprovedByPrincipalID
	e.OccurredAt = clock.Now()
	if err := s.audit.Record(ctx, e); err != nil {
		return fmt.Errorf("record %s: %w", e.Type, err)
	}
	return nil
}

// aggregateObject is a delivery aggregate: it belongs to an Organization and
// holds the events its methods recorded until they are saved.
type aggregateObject interface {
	OrganizationID() string
	PullEvents() []aggregate.Event
}

// recordEvents wraps the events obj recorded and writes them to the outbox in
// the caller's transaction, so they commit with the object they describe.
// aggregateID is the object's ID, or its key when it has no ID of its own,
// such as a Placement's.
func (s *service) recordEvents(ctx context.Context, aggregateType, aggregateID string, obj aggregateObject) error {
	envelopes, err := events.Wrap(ctx, events.Source{
		OrganizationID: obj.OrganizationID(),
		AggregateType:  aggregateType,
		AggregateID:    aggregateID,
	}, obj.PullEvents()...)
	if err != nil {
		return fmt.Errorf("wrap %s events: %w", aggregateType, err)
	}
	if err := s.outbox.Record(ctx, envelopes...); err != nil {
		return fmt.Errorf("record %s events: %w", aggregateType, err)
	}
	return nil
}

// change is one change to a stored object: load it, apply domain methods to
// it, save it, record the events it raised, and record an audit event with
// its state before and after.
type change[T aggregateObject] struct {
	// aggregateType names the domain type that raises the events, such as
	// AggregateApplication.
	aggregateType string
	// aggregateID names the changed object for the envelope: its ID, or its
	// key when it has no ID of its own, such as a Placement.
	aggregateID func(T) string
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
func run[T aggregateObject](ctx context.Context, s *service, actor identity.Actor, c change[T]) (T, error) {
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
		if err := s.recordEvents(ctx, c.aggregateType, c.aggregateID(current), current); err != nil {
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

func (s *service) checkTeam(ctx context.Context, organizationID, teamID string) error {
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
func (s *service) activeWorkspace(ctx context.Context, organizationID, workspaceID string) error {
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
func (s *service) activeProject(ctx context.Context, organizationID, projectID string) (*workspace.Project, error) {
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
func (s *service) ActiveDependents(ctx context.Context, target workspace.ArchiveTarget) ([]workspace.Dependency, error) {
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
