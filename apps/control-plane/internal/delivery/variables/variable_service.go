// Package variables implements delivery.VariableService: the variables a
// Project's Applications, or one Application, deploy with, and their HTTP
// API. The domain types stay in delivery; this package orchestrates them.
//
// NewVariableService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller is checked
// the same way. The service below assumes that check has happened and
// records the decision that allowed each change in its audit record.
package variables

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// ApplicationLookup reads the Application that owns a variable, to find its
// Project, and the Channels a value is scoped to.
// delivery.ApplicationRepository provides it.
type ApplicationLookup interface {
	GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error)
	GetApplicationByIdentifier(ctx context.Context, organizationID, identifier string) (*delivery.Application, error)
	GetChannel(ctx context.Context, organizationID, channelID string) (*delivery.Channel, error)
}

// EnvironmentLookup reads the Environments a value is scoped to.
// delivery.EnvironmentRepository provides it.
type EnvironmentLookup interface {
	GetEnvironments(ctx context.Context, organizationID string, environmentIDs []string) ([]*delivery.Environment, error)
}

// TargetLookup reads the Targets a value is scoped to.
// delivery.TargetRepository provides it.
type TargetLookup interface {
	GetTarget(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
}

type VariableServiceOption func(*options)

type options struct {
	variables    delivery.VariableRepository
	applications ApplicationLookup
	environments EnvironmentLookup
	targets      TargetLookup
	workspaces   delivery.Workspaces
	locker       transaction.Locker
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithVariableRepo(r delivery.VariableRepository) VariableServiceOption {
	return func(o *options) { o.variables = r }
}
func WithApplicationLookup(a ApplicationLookup) VariableServiceOption {
	return func(o *options) { o.applications = a }
}
func WithEnvironmentLookup(e EnvironmentLookup) VariableServiceOption {
	return func(o *options) { o.environments = e }
}
func WithTargetLookup(t TargetLookup) VariableServiceOption {
	return func(o *options) { o.targets = t }
}
func WithWorkspaces(w delivery.Workspaces) VariableServiceOption {
	return func(o *options) { o.workspaces = w }
}

// WithLocker makes changes to one Project's variables, and its
// Applications', run one at a time, so the rules that span several
// variables are checked against what is really stored.
func WithLocker(l transaction.Locker) VariableServiceOption {
	return func(o *options) { o.locker = l }
}
func WithAuthorizer(a authorization.Authorizer) VariableServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repositories, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) VariableServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) VariableServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) VariableServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewVariableService returns the VariableService, authorizing every
// operation, or an error naming a missing dependency.
func NewVariableService(opts ...VariableServiceOption) (delivery.VariableService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"VariableRepo", o.variables != nil},
		{"ApplicationLookup", o.applications != nil},
		{"EnvironmentLookup", o.environments != nil},
		{"TargetLookup", o.targets != nil},
		{"Workspaces", o.workspaces != nil},
		{"Locker", o.locker != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("variable service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("variable service: %w", err)
	}
	s := &service{
		variables:    o.variables,
		applications: o.applications,
		environments: o.environments,
		targets:      o.targets,
		workspaces:   o.workspaces,
		locker:       o.locker,
		w:            o.writer,
	}
	return &authorized{next: s, authz: o.authz, variables: o.variables, applications: o.applications, targets: o.targets}, nil
}

// service carries out Variable operations once they are authorized. It
// checks rules that span records, changes objects only through their
// methods, and commits each change with its events and audit record.
type service struct {
	variables    delivery.VariableRepository
	applications ApplicationLookup
	environments EnvironmentLookup
	targets      TargetLookup
	workspaces   delivery.Workspaces
	locker       transaction.Locker
	w            change.Writer
}

var _ delivery.VariableService = (*service)(nil)

// owner is where a Variable sits: the Project that bounds it and the
// Workspace its scopes must stay in.
type owner struct {
	projectID   string
	workspaceID string
}

