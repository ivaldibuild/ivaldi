package variables

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. It implements the whole interface, so a
// new operation does not compile until it says how it is authorized.
//
// Variables are not IAM resources: every operation is checked on the
// owning Project, or the owning Application's Project. Finding it for an
// existing Variable means reading the Variable first; next reads it again in
// its transaction. A Variable never moves to another owner, and an
// Application never moves to another Project.
//
// A change to values scoped to Environments, or to Targets in them, is also
// checked on each of those Environments, so the Project's members cannot
// change an Environment's configuration without being allowed to there. The
// audit record carries the Project's decision.
type authorized struct {
	next         delivery.VariableService
	authz        authorization.Authorizer
	variables    delivery.VariableRepository
	applications ApplicationLookup
	targets      TargetLookup
}

var _ delivery.VariableService = (*authorized)(nil)

// projectOf returns the Project an owner names: the Project itself, or its
// Application's.
func (a *authorized) projectOf(ctx context.Context, organizationID string, projectID, applicationID *string) (string, error) {
	if applicationID == nil {
		return *projectID, nil
	}
	app, err := a.applications.GetApplication(ctx, organizationID, *applicationID)
	if err != nil {
		return "", err
	}
	return app.ProjectID(), nil
}

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, projectID, action string) error {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionVariableRead, OrganizationID: organizationID,
		ResourceID: projectID, Action: action,
	})
	return err
}

// manage checks a change on the Project and on every Environment the scopes
// it touches reach, and returns ctx carrying the Project's decision.
func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, projectID, action string, proposal any, scopes ...delivery.VariableScope) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: delivery.PermissionVariableManage, OrganizationID: organizationID,
		ResourceID: projectID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	environmentIDs, err := a.environmentsOf(ctx, organizationID, scopes)
	if err != nil {
		return nil, err
	}
	for _, environmentID := range environmentIDs {
		_, err := authorization.Require(ctx, a.authz, authorization.Request{
			Actor: actor, Permission: delivery.PermissionVariableManage, OrganizationID: organizationID,
			ResourceID: environmentID, Action: action, Proposal: proposal,
		})
		if err != nil {
			return nil, err
		}
	}
	return authorization.WithDecision(ctx, "", d), nil
}

// environmentsOf returns the Environments the scopes name, and those of the
// Targets they name.
func (a *authorized) environmentsOf(ctx context.Context, organizationID string, scopes []delivery.VariableScope) ([]string, error) {
	var out []string
	add := func(environmentID string) {
		if !slices.Contains(out, environmentID) {
			out = append(out, environmentID)
		}
	}
	for _, scope := range scopes {
		for _, environmentID := range scope.EnvironmentIDs {
			add(environmentID)
		}
		for _, targetID := range scope.TargetIDs {
			t, err := a.targets.GetTarget(ctx, organizationID, targetID)
			if errors.Is(err, delivery.ErrNotFound) {
				return nil, delivery.NewErrInvalidInput(fmt.Sprintf("target %s does not exist", targetID))
			}
			if err != nil {
				return nil, fmt.Errorf("get target %s: %w", targetID, err)
			}
			add(t.EnvironmentID())
		}
	}
	return out, nil
}

// manageVariable checks a change to a Variable: on its Project, and on the
// Environments reached by scopes(v), the values the change touches.
func (a *authorized) manageVariable(ctx context.Context, actor identity.Actor, organizationID, variableID, action string, proposal any, scopes func(*delivery.Variable) ([]delivery.VariableScope, error)) (context.Context, error) {
	v, err := a.variables.GetVariable(ctx, organizationID, variableID)
	if err != nil {
		return nil, err
	}
	projectID, err := a.projectOf(ctx, organizationID, v.ProjectID(), v.ApplicationID())
	if err != nil {
		return nil, err
	}
	touched, err := scopes(v)
	if err != nil {
		return nil, err
	}
	ctx, err = a.manage(ctx, actor, organizationID, projectID, action, proposal, touched...)
	if err != nil {
		return nil, err
	}
	return withDecidedOn(ctx, v), nil
}

type decidedOnKey struct{}

// withDecidedOn records which version of a Variable a decision was made on.
// The Environments a change needs permission on come from the Variable's
// values, and next reads it again in its transaction: had it changed in
// between, the decision would cover the wrong Environments.
func withDecidedOn(ctx context.Context, v *delivery.Variable) context.Context {
	return context.WithValue(ctx, decidedOnKey{}, v.UpdatedAt())
}

// checkDecidedOn refuses a Variable changed since the decision in ctx was
// made on it, so the caller reads it and is authorized again.
func checkDecidedOn(ctx context.Context, v *delivery.Variable) error {
	if decidedOn, ok := ctx.Value(decidedOnKey{}).(time.Time); ok && !decidedOn.Equal(v.UpdatedAt()) {
		return fmt.Errorf("%w: variable %s", delivery.ErrConflict, v.ID())
	}
	return nil
}

