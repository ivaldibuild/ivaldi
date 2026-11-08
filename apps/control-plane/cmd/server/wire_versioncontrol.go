package server

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp/session"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/connections"
	versioncontrolpg "github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/sources"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
	"github.com/ivaldibuild/ivaldi/pkg/vcs/github"
)

// installStateKeyPurpose derives the keys that seal install state, apart
// from the sign-in cookies' keys.
const installStateKeyPurpose = "ivaldi/versioncontrol/install-state/v1"

// wireVersionControl builds the version-control capability's services and
// HTTP APIs. GitHub is optional: the control plane starts without it, and
// connecting GitHub or creating a Source then fails with a clear error
// rather than stopping the whole process.
func (a *app) wireVersionControl(cfg *config.Config) error {
	connectionRepo := versioncontrolpg.NewConnectionRepository(a.db)
	sourceRepo := versioncontrolpg.NewSourceRepository(a.db)

	client, err := gitHubClient(cfg)
	if err != nil {
		return err
	}
	// A nil *github.Client must not become a non-nil interface.
	var provider vcs.Provider
	var installer vcs.Installer
	if client != nil {
		provider, installer = client, client
	}

	stateKeys, err := a.keys.DeriveKeys(installStateKeyPurpose)
	if err != nil {
		return err
	}
	states, err := session.NewCodec(stateKeys)
	if err != nil {
		return err
	}

	connectionService, err := connections.NewConnectionService(
		connections.WithConnectionRepo(connectionRepo),
		connections.WithInstaller(vcs.ProviderGitHub, installer),
		connections.WithProvider(provider),
		connections.WithConnectedRepositories(sourceRepo),
		connections.WithStateSealer(states),
		connections.WithAuthorizer(a.authz),
		connections.WithTransactor(a.tx),
		connections.WithOutboxRecorder(a.outbox),
		connections.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	sourceService, err := sources.NewSourceService(
		sources.WithSourceRepo(sourceRepo),
		sources.WithConnectionRepo(connectionRepo),
		sources.WithProvider(provider),
		sources.WithWorkspaces(workspace.NewLookups(workspacepg.NewRepository(a.db))),
		sources.WithResourceRegistrar(a.iam.resources),
		sources.WithAuthorizer(a.authz),
		sources.WithTransactor(a.tx),
		sources.WithOutboxRecorder(a.outbox),
		sources.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	a.services.connections = connectionService
	a.services.sources = sourceService

	lookups, err := versioncontrol.NewLookups(sourceRepo, connectionRepo, provider)
	if err != nil {
		return err
	}
	a.commitHistory = lookups
	return nil
}

// gitHubClient returns the configured GitHub App client, or nil when GitHub
// is not configured. config.Validate has already checked the settings.
func gitHubClient(cfg *config.Config) (*github.Client, error) {
	app := cfg.VersionControl.GitHub
	if !app.Configured() {
		return nil, nil
	}
	key, err := app.PrivateKey()
	if err != nil {
		return nil, err
	}
	secret, err := app.Secret()
	if err != nil {
		return nil, err
	}
	client, err := github.New(github.Config{
		AppID:         app.AppID,
		PrivateKeyPEM: key,
		Slug:          app.Slug,
		ClientID:      app.ClientID,
		ClientSecret:  secret.Reveal(),
		WebURL:        app.WebURL,
		APIURL:        app.APIURL,
	})
	if err != nil {
		return nil, fmt.Errorf("configure the GitHub version-control provider: %w", err)
	}
	return client, nil
}
