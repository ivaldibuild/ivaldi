package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strings"
)

// Mode returns the deployment mode.
func (c *Config) Mode() DeploymentMode { return DeploymentMode(c.DeploymentMode) }

// IssuerURL is ExternalURL without a trailing slash. It is the OAuth issuer,
// the API's resource identifier, and its access tokens' audience, which must
// all be the same string.
func (c *Config) IssuerURL() string { return strings.TrimRight(c.ExternalURL, "/") }

// ValidationError lists every problem with a configuration, so an operator
// can fix them all at once.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid configuration:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Validate checks what the control plane needs before it starts, beyond what
// parsing checks. It reads the files that hold secrets, so a missing one is
// found at start rather than at the first sign-in.
func (c *Config) Validate() error {
	v := &validator{}

	switch c.Mode() {
	case DeploymentModeSelfHosted, DeploymentModeCloud:
	default:
		v.addf("deployment_mode %q must be %q or %q", c.DeploymentMode, DeploymentModeSelfHosted, DeploymentModeCloud)
	}
	v.externalURL(c.ExternalURL)
	c.validateAuth(v)
	c.validateBootstrap(v)

	if len(v.problems) > 0 {
		return &ValidationError{Problems: v.problems}
	}
	return nil
}

func (c *Config) validateAuth(v *validator) {
	a := c.Auth
	if len(a.SigningKeyFiles) == 0 && !a.EphemeralSigningKey {
		v.addf("auth.signing_key_files needs at least one PEM private key, or set auth.ephemeral_signing_key for local development")
	}
	for name, d := range map[string]int64{
		"auth.access_token_ttl":       int64(a.AccessTokenTTL),
		"auth.refresh_token_ttl":      int64(a.RefreshTokenTTL),
		"auth.authorization_code_ttl": int64(a.AuthorizationCodeTTL),
		"auth.sign_in_session_ttl":    int64(a.SignInSessionTTL),
	} {
		if d <= 0 {
			v.addf("%s must be positive", name)
		}
	}
	for _, uri := range a.WebClient.RedirectURIs {
		v.redirectURI("auth.web_client.redirect_uris", uri)
	}

	p := a.Providers
	installation := p.Google.Enabled() || p.GitHub.Enabled() || p.OIDC.Enabled()
	platform := a.Platform.Google.Enabled() || a.Platform.GitHub.Enabled()
	switch c.Mode() {
	case DeploymentModeSelfHosted:
		if platform {
			v.addf("auth.platform providers are only used in %s mode; use auth.providers", DeploymentModeCloud)
		}
	case DeploymentModeCloud:
		if installation {
			v.addf("auth.providers are only used in %s mode; use auth.platform", DeploymentModeSelfHosted)
		}
	}

	if p.Google.Enabled() {
		if len(p.Google.AllowedDomains) == 0 && len(p.Google.AllowedEmails) == 0 {
			v.addf("auth.providers.google needs allowed_domains or allowed_emails, because Google admits any account")
		}
		v.secret("auth.providers.google client secret", p.Google.Secret)
	}
	if p.GitHub.Enabled() {
		if len(p.GitHub.AllowedOrganizations) == 0 && len(p.GitHub.AllowedEmails) == 0 {
			v.addf("auth.providers.github needs allowed_organizations or allowed_emails, because GitHub admits any account")
		}
		v.httpsURL("auth.providers.github.url", p.GitHub.URL)
		v.secret("auth.providers.github client secret", p.GitHub.Secret)
	}
	if p.OIDC.Enabled() {
		v.httpsURL("auth.providers.oidc.issuer_url", p.OIDC.IssuerURL)
		v.secret("auth.providers.oidc client secret", p.OIDC.Secret)
	}
	if a.Platform.Google.Enabled() {
		v.secret("auth.platform.google client secret", a.Platform.Google.Secret)
	}
	if a.Platform.GitHub.Enabled() {
		v.secret("auth.platform.github client secret", a.Platform.GitHub.Secret)
	}
}

func (c *Config) validateBootstrap(v *validator) {
	b := c.Bootstrap
	set := b.OrganizationSlug != "" || b.OrganizationName != "" || b.AdminEmail != "" || b.AdminPasswordFile != ""
	if !set {
		return
	}
	if c.Mode() == DeploymentModeCloud {
		v.addf("bootstrap is only used in %s mode; the managed cloud creates Organizations through sign-up", DeploymentModeSelfHosted)
		return
	}
	if b.OrganizationName == "" {
		v.addf("bootstrap.organization_name is required")
	}
	if b.AdminEmail == "" {
		v.addf("bootstrap.admin_email is required")
	} else if addr, err := mail.ParseAddress(b.AdminEmail); err != nil || addr.Address != b.AdminEmail {
		v.addf("bootstrap.admin_email %q is not an email address", b.AdminEmail)
	}
	if b.AdminPasswordFile != "" {
		if !c.Auth.BuiltinAccounts {
			v.addf("bootstrap.admin_password_file needs auth.builtin_accounts")
		}
		if _, err := os.Stat(b.AdminPasswordFile); err != nil {
			v.addf("bootstrap.admin_password_file: %v", err)
		}
	}
}

type validator struct {
	problems []string
}

func (v *validator) addf(format string, args ...any) {
	v.problems = append(v.problems, fmt.Sprintf(format, args...))
}

// secret checks that an enabled provider's client secret resolves to a value.
func (v *validator) secret(name string, resolve func() (Secret, error)) {
	s, err := resolve()
	switch {
	case err != nil:
		v.addf("%v", err)
	case s == "":
		v.addf("%s is required", name)
	}
}

// externalURL checks the address clients reach the control plane at. It is
// the OAuth issuer, which RFC 8414 requires to have no query or fragment;
// paths are refused too, because the well-known documents sit at the root.
func (v *validator) externalURL(raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		v.addf("external_url %q must be an absolute http or https URL", raw)
		return
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		v.addf("external_url %q must have no path, query, or fragment", raw)
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		v.addf("external_url %q must use https unless it is a loopback address", raw)
	}
}

// httpsURL checks an identity provider's address. Tokens and secrets travel
// to it, so it must use https.
func (v *validator) httpsURL(field, raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme != "https" {
		v.addf("%s %q must be an absolute https URL", field, raw)
	}
}

func (v *validator) redirectURI(field, raw string) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Fragment != "" {
		v.addf("%s %q must be an absolute URL without a fragment", field, raw)
		return
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		v.addf("%s %q must use https unless it is a loopback address", field, raw)
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
