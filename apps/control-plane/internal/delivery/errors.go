package delivery

import "errors"

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrSlugTaken means another active record in the same scope uses the
	// slug, or, for artifacts, the name.
	ErrSlugTaken = errors.New("slug already in use")
	// ErrAlreadyExists means the record already exists, such as an
	// Application's placement on a Target.
	ErrAlreadyExists = errors.New("already exists")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrArchived means the record, or one it depends on, is archived.
	ErrArchived = errors.New("archived")
	// ErrInUse means active records still depend on this one.
	ErrInUse = errors.New("in use")
	// ErrWorkspaceMismatch means two records that must share a Workspace do not.
	ErrWorkspaceMismatch = errors.New("belongs to another workspace")
	// ErrDefaultChannel means the action is not allowed on an Application's default Channel.
	ErrDefaultChannel = errors.New("not allowed on the default channel")
	// ErrInactiveTeam means the owning Team does not exist, is inactive, or is in another Organization.
	ErrInactiveTeam = errors.New("team is not an active team in the organization")
	// ErrNotTargetAgent means the caller is not the runtime agent that serves the Target.
	ErrNotTargetAgent = errors.New("not the target's agent")
)

// ErrInvalidInput describes why input failed validation. It matches ErrInvalid.
type ErrInvalidInput struct {
	Message string
}

func (e *ErrInvalidInput) Error() string {
	return "invalid input: " + e.Message
}

func (e *ErrInvalidInput) Is(target error) bool { return target == ErrInvalid }

func NewErrInvalidInput(message string) *ErrInvalidInput {
	return &ErrInvalidInput{
		Message: message,
	}
}
