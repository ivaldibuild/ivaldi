package iam

// Events a PasswordCredential records. Each carries the PasswordCredential
// without its hash: only which Account it belongs to and when it changed.

// PasswordCredentialCreated is recorded when an Account gets a password.
type PasswordCredentialCreated struct {
	PasswordCredential PasswordCredentialSnapshot `json:"password_credential"`
}

// PasswordCredentialChanged is recorded when an Account's password changes.
type PasswordCredentialChanged struct {
	PasswordCredential PasswordCredentialSnapshot `json:"password_credential"`
}

func (PasswordCredentialCreated) EventName() string { return "iam.v1.password_credential.created" }
func (PasswordCredentialChanged) EventName() string { return "iam.v1.password_credential.changed" }
