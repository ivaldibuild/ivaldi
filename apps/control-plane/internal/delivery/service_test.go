package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
)

func TestServiceNewServiceRequiresEveryCollaborator(t *testing.T) {
	f := newSvcFixture(t)
	full := f.config()

	tests := []struct {
		name  string
		unset func(*Config)
	}{
		{"Environments", func(c *Config) { c.Environments = nil }},
		{"Lifecycles", func(c *Config) { c.Lifecycles = nil }},
		{"Targets", func(c *Config) { c.Targets = nil }},
		{"Registries", func(c *Config) { c.Registries = nil }},
		{"Applications", func(c *Config) { c.Applications = nil }},
		{"Transactor", func(c *Config) { c.Transactor = nil }},
		{"Authorizer", func(c *Config) { c.Authorizer = nil }},
		{"Resources", func(c *Config) { c.Resources = nil }},
		{"Teams", func(c *Config) { c.Teams = nil }},
		{"Audit", func(c *Config) { c.Audit = nil }},
		{"Workspaces", func(c *Config) { c.Workspaces = nil }},
		{"Outbox", func(c *Config) { c.Outbox = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := full
			tt.unset(&c)
			svc, err := NewService(c)
			require.Error(t, err)
			assert.Nil(t, svc)
			assert.Contains(t, err.Error(), tt.name+" is required")
		})
	}

	t.Run("all given", func(t *testing.T) {
		svc, err := NewService(full)
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})
}

// svcAuthzCase is one Service operation and the request it must send.
type svcAuthzCase struct {
	name       string
	permission string
	action     string
	resource   func(w *svcWorld) string
	call       func(w *svcWorld) error
}

