package credentials

import "errors"

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrSlugTaken means another active Secret Store in the Workspace uses the slug.
	ErrSlugTaken = errors.New("slug already in use")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrArchived means the record, or one it depends on, is archived.
	ErrArchived = errors.New("archived")
	// ErrInUse means active records still depend on this one.
	ErrInUse = errors.New("in use")
	// ErrWorkspaceMismatch means two records that must share a Workspace do not.
	ErrWorkspaceMismatch = errors.New("belongs to another workspace")
	// ErrNoFederation means a store is, or would be, in federated mode on an
	// installation with no federation signing keys.
	ErrNoFederation = errors.New("this installation has no federation signing keys")
	// ErrNoBrokering means a store is, or would be, in brokered mode on an
	// installation with no key-encryption key to keep minting credentials
	// with.
	ErrNoBrokering = errors.New("this installation has no key-encryption key")
	// ErrNoCredential means a brokered store's minting credential has not
	// been set.
	ErrNoCredential = errors.New("the store's minting credential has not been set")
	// ErrStoreRefused means a store refused its minting credential, or the
	// request made with it.
	ErrStoreRefused = errors.New("the store refused the request")
	// ErrStoreUnavailable means a store could not be reached, or is
	// throttling or failing: worth retrying.
	ErrStoreUnavailable = errors.New("the store is unavailable")
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