// activeOwner returns where an owner sits, if its Application and Project
// are active, and holds the Project's variables for the transaction.
func (s *service) activeOwner(ctx context.Context, v *delivery.Variable) (owner, error) {
	projectID := v.ProjectID()
	if applicationID := v.ApplicationID(); applicationID != nil {
		app, err := s.applications.GetApplication(ctx, v.OrganizationID(), *applicationID)
		if err != nil {
			return owner{}, err
		}
		if app.Archived() {
			return owner{}, fmt.Errorf("%w: application %s", delivery.ErrArchived, app.ID())
		}
		projectID = new(app.ProjectID())
	}
	project, err := delivery.ActiveProject(ctx, s.workspaces, v.OrganizationID(), *projectID)
	if err != nil {
		return owner{}, err
	}
	if err := s.locker.Lock(ctx, delivery.VariablesLock(project.ID())); err != nil {
		return owner{}, err
	}
	return owner{projectID: project.ID(), workspaceID: project.WorkspaceID()}, nil
}

// CreateVariable creates a Variable for an active Project or Application.
func (s *service) CreateVariable(ctx context.Context, actor identity.Actor, in delivery.CreateVariableInput) (*delivery.Variable, error) {
	values := make([]delivery.NewVariableValueProps, len(in.Values))
	for i, value := range in.Values {
		values[i] = delivery.NewVariableValueProps{Value: value.Value, Scope: value.Scope}
	}
	v, err := delivery.NewVariable(delivery.NewVariableProps{
		OrganizationID: in.OrganizationID,
		ProjectID:      &in.Owner.ProjectID,
		ApplicationID:  &in.Owner.ApplicationID,
		Name:           in.Name,
		Description:    &in.Description,
		Kind:           in.Kind,
		ValuesPath:     &in.ValuesPath,
		Values:         values,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		o, err := s.activeOwner(ctx, v)
		if err != nil {
			return err
		}
		if err := s.checkScope(ctx, v, o.workspaceID, v.Scope()); err != nil {
			return err
		}
		if err := s.checkProject(ctx, o.projectID, v); err != nil {
			return err
		}
		if err := s.variables.CreateVariable(ctx, v); err != nil {
			return fmt.Errorf("create variable: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateVariable, v.ID(), v); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventVariableCreated, OrganizationID: in.OrganizationID, ResourceID: v.ID(), Actor: actor, After: v.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// checkScope checks that everything a scope names is active and the
// Variable's to use: Environments and Targets in its Workspace, and Channels
// of its Application.
func (s *service) checkScope(ctx context.Context, v *delivery.Variable, workspaceID string, scope delivery.VariableScope) error {
	organizationID := v.OrganizationID()
	environmentIDs := slices.Clone(scope.EnvironmentIDs)
	for _, targetID := range scope.TargetIDs {
		t, err := s.targets.GetTarget(ctx, organizationID, targetID)
		if errors.Is(err, delivery.ErrNotFound) {
			return delivery.NewErrInvalidInput(fmt.Sprintf("target %s does not exist", targetID))
		}
		if err != nil {
			return fmt.Errorf("get target %s: %w", targetID, err)
		}
		if t.Archived() {
			return fmt.Errorf("%w: target %s", delivery.ErrArchived, targetID)
		}
		if !slices.Contains(environmentIDs, t.EnvironmentID()) {
			environmentIDs = append(environmentIDs, t.EnvironmentID())
		}
	}
	if len(environmentIDs) > 0 {
		envs, err := s.environments.GetEnvironments(ctx, organizationID, environmentIDs)
		if err != nil {
			return fmt.Errorf("get environments: %w", err)
		}
		byID := make(map[string]*delivery.Environment, len(envs))
		for _, e := range envs {
			byID[e.ID()] = e
		}
		for _, id := range environmentIDs {
			e, ok := byID[id]
			switch {
			case !ok:
				return delivery.NewErrInvalidInput(fmt.Sprintf("environment %s does not exist", id))
			case e.WorkspaceID() != workspaceID:
				return fmt.Errorf("%w: environment %s is not in workspace %s", delivery.ErrWorkspaceMismatch, id, workspaceID)
			case e.Archived():
				return fmt.Errorf("%w: environment %s", delivery.ErrArchived, id)
			}
		}
	}
	for _, channelID := range scope.ChannelIDs {
		c, err := s.applications.GetChannel(ctx, organizationID, channelID)
		if errors.Is(err, delivery.ErrNotFound) || (err == nil && !equal(v.ApplicationID(), c.ApplicationID())) {
			return delivery.NewErrInvalidInput(fmt.Sprintf("channel %s is not one of the application's channels", channelID))
		}
		if err != nil {
			return fmt.Errorf("get channel %s: %w", channelID, err)
		}
		if c.Archived() {
			return fmt.Errorf("%w: channel %s", delivery.ErrArchived, channelID)
		}
	}
	return nil
}

func equal(a *string, b string) bool { return a != nil && *a == b }

// checkProject checks the variables every Application in the Project would
// deploy with once changed is saved: what an Application's variable shadows
// must agree with it, and no two may write overlapping values paths. It
// also keeps each owner within its limit. The caller holds the Project's
// lock, so what is read here is what the change is saved beside.
func (s *service) checkProject(ctx context.Context, projectID string, changed *delivery.Variable) error {
	project, applications, err := s.variables.ListVariablesInProject(ctx, changed.OrganizationID(), projectID)
	if err != nil {
		return fmt.Errorf("list the project's variables: %w", err)
	}
	if applicationID := changed.ApplicationID(); applicationID != nil {
		applications[*applicationID] = withChanged(applications[*applicationID], changed)
		if len(applications[*applicationID]) > delivery.MaxVariablesPerOwner {
			return delivery.NewErrInvalidInput(fmt.Sprintf("an application can have at most %d variables", delivery.MaxVariablesPerOwner))
		}
		return delivery.CheckDeployable(project, applications[*applicationID])
	}
	project = withChanged(project, changed)
	if len(project) > delivery.MaxVariablesPerOwner {
		return delivery.NewErrInvalidInput(fmt.Sprintf("a project can have at most %d variables", delivery.MaxVariablesPerOwner))
	}
	if err := delivery.CheckDeployable(project, nil); err != nil {
		return err
	}
	for _, own := range applications {
		if err := delivery.CheckDeployable(project, own); err != nil {
			return err
		}
	}
	return nil
}

// withChanged returns the stored variables as they will be once changed is
// saved: replaced, added, or, when it is archived, left out.
func withChanged(stored []*delivery.Variable, changed *delivery.Variable) []*delivery.Variable {
	out := slices.DeleteFunc(slices.Clone(stored), func(v *delivery.Variable) bool { return v.ID() == changed.ID() })
	if !changed.Archived() {
		out = append(out, changed)
	}
	return out
}

func (s *service) GetVariable(ctx context.Context, _ identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
	return s.variables.GetVariable(ctx, organizationID, variableID)
}

// ListVariables returns an owner's active variables, an Application's own
// before the ones it inherits from its Project.
func (s *service) ListVariables(ctx context.Context, _ identity.Actor, query delivery.ListVariablesQuery) ([]*delivery.Variable, error) {
	own, err := s.variables.ListVariables(ctx, query.OrganizationID, query.Owner)
	if err != nil {
		return nil, err
	}
	if query.Owner.ApplicationID == "" || !query.IncludeInherited {
		return own, nil
	}
	app, err := s.applications.GetApplication(ctx, query.OrganizationID, query.Owner.ApplicationID)
	if err != nil {
		return nil, err
	}
	inherited, err := s.variables.ListVariables(ctx, query.OrganizationID, delivery.VariableOwner{ProjectID: app.ProjectID()})
	if err != nil {
		return nil, err
	}
	return append(own, inherited...), nil
}

// changeVariable applies a change to a Variable of an active owner, then
// checks the rules that span the Project's variables. scope returns what the
// change newly names, to check it is the Variable's to use.
func (s *service) changeVariable(ctx context.Context, actor identity.Actor, organizationID, variableID, event string, apply func(*delivery.Variable) (delivery.VariableScope, error)) (*delivery.Variable, error) {
	var o owner
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Variable]{
		AggregateType: delivery.AggregateVariable,
		AggregateID:   func(v *delivery.Variable) string { return v.ID() },
		// Read again once the Project's variables are held, so the change
		// applies to what is stored.
		Load: func(ctx context.Context) (*delivery.Variable, error) {
			v, err := s.variables.GetVariable(ctx, organizationID, variableID)
			if err != nil {
				return nil, err
			}
			if o, err = s.activeOwner(ctx, v); err != nil {
				return nil, err
			}
			return s.reload(ctx, organizationID, variableID)
		},
		Apply: func(ctx context.Context, v *delivery.Variable) error {
			scope, err := apply(v)
			if err != nil {
				return err
			}
			if err := s.checkScope(ctx, v, o.workspaceID, scope); err != nil {
				return err
			}
			return s.checkProject(ctx, o.projectID, v)
		},
		Save:     s.variables.UpdateVariable,
		Snapshot: func(v *delivery.Variable) any { return v.Snapshot() },
		Event: func(v *delivery.Variable) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: v.ID()}
		},
	})
}

// UpdateVariable changes an active Variable's description or values path.
func (s *service) UpdateVariable(ctx context.Context, actor identity.Actor, organizationID, variableID string, props delivery.UpdateVariableProps) (*delivery.Variable, error) {
	return s.changeVariable(ctx, actor, organizationID, variableID, delivery.EventVariableUpdated,
		func(v *delivery.Variable) (delivery.VariableScope, error) {
			return delivery.VariableScope{}, v.Update(props)
		})
}

// AddVariableValue adds a value to a Variable.
func (s *service) AddVariableValue(ctx context.Context, actor identity.Actor, organizationID, variableID string, props delivery.VariableValueProps) (*delivery.Variable, error) {
	return s.changeVariable(ctx, actor, organizationID, variableID, delivery.EventVariableUpdated,
		func(v *delivery.Variable) (delivery.VariableScope, error) {
			added, err := v.AddValue(props)
			return added.Scope, err
		})
}

// UpdateVariableValue replaces one of a Variable's values.
func (s *service) UpdateVariableValue(ctx context.Context, actor identity.Actor, organizationID, variableID, valueID string, props delivery.VariableValueProps) (*delivery.Variable, error) {
	return s.changeVariable(ctx, actor, organizationID, variableID, delivery.EventVariableUpdated,
		func(v *delivery.Variable) (delivery.VariableScope, error) {
			if err := v.UpdateValue(valueID, props); err != nil {
				return delivery.VariableScope{}, err
			}
			updated, err := v.Value(valueID)
			return updated.Scope, err
		})
}

// RemoveVariableValue removes one of a Variable's values.
func (s *service) RemoveVariableValue(ctx context.Context, actor identity.Actor, organizationID, variableID, valueID string) (*delivery.Variable, error) {
	return s.changeVariable(ctx, actor, organizationID, variableID, delivery.EventVariableUpdated,
		func(v *delivery.Variable) (delivery.VariableScope, error) {
			return delivery.VariableScope{}, v.RemoveValue(valueID)
		})
}

// reload reads a Variable once its Project's variables are held, and refuses
// one changed since the caller was authorized to change it.
func (s *service) reload(ctx context.Context, organizationID, variableID string) (*delivery.Variable, error) {
	v, err := s.variables.GetVariable(ctx, organizationID, variableID)
	if err != nil {
		return nil, err
	}
	if err := checkDecidedOn(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// lockOwner holds the variables of the Project a Variable sits in for the
// transaction, whether or not its owner is active.
func (s *service) lockOwner(ctx context.Context, v *delivery.Variable) error {
	projectID := v.ProjectID()
	if applicationID := v.ApplicationID(); applicationID != nil {
		app, err := s.applications.GetApplication(ctx, v.OrganizationID(), *applicationID)
		if err != nil {
			return err
		}
		projectID = new(app.ProjectID())
	}
	return s.locker.Lock(ctx, delivery.VariablesLock(*projectID))
}

// ArchiveVariable archives a Variable. Archiving an archived one does
// nothing, and needs no active owner.
func (s *service) ArchiveVariable(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Variable]{
		AggregateType: delivery.AggregateVariable,
		AggregateID:   func(v *delivery.Variable) string { return v.ID() },
		// A save writes the whole Variable, so an archive takes its turn with
		// the other changes to it, and reads it again once it has it: one
		// that ran beside an edit could undo it, or be undone by it.
		Load: func(ctx context.Context) (*delivery.Variable, error) {
			v, err := s.variables.GetVariable(ctx, organizationID, variableID)
			if err != nil {
				return nil, err
			}
			if err := s.lockOwner(ctx, v); err != nil {
				return nil, err
			}
			return s.reload(ctx, organizationID, variableID)
		},
		Apply: func(_ context.Context, v *delivery.Variable) error {
			if v.Archived() {
				return change.ErrUnchanged
			}
			v.Archive()
			return nil
		},
		Save:     s.variables.UpdateVariable,
		Snapshot: func(v *delivery.Variable) any { return v.Snapshot() },
		Event: func(v *delivery.Variable) audit.Event {
			return audit.Event{Type: delivery.EventVariableArchived, OrganizationID: organizationID, ResourceID: v.ID()}
		},
	})
}
