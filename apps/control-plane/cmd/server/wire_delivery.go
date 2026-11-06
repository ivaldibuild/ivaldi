package server

import (
	auditpg "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/applications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/environments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/lifecycles"
	deliverypg "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/registries"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authorizer"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	teampg "github.com/ivaldibuild/ivaldi-control-plane/internal/team/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team/teams"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// wireAccess builds the single authorizer and the audit recorder that every
// capability's services share.
func (a *app) wireAccess(logger logging.Logger) error {
	authz, err := authorizer.New(
		authorizer.WithAccessRepo(iampg.NewAccessRepository(a.db)),
		authorizer.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	a.authz = authz
	a.audit = auditpg.NewRecorder(a.db)
	return nil
}

// wireTeams builds the Team capability's service and HTTP API, and the
// Directory other capabilities check owning Teams through.
func (a *app) wireTeams() error {
	repo := teampg.NewTeamRepository(a.db)
	svc, err := teams.NewTeamService(
		teams.WithTeamRepo(repo),
		teams.WithPrincipals(iampg.NewPrincipalRepository(a.db)),
		teams.WithAgents(iampg.NewAgentRepository(a.db)),
		teams.WithTeamGroups(iampg.NewTeamGroups(a.db)),
		teams.WithAuthorizer(a.authz),
		teams.WithTransactor(a.tx),
		teams.WithOutboxRecorder(a.outbox),
		teams.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.teams = team.NewDirectory(repo)
	a.routes = append(a.routes, teams.NewTeamController(svc))
	return nil
}

// wireDelivery builds delivery's services and their HTTP APIs.
func (a *app) wireDelivery() error {
	envRepo := deliverypg.NewEnvironmentRepository(a.db)
	lifecycleRepo := deliverypg.NewLifecycleRepository(a.db)
	applicationRepo := deliverypg.NewApplicationRepository(a.db)
	registryRepo := deliverypg.NewRegistryRepository(a.db)
	targetRepo := deliverypg.NewTargetRepository(a.db)
	workspaces := workspace.NewLookups(workspacepg.NewRepository(a.db))

	envs, err := environments.NewEnvironmentService(
		environments.WithEnvironmentRepo(envRepo),
		environments.WithTargetRepo(targetRepo),
		environments.WithLifecycleUsage(lifecycleRepo),
		environments.WithWorkspaces(workspaces),
		environments.WithResourceRegistrar(a.iam.resources),
		environments.WithAuthorizer(a.authz),
		environments.WithTransactor(a.tx),
		environments.WithOutboxRecorder(a.outbox),
		environments.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	regs, err := registries.NewRegistryService(
		registries.WithRegistryRepo(registryRepo),
		registries.WithWorkspaces(workspaces),
		registries.WithAuthorizer(a.authz),
		registries.WithTransactor(a.tx),
		registries.WithOutboxRecorder(a.outbox),
		registries.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	lifecycleSvc, err := lifecycles.NewLifecycleService(
		lifecycles.WithLifecycleRepo(lifecycleRepo),
		lifecycles.WithEnvironmentLookup(envRepo),
		lifecycles.WithChannelUsage(applicationRepo),
		lifecycles.WithWorkspaces(workspaces),
		lifecycles.WithAuthorizer(a.authz),
		lifecycles.WithTransactor(a.tx),
		lifecycles.WithOutboxRecorder(a.outbox),
		lifecycles.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	apps, err := applications.NewApplicationService(
		applications.WithApplicationRepo(applicationRepo),
		applications.WithLifecycleLookup(lifecycleRepo),
		applications.WithRegistryLookup(registryRepo),
		applications.WithTargetLookup(targetRepo),
		applications.WithEnvironmentLookup(envRepo),
		applications.WithWorkspaces(workspaces),
		applications.WithTeamDirectory(a.teams),
		applications.WithAuthorizer(a.authz),
		applications.WithTransactor(a.tx),
		applications.WithOutboxRecorder(a.outbox),
		applications.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	a.routes = append(a.routes,
		environments.NewEnvironmentController(envs),
		registries.NewRegistryController(regs),
		lifecycles.NewLifecycleController(lifecycleSvc),
		applications.NewApplicationController(apps),
	)
	return nil
}