func svcAuthzCases() []svcAuthzCase {
	onWorkspace := func(w *svcWorld) string { return w.ws.ID() }
	onEnv := func(w *svcWorld) string { return w.env.ID() }
	onProject := func(w *svcWorld) string { return w.project.ID() }
	phase := func(w *svcWorld) string { return w.lifecycle.Phases()[0].ID }
	ruleID := func(w *svcWorld) string { return w.channel.Rules()[0].ID }

	return []svcAuthzCase{
		// Environments: created, listed, and restored on the Workspace; otherwise on the Environment.
		{"CreateEnvironment", PermissionEnvironmentManage, "environment.create", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), Slug: "prod", DisplayName: "Prod", Tier: TierProduction})
			return err
		}},
		{"GetEnvironment", PermissionEnvironmentRead, "environment.read", onEnv, func(w *svcWorld) error {
			_, err := w.svc.GetEnvironment(svcCtx, svcAlice, svcOrg, w.env.ID())
			return err
		}},
		{"GetEnvironmentBySlug", PermissionEnvironmentRead, "environment.read", onEnv, func(w *svcWorld) error {
			_, err := w.svc.GetEnvironmentBySlug(svcCtx, svcAlice, svcOrg, w.ws.ID(), w.env.Slug())
			return err
		}},
		{"ListEnvironments", PermissionEnvironmentRead, "environment.list", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.ListEnvironments(svcCtx, svcAlice, ListEnvironmentsQuery{OrganizationID: svcOrg, WorkspaceID: w.ws.ID()})
			return err
		}},
		{"UpdateEnvironment", PermissionEnvironmentManage, "environment.update", onEnv, func(w *svcWorld) error {
			_, err := w.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, w.env.ID(), UpdateEnvironmentProps{DisplayName: new("Staging 2")})
			return err
		}},
		{"ArchiveEnvironments", PermissionEnvironmentManage, "environment.archive", func(w *svcWorld) string { return w.spareEnv.ID() }, func(w *svcWorld) error {
			_, err := w.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{w.spareEnv.ID()})
			return err
		}},
		{"RestoreEnvironments", PermissionEnvironmentManage, "environment.restore", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{w.oldEnv.ID()})
			return err
		}},

		// Targets follow their Environment.
		{"CreateTarget", PermissionEnvironmentManage, "target.create", onEnv, func(w *svcWorld) error {
			_, err := w.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{OrganizationID: svcOrg, EnvironmentID: w.env.ID(), Slug: "worker", DisplayName: "Worker", Runtime: RuntimeKubernetes, Locator: svcLocator("worker")})
			return err
		}},
		{"GetTarget", PermissionEnvironmentRead, "target.read", onEnv, func(w *svcWorld) error {
			_, err := w.svc.GetTarget(svcCtx, svcAlice, svcOrg, w.target.ID())
			return err
		}},
		{"ListTargets", PermissionEnvironmentRead, "target.list", onEnv, func(w *svcWorld) error {
			_, err := w.svc.ListTargets(svcCtx, svcAlice, ListTargetsQuery{OrganizationID: svcOrg, EnvironmentID: w.env.ID()})
			return err
		}},
		{"UpdateTarget", PermissionEnvironmentManage, "target.update", onEnv, func(w *svcWorld) error {
			_, err := w.svc.UpdateTarget(svcCtx, svcAlice, svcOrg, w.target.ID(), UpdateTargetProps{DisplayName: new("Web 2")})
			return err
		}},
		{"AssignTargetAgent", PermissionEnvironmentManage, "target.assign_agent", onEnv, func(w *svcWorld) error {
			_, err := w.svc.AssignTargetAgent(svcCtx, svcAlice, svcOrg, w.target.ID(), svcAgent.PrincipalID)
			return err
		}},
		{"ArchiveTarget", PermissionEnvironmentManage, "target.archive", onEnv, func(w *svcWorld) error {
			_, err := w.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, w.target.ID())
			return err
		}},
		{"RestoreTarget", PermissionEnvironmentManage, "target.restore", onEnv, func(w *svcWorld) error {
			_, err := w.svc.RestoreTarget(svcCtx, svcAlice, svcOrg, w.target.ID())
			return err
		}},

		// Lifecycles are checked on their Workspace.
		{"CreateLifecycle", PermissionLifecycleManage, "lifecycle.create", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), Slug: "hotfix", DisplayName: "Hotfix"})
			return err
		}},
		{"GetLifecycle", PermissionLifecycleRead, "lifecycle.read", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.GetLifecycle(svcCtx, svcAlice, svcOrg, w.lifecycle.ID())
			return err
		}},
		{"ListLifecycles", PermissionLifecycleRead, "lifecycle.list", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.ListLifecycles(svcCtx, svcAlice, ListLifecyclesQuery{OrganizationID: svcOrg, WorkspaceID: w.ws.ID()})
			return err
		}},
		{"UpdateLifecycle", PermissionLifecycleManage, "lifecycle.update", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.UpdateLifecycle(svcCtx, svcAlice, svcOrg, w.lifecycle.ID(), UpdateLifecycleProps{DisplayName: new("Main")})
			return err
		}},
		{"AddLifecyclePhase", PermissionLifecycleManage, "lifecycle.add_phase", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.AddLifecyclePhase(svcCtx, svcAlice, svcOrg, w.lifecycle.ID(), LifecyclePhaseProps{Name: "qa", EnvironmentIDs: []string{w.spareEnv.ID()}})
			return err
		}},
		{"UpdateLifecyclePhase", PermissionLifecycleManage, "lifecycle.update_phase", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.UpdateLifecyclePhase(svcCtx, svcAlice, svcOrg, w.lifecycle.ID(), phase(w), LifecyclePhaseProps{Name: "renamed", EnvironmentIDs: []string{w.env.ID()}})
			return err
		}},
		{"MoveLifecyclePhase", PermissionLifecycleManage, "lifecycle.move_phase", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.MoveLifecyclePhase(svcCtx, svcAlice, svcOrg, w.lifecycle.ID(), phase(w), 0)
			return err
		}},
		{"RemoveLifecyclePhase", PermissionLifecycleManage, "lifecycle.remove_phase", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.RemoveLifecyclePhase(svcCtx, svcAlice, svcOrg, w.lifecycle.ID(), phase(w))
			return err
		}},
		{"ArchiveLifecycle", PermissionLifecycleManage, "lifecycle.archive", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, w.lifecycle.ID())
			return err
		}},
		{"RestoreLifecycle", PermissionLifecycleManage, "lifecycle.restore", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.RestoreLifecycle(svcCtx, svcAlice, svcOrg, w.lifecycle.ID())
			return err
		}},

		// Registries are checked on their Workspace.
		{"CreateRegistry", PermissionRegistryManage, "registry.create", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.CreateRegistry(svcCtx, svcAlice, CreateRegistryInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), Slug: "ecr", DisplayName: "ECR", Kind: RegistryOCI, URL: "https://ecr.example.com"})
			return err
		}},
		{"GetRegistry", PermissionRegistryRead, "registry.read", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.GetRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID())
			return err
		}},
		{"ListRegistries", PermissionRegistryRead, "registry.list", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.ListRegistries(svcCtx, svcAlice, ListRegistriesQuery{OrganizationID: svcOrg, WorkspaceID: w.ws.ID()})
			return err
		}},
		{"UpdateRegistry", PermissionRegistryManage, "registry.update", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.UpdateRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID(), UpdateRegistryProps{DisplayName: new("GHCR")})
			return err
		}},
		{"SetRegistryCredential", PermissionRegistryManage, "registry.set_credential", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.SetRegistryCredential(svcCtx, svcAlice, svcOrg, w.registry.ID(), "crd_1")
			return err
		}},
		{"SetRegistryLookup", PermissionRegistryManage, "registry.set_lookup", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.SetRegistryLookup(svcCtx, svcAlice, svcOrg, w.registry.ID(), LookupFromDataPlane, "pool-a")
			return err
		}},
		{"ArchiveRegistry", PermissionRegistryManage, "registry.archive", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.ArchiveRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID())
			return err
		}},
		{"RestoreRegistry", PermissionRegistryManage, "registry.restore", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.RestoreRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID())
			return err
		}},

		// Applications and what belongs to them are checked on the Project.
		{"CreateApplication", PermissionApplicationManage, "application.create", onProject, func(w *svcWorld) error {
			_, _, err := w.svc.CreateApplication(svcCtx, svcAlice, &CreateApplicationInput{OrganizationID: svcOrg, ProjectID: w.project.ID(), Slug: "worker", DisplayName: "Worker", DefaultLifecycleID: w.lifecycle.ID()})
			return err
		}},
		{"GetApplication", PermissionApplicationRead, "application.read", onProject, func(w *svcWorld) error {
			_, err := w.svc.GetApplication(svcCtx, svcAlice, svcOrg, w.app.ID())
			return err
		}},
		{"ListApplications in a project", PermissionApplicationRead, "application.list", onProject, func(w *svcWorld) error {
			_, err := w.svc.ListApplications(svcCtx, svcAlice, ListApplicationsQuery{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), ProjectID: w.project.ID()})
			return err
		}},
		{"ListApplications in a workspace", PermissionApplicationRead, "application.list", onWorkspace, func(w *svcWorld) error {
			_, err := w.svc.ListApplications(svcCtx, svcAlice, ListApplicationsQuery{OrganizationID: svcOrg, WorkspaceID: w.ws.ID()})
			return err
		}},
		{"UpdateApplication", PermissionApplicationManage, "application.update", onProject, func(w *svcWorld) error {
			_, err := w.svc.UpdateApplication(svcCtx, svcAlice, svcOrg, w.app.ID(), UpdateApplicationProps{DisplayName: new("API")})
			return err
		}},
		{"LinkApplicationSource", PermissionApplicationManage, "application.link_source", onProject, func(w *svcWorld) error {
			_, err := w.svc.LinkApplicationSource(svcCtx, svcAlice, svcOrg, w.app.ID(), "src_1")
			return err
		}},
		{"TransferApplication", PermissionApplicationManage, "application.transfer", onProject, func(w *svcWorld) error {
			_, err := w.svc.TransferApplication(svcCtx, svcAlice, svcOrg, w.app.ID(), svcOtherTeam)
			return err
		}},
		{"ArchiveApplication", PermissionApplicationManage, "application.archive", onProject, func(w *svcWorld) error {
			_, err := w.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, w.app.ID())
			return err
		}},
		{"RestoreApplication", PermissionApplicationManage, "application.restore", onProject, func(w *svcWorld) error {
			_, err := w.svc.RestoreApplication(svcCtx, svcAlice, svcOrg, w.app.ID())
			return err
		}},
		{"CreateArtifact", PermissionApplicationManage, "artifact.create", onProject, func(w *svcWorld) error {
			_, err := w.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{OrganizationID: svcOrg, ApplicationID: w.app.ID(), Name: "migrate", ArtifactKind: ArtifactContainerImage, ArtifactName: "ghcr.io/acme/migrate"})
			return err
		}},
		{"ListArtifacts", PermissionApplicationRead, "artifact.list", onProject, func(w *svcWorld) error {
			_, err := w.svc.ListArtifacts(svcCtx, svcAlice, svcOrg, w.app.ID(), false)
			return err
		}},
		{"UpdateArtifact", PermissionApplicationManage, "artifact.update", onProject, func(w *svcWorld) error {
			_, err := w.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, w.artifact.ID(), UpdateArtifactProps{Required: new(true)})
			return err
		}},
		{"SetArtifactRegistry", PermissionApplicationManage, "artifact.set_registry", onProject, func(w *svcWorld) error {
			_, err := w.svc.SetArtifactRegistry(svcCtx, svcAlice, svcOrg, w.artifact.ID(), w.registry.ID())
			return err
		}},
		{"DeclareArtifact", PermissionApplicationManage, "artifact.declare", onProject, func(w *svcWorld) error {
			_, err := w.svc.DeclareArtifact(svcCtx, svcAlice, svcOrg, w.artifact.ID())
			return err
		}},
		{"ArchiveArtifact", PermissionApplicationManage, "artifact.archive", onProject, func(w *svcWorld) error {
			_, err := w.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, w.artifact.ID())
			return err
		}},
		{"RestoreArtifact", PermissionApplicationManage, "artifact.restore", onProject, func(w *svcWorld) error {
			_, err := w.svc.RestoreArtifact(svcCtx, svcAlice, svcOrg, w.artifact.ID())
			return err
		}},
		{"CreateChannel", PermissionApplicationManage, "channel.create", onProject, func(w *svcWorld) error {
			_, err := w.svc.CreateChannel(svcCtx, svcAlice, CreateChannelInput{OrganizationID: svcOrg, ApplicationID: w.app.ID(), LifecycleID: w.lifecycle.ID(), Slug: "beta", DisplayName: "Beta"})
			return err
		}},
		{"GetChannel", PermissionApplicationRead, "channel.read", onProject, func(w *svcWorld) error {
			_, err := w.svc.GetChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
			return err
		}},
		{"ListChannels", PermissionApplicationRead, "channel.list", onProject, func(w *svcWorld) error {
			_, err := w.svc.ListChannels(svcCtx, svcAlice, svcOrg, w.app.ID(), false)
			return err
		}},
		{"UpdateChannel", PermissionApplicationManage, "channel.update", onProject, func(w *svcWorld) error {
			_, err := w.svc.UpdateChannel(svcCtx, svcAlice, svcOrg, w.channel.ID(), UpdateChannelProps{DisplayName: new("Hotfixes")})
			return err
		}},
		{"SetChannelLifecycle", PermissionApplicationManage, "channel.set_lifecycle", onProject, func(w *svcWorld) error {
			_, err := w.svc.SetChannelLifecycle(svcCtx, svcAlice, svcOrg, w.channel.ID(), w.lifecycle.ID())
			return err
		}},
		{"AddChannelRule", PermissionApplicationManage, "channel.add_rule", onProject, func(w *svcWorld) error {
			_, err := w.svc.AddChannelRule(svcCtx, svcAlice, svcOrg, w.channel.ID(), svcGitRule(nil))
			return err
		}},
		{"UpdateChannelRule", PermissionApplicationManage, "channel.update_rule", onProject, func(w *svcWorld) error {
			_, err := w.svc.UpdateChannelRule(svcCtx, svcAlice, svcOrg, w.channel.ID(), ruleID(w), svcGitRule(nil))
			return err
		}},
		{"RemoveChannelRule", PermissionApplicationManage, "channel.remove_rule", onProject, func(w *svcWorld) error {
			_, err := w.svc.RemoveChannelRule(svcCtx, svcAlice, svcOrg, w.channel.ID(), ruleID(w))
			return err
		}},
		{"SetDefaultChannel", PermissionApplicationManage, "channel.set_default", onProject, func(w *svcWorld) error {
			_, err := w.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
			return err
		}},
		{"ArchiveChannel", PermissionApplicationManage, "channel.archive", onProject, func(w *svcWorld) error {
			_, err := w.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
			return err
		}},
		{"RestoreChannel", PermissionApplicationManage, "channel.restore", onProject, func(w *svcWorld) error {
			_, err := w.svc.RestoreChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
			return err
		}},
		{"CreatePlacement", PermissionApplicationManage, "placement.create", onProject, func(w *svcWorld) error {
			tg := w.target
			_, err := w.svc.CreatePlacement(svcCtx, svcAlice, CreatePlacementInput{OrganizationID: svcOrg, ApplicationID: w.app.ID(), TargetID: tg.ID()})
			return err
		}},
		{"ListPlacements", PermissionApplicationRead, "placement.list", onProject, func(w *svcWorld) error {
			_, err := w.svc.ListPlacements(svcCtx, svcAlice, svcOrg, w.app.ID())
			return err
		}},
		{"ConfigurePlacement", PermissionApplicationManage, "placement.configure", onProject, func(w *svcWorld) error {
			_, err := w.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, w.app.ID(), w.target.ID(), nil)
			return err
		}},
		{"RemovePlacement", PermissionApplicationManage, "placement.remove", onProject, func(w *svcWorld) error {
			return w.svc.RemovePlacement(svcCtx, svcAlice, svcOrg, w.app.ID(), w.target.ID())
		}},
	}
}

