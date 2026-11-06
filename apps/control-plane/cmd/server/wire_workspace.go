package server

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	deliverypg "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/projects"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/settings"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspaces"
)

// wireProvisioner builds what IAM calls to set up a new Organization: its
// default Workspace. It must exist before onboarding.
func (a *app) wireProvisioner() error {
	p, err := workspace.NewProvisioner(workspacepg.NewRepository(a.db), a.tx, a.iam.resources, a.outbox, a.audit)
	if err != nil {
		return err
	}
	a.provisioner = p
	return nil
}

// wireWorkspace builds the workspace capability's services and HTTP APIs.
// Settings use the registry, locks, and resolver from wireSettings.
func (a *app) wireWorkspace() error {
	repo := workspacepg.NewRepository(a.db)
	dependents, err := delivery.NewDependents(
		deliverypg.NewEnvironmentRepository(a.db),
		deliverypg.NewLifecycleRepository(a.db),
		deliverypg.NewRegistryRepository(a.db),
		deliverypg.NewApplicationRepository(a.db),
	)
	if err != nil {
		return err
	}

	ws, err := workspaces.NewWorkspaceService(
		workspaces.WithRepository(repo),
		workspaces.WithResourceRegistrar(a.iam.resources),
		workspaces.WithAuthorizer(a.authz),
		workspaces.WithTransactor(a.tx),
		workspaces.WithOutboxRecorder(a.outbox),
		workspaces.WithAuditRecorder(a.audit),
		workspaces.WithDependents(dependents),
	)
	if err != nil {
		return err
	}
	ps, err := projects.NewProjectService(
		projects.WithRepository(repo),
		projects.WithResourceRegistrar(a.iam.resources),
		projects.WithTeams(a.teams),
		projects.WithAuthorizer(a.authz),
		projects.WithTransactor(a.tx),
		projects.WithOutboxRecorder(a.outbox),
		projects.WithAuditRecorder(a.audit),
		projects.WithDependents(dependents),
	)
	if err != nil {
		return err
	}
	ss, err := settings.NewSettingService(
		settings.WithRepository(repo),
		settings.WithDefinitions(a.settingDefs),
		settings.WithLockSource(a.settingLocks),
		settings.WithAuthorizer(a.authz),
		settings.WithTransactor(a.tx),
		settings.WithOutboxRecorder(a.outbox),
		settings.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.routes = append(a.routes,
		workspaces.NewWorkspaceController(ws),
		projects.NewProjectController(ps),
		settings.NewSettingController(ss),
	)
	return nil
}
