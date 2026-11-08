package provenance

import "errors"

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the Change does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists means one of the commits already belongs to a Change
	// in the same Source.
	ErrAlreadyExists = errors.New("already recorded for this commit")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
)

// ErrInvalidInput describes why input failed validation. It matches ErrInvalid.
type ErrInvalidInput struct {
	Message string
}

func (e *ErrInvalidInput) Error() string {
	return "invalid input: " + e.Message
}

func (e *ErrInvalidInput) Is(target error) bool { return target == ErrInvalid }

// NewErrInvalidInput returns an ErrInvalidInput.
func NewErrInvalidInput(message string) *ErrInvalidInput {
	return &ErrInvalidInput{Message: message}
}