func TestServiceAuthorizesEachOperation(t *testing.T) {
	for _, tt := range svcAuthzCases() {
		t.Run(tt.name, func(t *testing.T) {
			w := newSvcWorld(t)

			_ = tt.call(w) // Some calls fail on rules checked after authorizing; only the request matters here.

			reqs := w.authz.all()
			require.Len(t, reqs, 1, "one request per operation")
			req := reqs[0]
			assert.Equal(t, tt.permission, req.Permission)
			assert.Equal(t, tt.action, req.Action)
			assert.Equal(t, tt.resource(w), req.ResourceID)
			assert.Equal(t, svcOrg, req.OrganizationID)
			assert.Equal(t, svcAlice, req.Actor)
		})
	}
}

func TestServiceRefusalsPersistNothing(t *testing.T) {
	outcomes := []struct {
		outcome authorization.Outcome
		wantErr error
	}{
		{authorization.OutcomeDeny, authorization.ErrDenied},
		{authorization.OutcomeRequiresApproval, authorization.ErrApprovalRequired},
		{authorization.Outcome("unknown"), authorization.ErrDenied},
	}
	for _, o := range outcomes {
		for _, tt := range svcAuthzCases() {
			t.Run(string(o.outcome)+"/"+tt.name, func(t *testing.T) {
				w := newSvcWorld(t)
				state, events, resources := w.marks()
				txCalls := w.tx.calls
				w.authz.decide = fakeDecide(o.outcome)

				err := tt.call(w)

				require.ErrorIs(t, err, o.wantErr)
				w.requireUnchanged(t, state, events, resources)
				assert.Equal(t, txCalls, w.tx.calls, "refused before any transaction starts")
			})
		}
	}
}

