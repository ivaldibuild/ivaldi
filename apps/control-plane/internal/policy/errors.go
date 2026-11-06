package policy

import "errors"

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record, or the Workspace it names, does not exist
	// in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a concurrent change got there first, such as the same
	// lock added twice at once.
	ErrConflict = errors.New("changed since it was read")
	// ErrArchived means the Workspace a lock names is archived.
	ErrArchived = errors.New("archived")
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
	return &ErrInvalidInput{Message: message}
}