// everyValue is the scopes of a change that affects all of a Variable's
// values, such as where the chart receives it.
func everyValue(v *delivery.Variable) ([]delivery.VariableScope, error) {
	return []delivery.VariableScope{v.Scope()}, nil
}

// resolveOwner replaces an Application's slug with its ID and returns the
// owner's Project.
func (a *authorized) resolveOwner(ctx context.Context, organizationID string, owner *delivery.VariableOwner) (string, error) {
	if owner.ApplicationID == "" {
		return owner.ProjectID, nil
	}
	app, err := a.applications.GetApplicationByIdentifier(ctx, organizationID, owner.ApplicationID)
	if err != nil {
		return "", err
	}
	owner.ApplicationID = app.ID()
	return app.ProjectID(), nil
}

func (a *authorized) CreateVariable(ctx context.Context, actor identity.Actor, in delivery.CreateVariableInput) (*delivery.Variable, error) {
	projectID, err := a.resolveOwner(ctx, in.OrganizationID, &in.Owner)
	if err != nil {
		return nil, err
	}
	scopes := make([]delivery.VariableScope, len(in.Values))
	for i, value := range in.Values {
		scopes[i] = value.Scope
	}
	ctx, err = a.manage(ctx, actor, in.OrganizationID, projectID, delivery.ActionVariableCreate, in, scopes...)
	if err != nil {
		return nil, err
	}
	return a.next.CreateVariable(ctx, actor, in)
}

func (a *authorized) GetVariable(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
	v, err := a.next.GetVariable(ctx, actor, organizationID, variableID)
	if err != nil {
		return nil, err
	}
	projectID, err := a.projectOf(ctx, organizationID, v.ProjectID(), v.ApplicationID())
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, organizationID, projectID, delivery.ActionVariableRead); err != nil {
		return nil, err
	}
	return v, nil
}

func (a *authorized) ListVariables(ctx context.Context, actor identity.Actor, query delivery.ListVariablesQuery) ([]*delivery.Variable, error) {
	projectID, err := a.resolveOwner(ctx, query.OrganizationID, &query.Owner)
	if err != nil {
		return nil, err
	}
	if err := a.read(ctx, actor, query.OrganizationID, projectID, delivery.ActionVariableList); err != nil {
		return nil, err
	}
	return a.next.ListVariables(ctx, actor, query)
}

func (a *authorized) UpdateVariable(ctx context.Context, actor identity.Actor, organizationID, variableID string, props delivery.UpdateVariableProps) (*delivery.Variable, error) {
	ctx, err := a.manageVariable(ctx, actor, organizationID, variableID, delivery.ActionVariableUpdate, props, everyValue)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateVariable(ctx, actor, organizationID, variableID, props)
}

func (a *authorized) AddVariableValue(ctx context.Context, actor identity.Actor, organizationID, variableID string, props delivery.VariableValueProps) (*delivery.Variable, error) {
	ctx, err := a.manageVariable(ctx, actor, organizationID, variableID, delivery.ActionVariableAddValue, props,
		func(*delivery.Variable) ([]delivery.VariableScope, error) {
			return []delivery.VariableScope{props.Scope}, nil
		})
	if err != nil {
		return nil, err
	}
	return a.next.AddVariableValue(ctx, actor, organizationID, variableID, props)
}

// UpdateVariableValue is checked on the value's scope before and after:
// taking a value away from an Environment changes that Environment too.
func (a *authorized) UpdateVariableValue(ctx context.Context, actor identity.Actor, organizationID, variableID, valueID string, props delivery.VariableValueProps) (*delivery.Variable, error) {
	ctx, err := a.manageVariable(ctx, actor, organizationID, variableID, delivery.ActionVariableUpdateValue, props,
		func(v *delivery.Variable) ([]delivery.VariableScope, error) {
			current, err := v.Value(valueID)
			if err != nil {
				return nil, err
			}
			return []delivery.VariableScope{current.Scope, props.Scope}, nil
		})
	if err != nil {
		return nil, err
	}
	return a.next.UpdateVariableValue(ctx, actor, organizationID, variableID, valueID, props)
}

func (a *authorized) RemoveVariableValue(ctx context.Context, actor identity.Actor, organizationID, variableID, valueID string) (*delivery.Variable, error) {
	ctx, err := a.manageVariable(ctx, actor, organizationID, variableID, delivery.ActionVariableRemoveValue, valueID,
		func(v *delivery.Variable) ([]delivery.VariableScope, error) {
			current, err := v.Value(valueID)
			if err != nil {
				return nil, err
			}
			return []delivery.VariableScope{current.Scope}, nil
		})
	if err != nil {
		return nil, err
	}
	return a.next.RemoveVariableValue(ctx, actor, organizationID, variableID, valueID)
}

func (a *authorized) ArchiveVariable(ctx context.Context, actor identity.Actor, organizationID, variableID string) (*delivery.Variable, error) {
	ctx, err := a.manageVariable(ctx, actor, organizationID, variableID, delivery.ActionVariableArchive, nil, everyValue)
	if err != nil {
		return nil, err
	}
	return a.next.ArchiveVariable(ctx, actor, organizationID, variableID)
}