func TestServiceApprovalRequiredCarriesTheDecision(t *testing.T) {
	w := newSvcWorld(t)
	w.authz.decide = fakeDecide(authorization.OutcomeRequiresApproval)

	_, err := w.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, w.env.ID(), UpdateEnvironmentProps{Tier: new(TierProduction)})

	var approval *authorization.ApprovalRequiredError
	require.ErrorAs(t, err, &approval)
	assert.Equal(t, "dec_requires_approval", approval.DecisionID, "the caller resubmits with this decision once approved")
}

func TestServiceAuditEventsCarryTheDecision(t *testing.T) {
	w := newSvcWorld(t)
	w.authz.decide = func(authorization.Request) authorization.Decision {
		return authorization.Decision{ID: "dec_approved", Outcome: authorization.OutcomeAllow, ApprovedByPrincipalID: "prn_bob"}
	}
	before := w.audit.count()

	_, err := w.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), DisplayName: "Prod", Tier: TierProduction})
	require.NoError(t, err)
	_, err = w.svc.UpdateRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID(), UpdateRegistryProps{DisplayName: new("GHCR")})
	require.NoError(t, err)
	_, err = w.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{w.spareEnv.ID()})
	require.NoError(t, err)
	_, err = w.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
	require.NoError(t, err)
	_, _, err = w.svc.CreateApplication(svcCtx, svcAlice, &CreateApplicationInput{OrganizationID: svcOrg, ProjectID: w.project.ID(), DisplayName: "Worker", DefaultLifecycleID: w.lifecycle.ID()})
	require.NoError(t, err)
	require.NoError(t, w.svc.RemovePlacement(svcCtx, svcAlice, svcOrg, w.app.ID(), w.target.ID()))

	events := w.audit.since(before)
	assert.Equal(t, []string{
		EventEnvironmentCreated, EventRegistryUpdated, EventEnvironmentArchived, EventChannelDefaultMoved,
		EventApplicationCreated, EventChannelCreated, EventPlacementRemoved,
	}, func() []string {
		out := make([]string, len(events))
		for i, e := range events {
			out[i] = e.Type
		}
		return out
	}())
	for _, e := range events {
		assert.Equal(t, "dec_approved", e.DecisionID, e.Type)
		assert.Equal(t, "prn_bob", e.ApprovedByPrincipalID, e.Type)
		assert.Equal(t, svcAlice, e.Actor, e.Type)
		assert.Equal(t, svcOrg, e.OrganizationID, e.Type)
		assert.Equal(t, svcNow, e.OccurredAt, "%s: audit records use the domain clock", e.Type)
	}
}

