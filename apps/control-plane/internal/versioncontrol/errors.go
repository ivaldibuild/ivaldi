package versioncontrol

import (
	"errors"
)

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists means another active Source in the Organization
	// already connects the same repository.
	ErrAlreadyExists = errors.New("already exists")
	// ErrArchived means the record, or one it depends on, is archived.
	ErrArchived = errors.New("archived")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrWorkspaceMismatch means the Project and the Source's Workspace differ.
	ErrWorkspaceMismatch = errors.New("belongs to another workspace")
	// ErrProviderUnsupported means no implementation is configured for the
	// Connection's provider.
	ErrProviderUnsupported = errors.New("the provider is not supported")
	// ErrProviderUnavailable means the provider could not answer.
	ErrProviderUnavailable = errors.New("the provider is unavailable")
	// ErrInactiveConnection means the Connection cannot be used for lookups.
	ErrInactiveConnection = errors.New("the connection is not active")
	// ErrLookupInDataPlane means the Connection's lookups run in the data
	// plane, so the control plane must not contact it (ADR 0008).
	ErrLookupInDataPlane = errors.New("lookups for this connection run in the data plane")
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
