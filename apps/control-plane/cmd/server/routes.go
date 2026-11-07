package server

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/applications"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/deployments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/environments"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/lifecycles"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/registries"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery/releases"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/authhttp"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/grants"
	iamhttp "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/httpapi"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/organizations"
	peopleapi "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/people"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/profile"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/serviceaccounts"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration/agents"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/locks"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team/teams"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/projects"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/settings"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace/workspaces"
)

// services are what the API's operations are served by. The wiring fills
// them in. Registering an operation never calls its service, so the API's
// description (OpenAPI) is built from services with none set, from the same
// list of controllers the server serves.
type services struct {
	// onboarding is set only where anyone may create an Organization: the
	// managed cloud, with built-in accounts.
	onboarding iam.Onboarding
	// signIn is the authorization server's JSON operations for the
	// frontend's sign-in screens.
	signIn api.Routes

	profile         iam.ProfileService
	organizations   iam.OrganizationService
	people          iam.PeopleService
	grants          iam.GrantService
	serviceAccounts iam.ServiceAccountService
	teams           team.TeamService
	agents          orchestration.AgentService
	settingLocks    policy.SettingLockService
	workspaces      workspace.WorkspaceService
	projects        workspace.ProjectService
	settings        workspace.SettingService
	environments    delivery.EnvironmentService
	registries      delivery.RegistryService
	lifecycles      delivery.LifecycleService
	applications    delivery.ApplicationService
	releases        delivery.ReleaseService
	deployments     delivery.DeploymentService
}

// routes returns a controller for every /v1 operation. Onboarding is served
// only when it is set, and described always.
func (s services) routes(describe bool) []api.Routes {
	out := []api.Routes{
		profile.NewProfileController(s.profile),
		organizations.NewOrganizationController(s.organizations),
		peopleapi.NewPeopleController(s.people),
		grants.NewGrantController(s.grants),
		serviceaccounts.NewController(s.serviceAccounts),
		teams.NewTeamController(s.teams),
		agents.NewController(s.agents),
		locks.NewSettingLockController(s.settingLocks),
		workspaces.NewWorkspaceController(s.workspaces),
		projects.NewProjectController(s.projects),
		settings.NewSettingController(s.settings),
		environments.NewEnvironmentController(s.environments),
		registries.NewRegistryController(s.registries),
		lifecycles.NewLifecycleController(s.lifecycles),
		applications.NewApplicationController(s.applications),
		releases.NewReleaseController(s.releases),
		deployments.NewController(s.deployments),
	}
	if s.onboarding != nil || describe {
		out = append(out, iamhttp.NewOnboardingController(s.onboarding))
	}
	if s.signIn != nil {
		out = append(out, s.signIn)
	} else if describe {
		out = append(out, authhttp.Routes())
	}
	return out
}
