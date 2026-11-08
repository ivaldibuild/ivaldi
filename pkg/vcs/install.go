package vcs

import "context"

// Installation is a provider account that the person who completed an
// install flow can access, as the provider reports it.
type Installation struct {
	Provider ProviderKind
	// HostURL is the provider's web address, such as https://github.com.
	HostURL string
	// InstallationID identifies the account, such as a GitHub App
	// installation ID.
	InstallationID string
	// AccountName is the account's name, such as the GitHub organization
	// login.
	AccountName string
}

// Installer connects a provider account through an install flow that a
// person completes at the provider. The provider, not the caller, vouches
// that the person can access the account, so an Organization cannot claim
// an account by naming its ID.
type Installer interface {
	// InstallURL returns where to send a person to install Ivaldi's app.
	// The provider sends state back with the result.
	InstallURL(state string) string
	// VerifyInstallation exchanges the authorization code the provider sent
	// back, and returns the installation when the person who authorized can
	// access it. It returns ErrUnauthorized when the provider refuses the
	// code or the person cannot access the installation.
	VerifyInstallation(ctx context.Context, code, installationID string) (Installation, error)
}
