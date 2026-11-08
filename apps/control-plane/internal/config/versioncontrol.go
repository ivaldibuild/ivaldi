package config

import (
	"fmt"
	"os"
)

// VersionControl configures the version-control providers Ivaldi reads from.
type VersionControl struct {
	GitHub GitHubApp `yaml:"github"`
}

// GitHubApp is the GitHub App an installation uses to read repositories and
// commit history. Organizations connect provider accounts as Connections;
// the App's private key is the installation's own credential, so it is
// configured here rather than stored with a Connection.
type GitHubApp struct {
	// AppID is the GitHub App's ID.
	AppID int64 `yaml:"app_id" env:"IVALDI_VERSION_CONTROL_GITHUB_APP_ID"`
	// PrivateKeyFile holds the App's PEM private key. The secret stays in a
	// mounted file, never in YAML or the environment.
	PrivateKeyFile string `yaml:"private_key_file" env:"IVALDI_VERSION_CONTROL_GITHUB_PRIVATE_KEY_FILE"`
	// APIURL overrides the GitHub API address, such as
	// https://ghe.example.com/api/v3. Leave it empty to derive it from each
	// Connection's host.
	APIURL string `yaml:"api_url" env:"IVALDI_VERSION_CONTROL_GITHUB_API_URL"`
}

// Enabled reports whether GitHub reads are configured.
func (g GitHubApp) Enabled() bool {
	return g.AppID > 0 && g.PrivateKeyFile != ""
}

// PrivateKey reads the App's PEM private key.
func (g GitHubApp) PrivateKey() ([]byte, error) {
	if g.PrivateKeyFile == "" {
		return nil, fmt.Errorf("version control: github private_key_file is required")
	}
	key, err := os.ReadFile(g.PrivateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("version control: read the GitHub app private key: %w", err)
	}
	return key, nil
}