func TestServiceInvalidInputIsRefusedBeforeAuthorizing(t *testing.T) {
	w := newSvcWorld(t)

	_, err := w.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), DisplayName: "Prod", Tier: "live"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.CreateTarget(svcCtx, svcAlice, CreateTargetInput{OrganizationID: svcOrg, EnvironmentID: w.env.ID(), DisplayName: "Web", Runtime: "nomad"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.CreateLifecycle(svcCtx, svcAlice, CreateLifecycleInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), DisplayName: "L", Phases: []LifecyclePhaseProps{{Name: "empty"}}})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.CreateRegistry(svcCtx, svcAlice, CreateRegistryInput{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), DisplayName: "R", Kind: RegistryOCI, URL: "https://user:pass@ghcr.io"})
	require.ErrorIs(t, err, ErrInvalid)
	_, _, err = w.svc.CreateApplication(svcCtx, svcAlice, &CreateApplicationInput{OrganizationID: svcOrg, ProjectID: w.project.ID(), DisplayName: "A", DefaultLifecycleID: "not-a-lifecycle"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.CreateArtifact(svcCtx, svcAlice, CreateArtifactInput{OrganizationID: svcOrg, ApplicationID: w.app.ID(), Name: "Bad Name", ArtifactKind: ArtifactContainerImage, ArtifactName: "x"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.CreateChannel(svcCtx, svcAlice, CreateChannelInput{OrganizationID: svcOrg, ApplicationID: w.app.ID(), LifecycleID: w.lifecycle.ID(), DisplayName: "C", Rules: []ChannelRuleProps{{Kind: "nope"}}})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.CreatePlacement(svcCtx, svcAlice, CreatePlacementInput{OrganizationID: svcOrg, ApplicationID: w.app.ID(), TargetID: "not-a-target"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = w.svc.ListApplications(svcCtx, svcAlice, ListApplicationsQuery{OrganizationID: svcOrg})
	require.ErrorIs(t, err, ErrInvalid, "listing applications needs a project or a workspace")

	assert.Empty(t, w.authz.all(), "the authorizer is not asked about requests that can never succeed")
}

func TestServiceAuditFailureRollsBackTheChange(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		f := newSvcFixture(t)
		state, events, resources := f.marks()
		f.audit.err = svcErrBoom

		_, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), DisplayName: "Prod", Tier: TierProduction})

		require.ErrorIs(t, err, svcErrBoom)
		f.requireUnchanged(t, state, events, resources)
		assert.Equal(t, 1, f.tx.rollbacks)
	})
	t.Run("change", func(t *testing.T) {
		f := newSvcFixture(t)
		r := f.registry(t, "ghcr")
		state, events, resources := f.marks()
		f.audit.err = svcErrBoom

		_, err := f.svc.UpdateRegistry(svcCtx, svcAlice, svcOrg, r.ID(), UpdateRegistryProps{DisplayName: new("GHCR")})

		require.ErrorIs(t, err, svcErrBoom)
		f.requireUnchanged(t, state, events, resources)
	})
	t.Run("default channel", func(t *testing.T) {
		w := newSvcWorld(t)
		state, events, resources := w.marks()
		w.audit.err = svcErrBoom

		_, err := w.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())

		require.ErrorIs(t, err, svcErrBoom)
		w.requireUnchanged(t, state, events, resources)
	})
}

