package config

import (
	"fmt"
	"net/url"
	"os"
)

// gitHubInstallCallbackPath is the web app's page that finishes a GitHub
// App install.
const gitHubInstallCallbackPath = "/settings/connections/github/callback"

// VersionControl configures the version-control providers Ivaldi reads from.
type VersionControl struct {
	GitHub GitHubApp `yaml:"github"`
}

// GitHubApp is the GitHub App an installation uses to connect GitHub
// accounts and read their repositories and commit history. Organizations
// connect accounts by installing the App; the App's private key and client
// secret are the installation's own credentials, so they are configured
// here rather than stored with a Connection.
//
// The App must have "Request user authorization (OAuth) during
// installation" turned on, with its callback URL set to the web app's
// <web app URL>/settings/connections/github/callback, so GitHub confirms
// the person installing can access the installation.
type GitHubApp struct {
	// AppID is the GitHub App's ID.
	AppID int64 `yaml:"app_id" env:"IVALDI_VERSION_CONTROL_GITHUB_APP_ID"`
	// Slug is the App's name in its URL, as in https://github.com/apps/<slug>.
	Slug string `yaml:"slug" env:"IVALDI_VERSION_CONTROL_GITHUB_SLUG"`
	// PrivateKeyFile holds the App's PEM private key. The secret stays in a
	// mounted file, never in YAML or the environment.
	PrivateKeyFile string `yaml:"private_key_file" env:"IVALDI_VERSION_CONTROL_GITHUB_PRIVATE_KEY_FILE"`
	// ClientID is the App's OAuth client ID.
	ClientID string `yaml:"client_id" env:"IVALDI_VERSION_CONTROL_GITHUB_CLIENT_ID"`
	// ClientSecret, or the file holding it, is the App's OAuth client secret.
	ClientSecret     Secret `yaml:"-" env:"IVALDI_VERSION_CONTROL_GITHUB_CLIENT_SECRET"`
	ClientSecretFile string `yaml:"client_secret_file" env:"IVALDI_VERSION_CONTROL_GITHUB_CLIENT_SECRET_FILE"`
	// WebURL is the GitHub the App is registered with. Leave it empty for
	// github.com; set it for GitHub Enterprise Server, such as
	// https://ghe.example.com. Ivaldi contacts no other GitHub host.
	WebURL string `yaml:"web_url" env:"IVALDI_VERSION_CONTROL_GITHUB_WEB_URL"`
	// APIURL overrides the API address, which is otherwise api.github.com,
	// or <web_url>/api/v3 for GitHub Enterprise Server.
	APIURL string `yaml:"api_url" env:"IVALDI_VERSION_CONTROL_GITHUB_API_URL"`
	// InstallCallbackURL is the web app's page that finishes an install.
	// Leave it empty to use /settings/connections/github/callback on
	// auth.login_url's host. When the App also signs people in, GitHub may
	// return from an install to the sign-in callback instead, which sends
	// the person on to this page.
	InstallCallbackURL string `yaml:"install_callback_url" env:"IVALDI_VERSION_CONTROL_GITHUB_INSTALL_CALLBACK_URL"`
}

// GitHubInstallCallbackURL is the web app's page that finishes a GitHub App
// install: InstallCallbackURL, or the page on auth.login_url's host.
func (c *Config) GitHubInstallCallbackURL() string {
	if u := c.VersionControl.GitHub.InstallCallbackURL; u != "" {
		return u
	}
	login, err := url.Parse(c.Auth.LoginURL)
	if err != nil || login.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: login.Scheme, Host: login.Host, Path: gitHubInstallCallbackPath}).String()
}

// Configured reports whether any GitHub App setting is present.
func (g GitHubApp) Configured() bool {
	return g.AppID != 0 || g.Slug != "" || g.PrivateKeyFile != "" || g.ClientID != "" ||
		g.ClientSecret != "" || g.ClientSecretFile != "" || g.WebURL != "" || g.APIURL != "" ||
		g.InstallCallbackURL != ""
}

// PrivateKey reads the App's PEM private key.
func (g GitHubApp) PrivateKey() ([]byte, error) {
	if g.PrivateKeyFile == "" {
		return nil, fmt.Errorf("version_control.github.private_key_file is required")
	}
	key, err := os.ReadFile(g.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("version_control.github: read the private key: %w", err)
	}
	return key, nil
}

// Secret returns the App's OAuth client secret.
func (g GitHubApp) Secret() (Secret, error) {
	return resolveSecret("version_control.github client secret", g.ClientSecret, g.ClientSecretFile)
}

// validateVersionControl checks a GitHub App that is partly or fully
// configured. Leaving it out entirely is allowed: the control plane starts,
// and connecting GitHub fails with a clear error.
func (c *Config) validateVersionControl(v *validator) {
	g := c.VersionControl.GitHub
	if !g.Configured() {
		return
	}
	if g.AppID <= 0 {
		v.addf("version_control.github.app_id is required")
	}
	if g.Slug == "" {
		v.addf("version_control.github.slug is required")
	}
	if g.ClientID == "" {
		v.addf("version_control.github.client_id is required")
	}
	v.secret("version_control.github client secret", g.Secret)
	if _, err := g.PrivateKey(); err != nil {
		v.addf("%v", err)
	}
	if g.WebURL != "" {
		v.httpsURL("version_control.github.web_url", g.WebURL)
	}
	if g.APIURL != "" {
		v.httpsURL("version_control.github.api_url", g.APIURL)
	}
	if g.InstallCallbackURL != "" {
		v.redirectURI("version_control.github.install_callback_url", g.InstallCallbackURL)
	}
}
