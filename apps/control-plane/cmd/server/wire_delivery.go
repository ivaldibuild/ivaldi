package server

import (
	auditpg "github.com/ivaldibuild/ivaldi-control-plane/internal/audit/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
	storegrants "github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/grants"
	credentialspg "github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials/secretstores"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/agentapi"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/applications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/clusters"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/deployments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/environments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/lifecycles"
	deliverypg "github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/registries"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/releases"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/variables"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/enrollment"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authorizer"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/approvals"
	policypg "github.com/ivaldibuild/ivaldi-control-plane/internal/policy/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	teampg "github.com/ivaldibuild/ivaldi-control-plane/internal/team/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team/teams"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	workspacepg "github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// wireAccess builds what every capability's services share: the single
// authorizer, IAM's grants followed by policy's rules; the audit recorder;
// and the approval requests that capabilities whose subjects wait open,
// with the registry of those subjects.
func (a *app) wireAccess(logger logging.Logger) error {
	grants, err := authorizer.New(
		authorizer.WithAccessRepo(iampg.NewAccessRepository(a.db)),
		authorizer.WithPlatformWorkloads(iampg.NewWorkloadRepository(a.db)),
		authorizer.WithAgentSessions(iampg.NewAgentSessionRepository(a.db)),
		authorizer.WithResourceAncestry(iampg.NewResourceRepository(a.db)),
		authorizer.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	approvalRepo := policypg.NewApprovalRequestRepository(a.db)
	a.authz = policy.NewAuthorizer(grants, approvalRepo)
	a.audit = auditpg.NewRecorder(a.db)
	a.approvalSubjects = policy.NewSubjects()
	a.approvals, err = approvals.NewApprovals(
		approvals.WithApprovalRequestRepo(approvalRepo),
		approvals.WithResourceAncestry(iampg.NewResourceRepository(a.db)),
		approvals.WithAgentSessions(iampg.NewAgentSessionRepository(a.db)),
		approvals.WithTransactor(a.tx),
		approvals.WithOutboxRecorder(a.outbox),
		approvals.WithAuditRecorder(a.audit),
	)
	return err
}

// wireTeams builds the Team capability's service and HTTP API, and the
// Directory other capabilities check owning Teams through.
func (a *app) wireTeams() error {
	repo := teampg.NewTeamRepository(a.db)
	// Workspace and delivery record owning Teams, so a Team that still owns
	// active Projects or Applications cannot be archived.
	projectOwners, err := workspace.NewTeamOwnership(workspacepg.NewRepository(a.db))
	if err != nil {
		return err
	}
	applicationOwners, err := delivery.NewTeamOwnership(deliverypg.NewApplicationRepository(a.db))
	if err != nil {
		return err
	}
	svc, err := teams.NewTeamService(
		teams.WithTeamRepo(repo),
		teams.WithOwners(projectOwners, applicationOwners),
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
	a.services.teams = svc
	return nil
}

// wireDelivery builds delivery's services and their HTTP APIs.
func (a *app) wireDelivery(runners config.Runners) error {
	envRepo := deliverypg.NewEnvironmentRepository(a.db)
	lifecycleRepo := deliverypg.NewLifecycleRepository(a.db)
	applicationRepo := deliverypg.NewApplicationRepository(a.db)
	registryRepo := deliverypg.NewRegistryRepository(a.db)
	targetRepo := deliverypg.NewTargetRepository(a.db)
	clusterRepo := deliverypg.NewClusterRepository(a.db)
	variableRepo := deliverypg.NewVariableRepository(a.db)
	workspaces := workspace.NewLookups(workspacepg.NewRepository(a.db))

	// Secret Stores belong to the credentials capability; delivery reads
	// them through a lookup, and credentials asks delivery whether a store
	// is still referred to.
	storeRepo := credentialspg.NewSecretStoreRepository(a.db)
	storeLookup, err := credentials.NewStoreLookup(storeRepo, a.federation != nil)
	if err != nil {
		return err
	}
	stores, err := secretstores.NewSecretStoreService(
		secretstores.WithSecretStoreRepo(storeRepo),
		secretstores.WithWorkspaces(workspaces),
		secretstores.WithEnvironmentLookup(envRepo),
		secretstores.WithVariableUsage(variableRepo),
		secretstores.WithFederation(a.federation != nil),
		secretstores.WithAuthorizer(a.authz),
		secretstores.WithTransactor(a.tx),
		secretstores.WithOutboxRecorder(a.outbox),
		secretstores.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	// A Variable's save and an Application's restore check the same
	// records, under their rows' locks.
	scopes, err := delivery.NewVariableScopeChecker(targetRepo, envRepo, applicationRepo, storeLookup)
	if err != nil {
		return err
	}

	revisionRepo := deliverypg.NewReleaseVariablesRepository(a.db)
	snapshots, err := delivery.NewReleaseVariablesTaker(variableRepo, revisionRepo, database.NewLocker(a.db))
	if err != nil {
		return err
	}

	// Deployments that wait for approval are built from the same options as
	// the deployment service: a decided request applies or refuses its
	// Deployment, and withdrawing a Release or archiving a Target or
	// Application cancels the Deployments waiting there.
	deploymentOpts := []deployments.ServiceOption{
		deployments.WithDeploymentRepo(deliverypg.NewDeploymentRepository(a.db)),
		deployments.WithReleaseRepo(deliverypg.NewReleaseRepository(a.db)),
		deployments.WithBuildRepo(deliverypg.NewBuildRepository(a.db)),
		deployments.WithApplications(applicationRepo),
		deployments.WithEnvironments(envRepo),
		deployments.WithTargets(targetRepo),
		deployments.WithClusters(clusterRepo),
		deployments.WithHeartbeatInterval(runners.HeartbeatInterval),
		deployments.WithLifecycles(lifecycleRepo),
		deployments.WithVariables(revisionRepo),
		deployments.WithVariableSnapshots(snapshots),
		deployments.WithSecretStores(storeLookup),
		deployments.WithCommitHistory(a.commitHistory),
		deployments.WithChanges(a.changes),
		deployments.WithApprovals(a.approvals),
		deployments.WithDeploymentLocks(deliverypg.NewDeploymentLocks(a.db)),
		deployments.WithAuthorizer(a.authz),
		deployments.WithTransactor(a.tx),
		deployments.WithOutboxRecorder(a.outbox),
		deployments.WithAuditRecorder(a.audit),
	}
	waiting, err := deployments.NewWaitingDeploymentCanceller(deploymentOpts...)
	if err != nil {
		return err
	}
	approvalHandler, err := deployments.NewApprovalHandler(deploymentOpts...)
	if err != nil {
		return err
	}
	if err := a.approvalSubjects.Register(policy.SubjectType{Type: delivery.SubjectDeployment, Handler: approvalHandler}); err != nil {
		return err
	}

	revoker, err := enrollment.NewAgentRevoker(
		iampg.NewPrincipalRepository(a.db),
		iampg.NewWorkloadRepository(a.db),
		iampg.NewWorkloadCredentialRepository(a.db),
		change.Writer{Outbox: a.outbox},
	)
	if err != nil {
		return err
	}

	clusterSvc, err := clusters.NewClusterService(
		clusters.WithClusterRepo(clusterRepo),
		clusters.WithAgentRevoker(revoker),
		clusters.WithAuthorizer(a.authz),
		clusters.WithTransactor(a.tx),
		clusters.WithOutboxRecorder(a.outbox),
		clusters.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	envs, err := environments.NewEnvironmentService(
		environments.WithEnvironmentRepo(envRepo),
		environments.WithTargetRepo(targetRepo),
		environments.WithClusterRepo(clusterRepo),
		environments.WithLifecycleUsage(lifecycleRepo),
		environments.WithVariableUsage(variableRepo),
		environments.WithWorkspaces(workspaces),
		environments.WithResourceRegistrar(a.iam.resources),
		environments.WithWaitingDeployments(waiting),
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
		applications.WithProjectLookup(workspaces),
		applications.WithVariableUsage(variableRepo),
		applications.WithVariableSet(variableRepo),
		applications.WithVariableScopes(scopes),
		applications.WithWorkspaces(workspaces),
		applications.WithTeamDirectory(a.teams),
		applications.WithSettingResolver(a.settingResolver),
		applications.WithLocker(database.NewLocker(a.db)),
		applications.WithWaitingDeployments(waiting),
		applications.WithAuthorizer(a.authz),
		applications.WithTransactor(a.tx),
		applications.WithOutboxRecorder(a.outbox),
		applications.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	vars, err := variables.NewVariableService(
		variables.WithVariableRepo(variableRepo),
		variables.WithApplicationLookup(applicationRepo),
		variables.WithTargetLookup(targetRepo),
		variables.WithVariableScopes(scopes),
		variables.WithWorkspaces(workspaces),
		variables.WithLocker(database.NewLocker(a.db)),
		variables.WithAuthorizer(a.authz),
		variables.WithTransactor(a.tx),
		variables.WithOutboxRecorder(a.outbox),
		variables.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	// Tags cannot be resolved to digests until a registry client exists, so
	// Builds are named by digest.
	rels, err := releases.NewReleaseService(
		releases.WithBuildRepo(deliverypg.NewBuildRepository(a.db)),
		releases.WithReleaseRepo(deliverypg.NewReleaseRepository(a.db)),
		releases.WithApplications(applicationRepo),
		releases.WithTagResolver(delivery.NoTagResolver()),
		releases.WithVariableSnapshots(snapshots),
		releases.WithCurrentVariables(revisionRepo),
		releases.WithTargetLookup(targetRepo),
		releases.WithDeploymentLookup(deliverypg.NewDeploymentRepository(a.db)),
		releases.WithLifecycles(lifecycleRepo),
		releases.WithEnvironments(envRepo),
		releases.WithWaitingDeployments(waiting),
		releases.WithAuthorizer(a.authz),
		releases.WithTransactor(a.tx),
		releases.WithOutboxRecorder(a.outbox),
		releases.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}

	deps, err := deployments.NewService(deploymentOpts...)
	if err != nil {
		return err
	}

	agentSvc, err := agentapi.NewService(
		agentapi.WithClusters(clusterRepo),
		agentapi.WithTargets(targetRepo),
		agentapi.WithApplications(applicationRepo),
		agentapi.WithDeployments(deliverypg.NewDeploymentRepository(a.db)),
		agentapi.WithAssignments(deps),
		agentapi.WithObservations(deps),
		agentapi.WithTransactor(a.tx),
		agentapi.WithIntervals(agentapi.Intervals{
			Heartbeat: runners.HeartbeatInterval,
			Poll:      runners.AgentPollInterval,
			Watch:     runners.AgentWatchInterval,
		}),
	)
	if err != nil {
		return err
	}

	a.services.environments, a.services.registries, a.services.lifecycles = envs, regs, lifecycleSvc
	a.services.clusters = clusterSvc
	a.services.agent = agentSvc
	a.services.applications, a.services.releases = apps, rels
	a.services.deployments = deps

	// Grants are credentials': it reads the Deployment's assignment from
	// delivery, which never imports it.
	a.services.secretGrants, err = storegrants.NewService(
		storegrants.WithAssignments(deps),
		storegrants.WithApplications(applicationRepo),
		storegrants.WithGrantRepo(credentialspg.NewSecretStoreGrantRepository(a.db)),
		storegrants.WithIssuer(a.federation),
		storegrants.WithTransactor(a.tx),
		storegrants.WithAuditRecorder(a.audit),
	)
	if err != nil {
		return err
	}
	a.services.variables = vars
	a.services.secretStores = stores
	return nil
}