func TestServiceNoOpChangesRecordNothing(t *testing.T) {
	w := newSvcWorld(t)
	_, err := w.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, w.target.ID())
	require.NoError(t, err)
	_, err = w.svc.ArchiveRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID())
	require.NoError(t, err)
	_, err = w.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
	require.NoError(t, err)
	_, err = w.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, w.artifact.ID())
	require.NoError(t, err)
	state, events, resources := w.marks()

	ops := []struct {
		name string
		call func() error
	}{
		{"archive an archived environment", func() error {
			_, err := w.svc.ArchiveEnvironments(svcCtx, svcAlice, svcOrg, []string{w.oldEnv.ID()})
			return err
		}},
		{"restore an active environment", func() error {
			_, err := w.svc.RestoreEnvironments(svcCtx, svcAlice, svcOrg, []string{w.env.ID()})
			return err
		}},
		{"archive an archived target", func() error {
			_, err := w.svc.ArchiveTarget(svcCtx, svcAlice, svcOrg, w.target.ID())
			return err
		}},
		{"restore an active lifecycle", func() error {
			_, err := w.svc.RestoreLifecycle(svcCtx, svcAlice, svcOrg, w.lifecycle.ID())
			return err
		}},
		{"archive an archived registry", func() error {
			_, err := w.svc.ArchiveRegistry(svcCtx, svcAlice, svcOrg, w.registry.ID())
			return err
		}},
		{"restore an active application", func() error {
			_, err := w.svc.RestoreApplication(svcCtx, svcAlice, svcOrg, w.app.ID())
			return err
		}},
		{"archive an archived artifact", func() error {
			_, err := w.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, w.artifact.ID())
			return err
		}},
		{"restore an active artifact", func() error {
			_, err := w.svc.RestoreArtifact(svcCtx, svcAlice, svcOrg, w.chart.ID())
			return err
		}},
		{"archive an archived channel", func() error {
			_, err := w.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, w.channel.ID())
			return err
		}},
		{"restore an active channel", func() error {
			_, err := w.svc.RestoreChannel(svcCtx, svcAlice, svcOrg, w.defaultCh.ID())
			return err
		}},
		{"make the default channel the default", func() error {
			_, err := w.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, w.defaultCh.ID())
			return err
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			require.NoError(t, op.call())
			w.requireUnchanged(t, state, events, resources)
		})
	}
}

