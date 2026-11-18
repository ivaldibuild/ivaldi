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
	// ErrNotPending means an approval request was already approved,
	// rejected, or cancelled.
	ErrNotPending = errors.New("approval request is not pending")
	// ErrSessionRevoked means the agent Session that asked for an approval
	// was revoked, so its work cannot be approved; it can still be rejected.
	ErrSessionRevoked = errors.New("the requesting agent session was revoked")
	// ErrCannotApply means the subject's capability refused to apply an
	// approved subject, such as a Deployment whose checks now fail. The
	// request stays pending, so it can be rejected instead.
	ErrCannotApply = errors.New("the subject can no longer be applied")
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
