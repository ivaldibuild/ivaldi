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
	// ErrInvalidCredentials means a sign-in's email or password is wrong. It
	// never says which, so it does not reveal which accounts exist.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrNotAdmitted means an identity provider's allow-lists do not admit
	// the person who signed in.
	ErrNotAdmitted = errors.New("not admitted by the identity provider")
	// ErrNoMembership means the person signed in but belongs to no
	// Organization they can use, and none provisions them.
	ErrNoMembership = errors.New("no membership in the organization")
	// ErrSignUpRequired means a platform sign-in matched no Account, so the
	// person must sign up first.
	ErrSignUpRequired = errors.New("sign-up required")
	// ErrAccountExists means an Account already has the email, and it cannot
	// be linked to the new sign-in without proof the person owns it.
	ErrAccountExists = errors.New("an account with this email already exists")
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
