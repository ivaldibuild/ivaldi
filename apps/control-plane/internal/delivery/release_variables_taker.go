package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// VariablesLock names the lock that holds one Project's variables, and its
// Applications', for a transaction: changes to them take it, and so does a
// snapshot, so it reads them as they stood together.
func VariablesLock(projectID string) string {
	return "delivery.variables:" + projectID
}

// ReleaseVariablesTaker snapshots the variables an Application deploys with
// as a revision of one of its Releases. The services that create Releases
// and refresh their variables share it.
type ReleaseVariablesTaker struct {
	variables VariableRepository
	revisions ReleaseVariablesRepository
	locker    transaction.Locker
}

func NewReleaseVariablesTaker(variables VariableRepository, revisions ReleaseVariablesRepository, locker transaction.Locker) (*ReleaseVariablesTaker, error) {
	switch {
	case variables == nil:
		return nil, errors.New("release variables taker: a VariableRepository is required")
	case revisions == nil:
		return nil, errors.New("release variables taker: a ReleaseVariablesRepository is required")
	case locker == nil:
		return nil, errors.New("release variables taker: a Locker is required")
	}
	return &ReleaseVariablesTaker{variables: variables, revisions: revisions, locker: locker}, nil
}

// Preview returns what a revision taken now would hold, without storing it:
// the Application's variables and its Project's, as they are.
func (t *ReleaseVariablesTaker) Preview(ctx context.Context, rel *Release, projectID string) ([]SnapshotVariable, error) {
	organizationID := rel.OrganizationID()
	// The Project's and the Application's variables are two reads; held,
	// no change lands between them.
	if err := t.locker.Lock(ctx, VariablesLock(projectID)); err != nil {
		return nil, fmt.Errorf("hold the project's variables: %w", err)
	}
	project, err := t.variables.ListVariables(ctx, organizationID, VariableOwner{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("list the project's variables: %w", err)
	}
	application, err := t.variables.ListVariables(ctx, organizationID, VariableOwner{ApplicationID: rel.ApplicationID()})
	if err != nil {
		return nil, fmt.Errorf("list the application's variables: %w", err)
	}
	return SnapshotVariables(project, application)
}

// Take stores the Application's variables and its Project's, as they are
// now, as the Release's next revision and makes it the current one. It
// returns the revisions it changed, the new one last, so the caller records
// their events in the same transaction.
func (t *ReleaseVariablesTaker) Take(ctx context.Context, rel *Release, projectID string, takenBy *string) ([]*ReleaseVariables, error) {
	variables, err := t.Preview(ctx, rel, projectID)
	if err != nil {
		return nil, err
	}
	return t.Store(ctx, rel, variables, takenBy)
}

// Store makes the variables a caller previewed the Release's next revision,
// so what it checked is exactly what is stored. It returns what Take does.
func (t *ReleaseVariablesTaker) Store(ctx context.Context, rel *Release, variables []SnapshotVariable, takenBy *string) ([]*ReleaseVariables, error) {
	organizationID := rel.OrganizationID()

	var changed []*ReleaseVariables
	latest, err := t.revisions.LatestReleaseVariablesRevision(ctx, organizationID, rel.ID())
	if err != nil {
		return nil, fmt.Errorf("find the release's latest revision: %w", err)
	}
	current, err := t.revisions.GetCurrentReleaseVariables(ctx, organizationID, rel.ID())
	switch {
	case errors.Is(err, ErrNotFound):
	case err != nil:
		return nil, fmt.Errorf("get the release's current revision: %w", err)
	default:
		current.Supersede()
		if err := t.revisions.UpdateReleaseVariables(ctx, current); err != nil {
			return nil, fmt.Errorf("supersede revision %d: %w", current.Revision(), err)
		}
		changed = append(changed, current)
	}

	taken, err := NewReleaseVariables(NewReleaseVariablesProps{
		OrganizationID: organizationID, ApplicationID: rel.ApplicationID(), ReleaseID: rel.ID(),
		Revision: latest + 1, Variables: variables, TakenByPrincipalID: takenBy,
	}, nil)
	if err != nil {
		return nil, err
	}
	if err := t.revisions.CreateReleaseVariables(ctx, taken); err != nil {
		return nil, fmt.Errorf("store revision %d: %w", taken.Revision(), err)
	}
	return append(changed, taken), nil
}