func TestServiceActiveDependents(t *testing.T) {
	w := newSvcWorld(t)

	t.Run("project", func(t *testing.T) {
		deps, err := w.svc.ActiveDependents(svcCtx, workspace.ArchiveTarget{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), ProjectID: w.project.ID()})
		require.NoError(t, err)
		assert.Equal(t, []workspace.Dependency{{Capability: "delivery", Kind: "Application", Count: 1}}, deps)
	})

	t.Run("workspace", func(t *testing.T) {
		deps, err := w.svc.ActiveDependents(svcCtx, workspace.ArchiveTarget{OrganizationID: svcOrg, WorkspaceID: w.ws.ID()})
		require.NoError(t, err)
		assert.Equal(t, []workspace.Dependency{
			{Capability: "delivery", Kind: "Environment", Count: 2},
			{Capability: "delivery", Kind: "Lifecycle", Count: 1},
			{Capability: "delivery", Kind: "Registry", Count: 1},
		}, deps, "archived environments are not counted")
	})

	t.Run("archived applications are not counted", func(t *testing.T) {
		_, err := w.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, w.app.ID())
		require.NoError(t, err)
		deps, err := w.svc.ActiveDependents(svcCtx, workspace.ArchiveTarget{OrganizationID: svcOrg, WorkspaceID: w.ws.ID(), ProjectID: w.project.ID()})
		require.NoError(t, err)
		assert.Equal(t, []workspace.Dependency{{Capability: "delivery", Kind: "Application", Count: 0}}, deps)
	})

	t.Run("another workspace has none", func(t *testing.T) {
		other, _ := w.addWorkspace(t, "data", "etl")
		deps, err := w.svc.ActiveDependents(svcCtx, workspace.ArchiveTarget{OrganizationID: svcOrg, WorkspaceID: other.ID()})
		require.NoError(t, err)
		for _, d := range deps {
			assert.Zero(t, d.Count, d.Kind)
		}
	})
}
