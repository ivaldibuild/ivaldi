package workspace

import (
	"errors"
	"strings"
)

var (
	// ErrNotFound means the Workspace, Project, or setting does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrInvalid means the input failed validation.
	ErrInvalid = errors.New("invalid input")
	// ErrSlugTaken means another active Workspace or Project at the same level uses the slug.
	ErrSlugTaken = errors.New("slug already in use")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrArchived means the Workspace or Project is archived.
	ErrArchived = errors.New("archived")
	// ErrDefaultWorkspace means the action is not allowed on the default Workspace.
	ErrDefaultWorkspace = errors.New("not allowed on the default workspace")
	// ErrUnknownSetting means no capability registered a definition for the key.
	ErrUnknownSetting = errors.New("unknown setting")
	// ErrLevelNotAllowed means the setting's definition does not allow this level.
	ErrLevelNotAllowed = errors.New("setting not allowed at this level")
	// ErrInactiveTeam means the owning Team does not exist, is inactive, or is in another Organization.
	ErrInactiveTeam = errors.New("team is not an active team in the organization")
	// ErrHasDependents means active records still exist under the Workspace or Project.
	ErrHasDependents = errors.New("has active dependents")
)

// DependentsError lists what is still active under a Workspace or Project
// that was asked to be archived. It matches ErrHasDependents.
type DependentsError struct {
	Dependencies []Dependency
}

func (e *DependentsError) Error() string {
	parts := make([]string, 0, len(e.Dependencies))
	for _, d := range e.Dependencies {
		parts = append(parts, d.String())
	}
	return "has active dependents: " + strings.Join(parts, ", ")
}

func (e *DependentsError) Is(target error) bool { return target == ErrHasDependents }
