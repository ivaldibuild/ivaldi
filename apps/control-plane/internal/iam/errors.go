package iam

import "errors"

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists means the record already exists, such as a second
	// membership of one account in an Organization.
	ErrAlreadyExists = errors.New("already exists")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrStatus means the record's status does not allow the change, such as
	// suspending a deleted Organization.
	ErrStatus = errors.New("not allowed in its current status")
	// ErrExpired means a token, code, credential, or Session is past its expiry.
	ErrExpired = errors.New("expired")
	// ErrRevoked means a token, credential, or Session was revoked.
	ErrRevoked = errors.New("revoked")
	// ErrAlreadyUsed means a single-use token or code was used before. For a
	// refresh token, it means a rotated token was replayed, and its whole
	// family must be revoked.
	ErrAlreadyUsed = errors.New("already used")
	// ErrGrantMismatch means an authorization code was redeemed by another
	// client, for another redirect URI, or with the wrong PKCE verifier.
	ErrGrantMismatch = errors.New("does not match the authorization request")
	// ErrManagedByConfiguration means the identity provider is set in the
	// installation's configuration and cannot be changed through the API.
	ErrManagedByConfiguration = errors.New("managed by configuration")
	// ErrBuiltInRole means the action is not allowed on a built-in role.
	ErrBuiltInRole = errors.New("not allowed on a built-in role")
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
