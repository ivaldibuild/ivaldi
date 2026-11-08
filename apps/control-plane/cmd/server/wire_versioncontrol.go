package server

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/connections"
	versioncontrolpg "github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol/sources"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
	"github.com/ivaldibuild/ivaldi/pkg/vcs/github"
)

// wireVersionControl builds the version-control capability's services and
// HTTP APIs. A provider is optional: the control plane starts without one,
// and creating a Source then fails with a clear error rather than stopping
// the whole process.
func (a *app) wireVersionControl(cfg *config.Config) error {
	connectionRepo := versioncontrolpg.NewConnectionRepository(a.db)
	sourceRepo := versioncontrolpg.NewSourceRepository(a.db)

	provider, err := gitHubProvider(cfg)
	if err != nil {
		return err
	}

	connectionService, err := connections.NewConnectionService(
		connections.WithConnectionRepo(connectionRepo),
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
	return nil
}

// gitHubProvider returns the configured GitHub provider, or nil when GitHub
// reads are not configured.
func gitHubProvider(cfg *config.Config) (vcs.Provider, error) {
	app := cfg.VersionControl.GitHub
	if app.AppID == 0 && app.PrivateKeyFile == "" && app.APIURL == "" {
		return nil, nil
	}
	if !app.Enabled() {
		return nil, fmt.Errorf("configure the GitHub version-control provider: app_id and private_key_file are both required")
	}
	key, err := app.PrivateKey()
	if err != nil {
		return nil, err
	}
	provider, err := github.New(github.Config{
		AppID:         app.AppID,
		PrivateKeyPEM: key,
		BaseURL:       app.APIURL,
	})
	if err != nil {
		return nil, fmt.Errorf("configure the GitHub version-control provider: %w", err)
	}
	return provider, nil
}
