package notifications

import "errors"

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means no notification matches, or it is someone else's.
	ErrNotFound = errors.New("not found")
)

// ErrInvalidInput describes why input failed validation. It matches ErrInvalid.
type ErrInvalidInput struct {
	Message string
}

func (e *ErrInvalidInput) Error() string { return "invalid input: " + e.Message }

func (e *ErrInvalidInput) Is(target error) bool { return target == ErrInvalid }

func NewErrInvalidInput(message string) *ErrInvalidInput {
	return &ErrInvalidInput{Message: message}
}
