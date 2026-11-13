package delivery

// The interfaces this capability defines for itself: its repositories, the
// lookups it needs from the workspace capability, and its services.
// Contracts shared with other capabilities live with their owners.

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// Repositories persist and retrieve domain objects, and nothing more: IDs,
// timestamps, and every rule come from the domain. Every method is scoped to
// an Organization. Methods return ErrNotFound when no row matches, and
// ErrSlugTaken for a slug clash. Update methods write the whole object as
// its Snapshot gives it; the fields that identify it, and what it belongs
// to, never change.
//
// List methods sort by the fields their query documents, with the ID
// breaking ties, and return ErrInvalid for an unknown field or a cursor from
// another sort.

// ListEnvironmentsQuery lists a Workspace's Environments. It sorts by
// "created_at" (the default), "slug", or "display_name".
type ListEnvironmentsQuery struct {
	OrganizationID string
	WorkspaceID    string
	// IncludeArchived specifies whether to include archived environments in the results.
	IncludeArchived bool
	Page            pagination.Query
}

type EnvironmentRepository interface {
	// CreateEnvironment creates a new environment in the repository.
	CreateEnvironment(ctx context.Context, env *Environment) error
	// GetEnvironment retrieves an environment by its ID.
	GetEnvironment(ctx context.Context, organizationID, environmentID string) (*Environment, error)
	// GetEnvironmentForUpdate is GetEnvironment that also locks the row until
	// the transaction ends. It must be called inside a transaction.
	GetEnvironmentForUpdate(ctx context.Context, organizationID, environmentID string) (*Environment, error)
	// GetEnvironmentBySlug retrieves the active environment with the slug in a workspace.
	GetEnvironmentBySlug(ctx context.Context, organizationID, workspaceID, slug string) (*Environment, error)
	// GetEnvironments retrieves the environments with the given IDs, in no
	// particular order. IDs that match nothing are left out.
	GetEnvironments(ctx context.Context, organizationID string, environmentIDs []string) ([]*Environment, error)
	// ListEnvironments lists environments based on the provided query parameters.
	ListEnvironments(ctx context.Context, query ListEnvironmentsQuery) (*pagination.Result[*Environment], error)
	// ListActiveEnvironments returns a Workspace's active Environments,
	// ordered by creation time and then ID. They are few, so they are
	// returned whole rather than paged.
	ListActiveEnvironments(ctx context.Context, organizationID, workspaceID string) ([]*Environment, error)
	// UpdateEnvironment writes the environment's state.
	UpdateEnvironment(ctx context.Context, env *Environment) error
	// CountActiveEnvironments counts a workspace's active environments.
	CountActiveEnvironments(ctx context.Context, organizationID, workspaceID string) (int, error)
}

// ListLifecyclesQuery lists a Workspace's Lifecycles. It sorts by
// "created_at" (the default), "slug", or "display_name".
type ListLifecyclesQuery struct {
	OrganizationID  string
	WorkspaceID     string
	IncludeArchived bool
	Page            pagination.Query
}

// LifecycleRepository stores Lifecycles with their phases.
type LifecycleRepository interface {
	CreateLifecycle(ctx context.Context, l *Lifecycle) error
	GetLifecycle(ctx context.Context, organizationID, lifecycleID string) (*Lifecycle, error)
	// GetLifecycleForUpdate is GetLifecycle that also locks the row until the
	// transaction ends. It must be called inside a transaction.
	GetLifecycleForUpdate(ctx context.Context, organizationID, lifecycleID string) (*Lifecycle, error)
	GetLifecycleBySlug(ctx context.Context, organizationID, workspaceID, slug string) (*Lifecycle, error)
	ListLifecycles(ctx context.Context, query ListLifecyclesQuery) (*pagination.Result[*Lifecycle], error)
	// UpdateLifecycle writes the lifecycle's state, including adding,
	// changing, reordering, and removing its phases.
	UpdateLifecycle(ctx context.Context, l *Lifecycle) error
	CountActiveLifecycles(ctx context.Context, organizationID, workspaceID string) (int, error)
	// CountActiveLifecyclesWithEnvironment counts the active lifecycles with
	// the environment in one of their phases.
	CountActiveLifecyclesWithEnvironment(ctx context.Context, organizationID, environmentID string) (int, error)
}

// ListTargetsQuery lists an Environment's Targets. It sorts by "created_at"
// (the default), "slug", or "display_name".
type ListTargetsQuery struct {
	OrganizationID  string
	EnvironmentID   string
	IncludeArchived bool
	Page            pagination.Query
}

type TargetRepository interface {
	CreateTarget(ctx context.Context, t *Target) error
	GetTarget(ctx context.Context, organizationID, targetID string) (*Target, error)
	// GetTargetForUpdate is GetTarget that also locks the row until the
	// transaction ends.
	GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*Target, error)
	// GetTargetByAgentPrincipalID returns the active Target the workload
	// principal serves.
	GetTargetByAgentPrincipalID(ctx context.Context, organizationID, principalID string) (*Target, error)
	GetTargetBySlug(ctx context.Context, organizationID, environmentID, slug string) (*Target, error)
	ListTargets(ctx context.Context, query ListTargetsQuery) (*pagination.Result[*Target], error)
	UpdateTarget(ctx context.Context, t *Target) error
	CountActiveTargets(ctx context.Context, organizationID, environmentID string) (int, error)
}

// ListRegistriesQuery lists a Workspace's Registries. It sorts by
// "created_at" (the default), "slug", or "display_name".
type ListRegistriesQuery struct {
	OrganizationID  string
	WorkspaceID     string
	IncludeArchived bool
	Page            pagination.Query
}

type RegistryRepository interface {
	CreateRegistry(ctx context.Context, r *Registry) error
	GetRegistry(ctx context.Context, organizationID, registryID string) (*Registry, error)
	GetRegistryBySlug(ctx context.Context, organizationID, workspaceID, slug string) (*Registry, error)
	ListRegistries(ctx context.Context, query ListRegistriesQuery) (*pagination.Result[*Registry], error)
	UpdateRegistry(ctx context.Context, r *Registry) error
	CountActiveRegistries(ctx context.Context, organizationID, workspaceID string) (int, error)
}

// ListApplicationsQuery lists Applications in a Project, or in a Workspace
// when ProjectID is empty, optionally only one Team's. It sorts by
// "created_at" (the default), "slug", or "display_name".
type ListApplicationsQuery struct {
	OrganizationID  string
	WorkspaceID     string
	ProjectID       string
	OwnerTeamID     string
	IncludeArchived bool
	Page            pagination.Query
}

// ApplicationRepository stores Applications and the records that belong to
// them: artifacts, Channels with their rules, and placements on Targets. An
// Application's artifacts, Channels, and placements are few, so they are
// listed whole rather than paged.
type ApplicationRepository interface {
	CreateApplication(ctx context.Context, a *Application) error
	GetApplication(ctx context.Context, organizationID, applicationID string) (*Application, error)
	// GetApplicationForUpdate is GetApplication that also locks the row until
	// the transaction ends. It must be called inside a transaction.
	GetApplicationForUpdate(ctx context.Context, organizationID, applicationID string) (*Application, error)
	GetApplicationBySlug(ctx context.Context, organizationID, projectID, slug string) (*Application, error)
	// GetApplicationByIdentifier finds an Application by its ID, archived or
	// not, or by the slug of an active one. Slugs are unique only within a
	// Project, so a slug two Projects use returns ErrAmbiguous.
	GetApplicationByIdentifier(ctx context.Context, organizationID, identifier string) (*Application, error)
	ListApplications(ctx context.Context, query ListApplicationsQuery) (*pagination.Result[*Application], error)
	UpdateApplication(ctx context.Context, a *Application) error
	CountActiveApplications(ctx context.Context, organizationID, projectID string) (int, error)
	// CountActiveApplicationsOwnedBy counts the active Applications a Team owns.
	CountActiveApplicationsOwnedBy(ctx context.Context, organizationID, teamID string) (int, error)

	CreateArtifact(ctx context.Context, a *Artifact) error
	GetArtifact(ctx context.Context, organizationID, artifactID string) (*Artifact, error)
	ListArtifacts(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*Artifact, error)
	UpdateArtifact(ctx context.Context, a *Artifact) error

	// CreateChannel stores a Channel with its rules.
	CreateChannel(ctx context.Context, c *Channel) error
	GetChannel(ctx context.Context, organizationID, channelID string) (*Channel, error)
	GetDefaultChannel(ctx context.Context, organizationID, applicationID string) (*Channel, error)
	ListChannels(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*Channel, error)
	// UpdateChannel writes the channel's state, including adding, changing,
	// and removing its rules. It returns ErrConflict if it would make a
	// second default Channel for the Application.
	UpdateChannel(ctx context.Context, c *Channel) error
	// CountActiveChannelsWithLifecycle counts the active channels of active
	// applications that follow the lifecycle.
	CountActiveChannelsWithLifecycle(ctx context.Context, organizationID, lifecycleID string) (int, error)

	// CreatePlacement returns ErrAlreadyExists if the Application is already on the Target.
	CreatePlacement(ctx context.Context, p *Placement) error
	GetPlacement(ctx context.Context, organizationID, applicationID, targetID string) (*Placement, error)
	ListPlacements(ctx context.Context, organizationID, applicationID string) ([]*Placement, error)
	// ListRunsOn reads where the Applications run: each placement with its
	// Target, Environment, and current Release and Deployment. Archived
	// Targets and Environments are left out. It is a read model, so it
	// returns one row per placement rather than domain objects.
	ListRunsOn(ctx context.Context, organizationID string, applicationIDs []string) ([]PlacementRunsOn, error)
	// UpdatePlacement writes the placement's configuration. It leaves the
	// current Deployment and Release alone.
	UpdatePlacement(ctx context.Context, p *Placement) error
	// UpdatePlacementCurrent writes the Deployment and Release now running on
	// the Target, and the placement's updated time.
	UpdatePlacementCurrent(ctx context.Context, p *Placement) error
	// DeletePlacement returns ErrInUse if Deployments refer to the placement.
	DeletePlacement(ctx context.Context, organizationID, applicationID, targetID string) error
}

// Workspaces looks up Workspaces and Projects in the workspace capability,
// without authorizing, to check the boundaries delivery records must keep.
// workspace.NewLookups implements it.
type Workspaces interface {
	LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*workspace.Workspace, error)
	LookupProject(ctx context.Context, organizationID, projectID string) (*workspace.Project, error)
}

// Each service interface below is implemented by one sub-package, such as
// environments for EnvironmentService, which also serves it over HTTP. A
// sub-package's constructor returns its service wrapped in an authorizing
// layer that checks every operation with the single authorizer before it
// runs, so the interface is the only way in.

// EnvironmentService manages Environments and the Targets they run on.
type EnvironmentService interface {
	CreateEnvironment(ctx context.Context, actor identity.Actor, in CreateEnvironmentInput) (*Environment, error)
	GetEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string) (*Environment, error)
	GetEnvironmentBySlug(ctx context.Context, actor identity.Actor, organizationID, workspaceID, slug string) (*Environment, error)
	ListEnvironments(ctx context.Context, actor identity.Actor, query ListEnvironmentsQuery) (*pagination.Result[*Environment], error)
	UpdateEnvironment(ctx context.Context, actor identity.Actor, organizationID, environmentID string, props UpdateEnvironmentProps) (*Environment, error)
	// ArchiveEnvironments archives the Environments together, or none of them.
	ArchiveEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*Environment, error)
	// RestoreEnvironments restores the Environments together, or none of them.
	RestoreEnvironments(ctx context.Context, actor identity.Actor, organizationID string, environmentIDs []string) ([]*Environment, error)

	CreateTarget(ctx context.Context, actor identity.Actor, in CreateTargetInput) (*Target, error)
	GetTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*Target, error)
	ListTargets(ctx context.Context, actor identity.Actor, query ListTargetsQuery) (*pagination.Result[*Target], error)
	UpdateTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string, props UpdateTargetProps) (*Target, error)
	// AssignTargetAgent makes a runtime agent's workload principal the only
	// one that may report on the Target; an empty principalID unassigns it.
	AssignTargetAgent(ctx context.Context, actor identity.Actor, organizationID, targetID, principalID string) (*Target, error)
	// RecordTargetReport notes that the Target's agent, the actor, is in contact.
	RecordTargetReport(ctx context.Context, actor identity.Actor, organizationID, targetID string) error
	ArchiveTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*Target, error)
	RestoreTarget(ctx context.Context, actor identity.Actor, organizationID, targetID string) (*Target, error)
}

// CreateEnvironmentInput describes a new Environment.
type CreateEnvironmentInput struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug             string
	DisplayName      string
	Tier             Tier
	ExecutionEnabled bool
}

// CreateTargetInput describes a new Target.
type CreateTargetInput struct {
	OrganizationID string
	EnvironmentID  string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Runtime     Runtime
	Locator     map[string]string
	// AgentPrincipalID is the runtime agent that serves the Target, if known yet.
	AgentPrincipalID string
}

// LifecycleService manages Lifecycles and their phases.
type LifecycleService interface {
	CreateLifecycle(ctx context.Context, actor identity.Actor, in CreateLifecycleInput) (*Lifecycle, error)
	GetLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*Lifecycle, error)
	ListLifecycles(ctx context.Context, actor identity.Actor, query ListLifecyclesQuery) (*pagination.Result[*Lifecycle], error)
	UpdateLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props UpdateLifecycleProps) (*Lifecycle, error)
	AddLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string, props LifecyclePhaseProps) (*Lifecycle, error)
	UpdateLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, props LifecyclePhaseProps) (*Lifecycle, error)
	MoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string, position int) (*Lifecycle, error)
	RemoveLifecyclePhase(ctx context.Context, actor identity.Actor, organizationID, lifecycleID, phaseID string) (*Lifecycle, error)
	ArchiveLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*Lifecycle, error)
	RestoreLifecycle(ctx context.Context, actor identity.Actor, organizationID, lifecycleID string) (*Lifecycle, error)
}

// CreateLifecycleInput describes a new Lifecycle and its phases, in order.
type CreateLifecycleInput struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description string
	Phases      []LifecyclePhaseProps
}

// RegistryService manages Registries.
type RegistryService interface {
	CreateRegistry(ctx context.Context, actor identity.Actor, in CreateRegistryInput) (*Registry, error)
	GetRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*Registry, error)
	ListRegistries(ctx context.Context, actor identity.Actor, query ListRegistriesQuery) (*pagination.Result[*Registry], error)
	UpdateRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string, props UpdateRegistryProps) (*Registry, error)
	// SetRegistryCredential points the Registry at a credential; an empty
	// credentialRef makes it anonymous.
	SetRegistryCredential(ctx context.Context, actor identity.Actor, organizationID, registryID, credentialRef string) (*Registry, error)
	SetRegistryLookup(ctx context.Context, actor identity.Actor, organizationID, registryID string, from LookupFrom, pool string) (*Registry, error)
	ArchiveRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*Registry, error)
	RestoreRegistry(ctx context.Context, actor identity.Actor, organizationID, registryID string) (*Registry, error)
}

// CreateRegistryInput describes a new Registry.
type CreateRegistryInput struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug          string
	DisplayName   string
	Kind          RegistryKind
	URL           string
	CredentialRef string
	// LookupFrom defaults to LookupFromControlPlane.
	LookupFrom LookupFrom
	LookupPool string
}

// ApplicationService manages Applications, their artifacts, their Channels,
// and their placements on Targets.
//
// An applicationID, as an argument or in an input, may also be the slug of
// an active Application: the authorizing layer resolves it with
// ApplicationRepository.GetApplicationByIdentifier and passes the ID on, so
// the decision and the change are about the same Application.
type ApplicationService interface {
	// CreateApplication creates an Application with its default Channel.
	CreateApplication(ctx context.Context, actor identity.Actor, in CreateApplicationInput) (*Application, *Channel, error)
	GetApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*Application, error)
	ListApplications(ctx context.Context, actor identity.Actor, query ListApplicationsQuery) (*pagination.Result[*Application], error)
	UpdateApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string, props UpdateApplicationProps) (*Application, error)
	// LinkApplicationSource records the Source the Application is built
	// from; an empty sourceID removes it.
	LinkApplicationSource(ctx context.Context, actor identity.Actor, organizationID, applicationID, sourceID string) (*Application, error)
	TransferApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID, ownerTeamID string) (*Application, error)
	ArchiveApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*Application, error)
	RestoreApplication(ctx context.Context, actor identity.Actor, organizationID, applicationID string) (*Application, error)

	CreateArtifact(ctx context.Context, actor identity.Actor, in CreateArtifactInput) (*Artifact, error)
	ListArtifacts(ctx context.Context, actor identity.Actor, organizationID, applicationID string, includeArchived bool) ([]*Artifact, error)
	UpdateArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string, props UpdateArtifactProps) (*Artifact, error)
	// SetArtifactRegistry records the Registry an artifact is looked up in;
	// an empty registryID removes it.
	SetArtifactRegistry(ctx context.Context, actor identity.Actor, organizationID, artifactID, registryID string) (*Artifact, error)
	DeclareArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*Artifact, error)
	ArchiveArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*Artifact, error)
	RestoreArtifact(ctx context.Context, actor identity.Actor, organizationID, artifactID string) (*Artifact, error)

	CreateChannel(ctx context.Context, actor identity.Actor, in CreateChannelInput) (*Channel, error)
	GetChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error)
	ListChannels(ctx context.Context, actor identity.Actor, organizationID, applicationID string, includeArchived bool) ([]*Channel, error)
	UpdateChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string, props UpdateChannelProps) (*Channel, error)
	SetChannelLifecycle(ctx context.Context, actor identity.Actor, organizationID, channelID, lifecycleID string) (*Channel, error)
	AddChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID string, props ChannelRuleProps) (*Channel, error)
	UpdateChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string, props ChannelRuleProps) (*Channel, error)
	RemoveChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string) (*Channel, error)
	// SetDefaultChannel makes an active Channel its Application's default, in
	// place of the current one.
	SetDefaultChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error)
	ArchiveChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error)
	RestoreChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error)

	CreatePlacement(ctx context.Context, actor identity.Actor, in CreatePlacementInput) (*Placement, error)
	ListPlacements(ctx context.Context, actor identity.Actor, organizationID, applicationID string) ([]*Placement, error)
	// RunsOn reads where the Workspace's Applications run, page by
	// Application: their Project and owning Team names, and the Targets each
	// is placed on with the Release and Deployment current there.
	RunsOn(ctx context.Context, actor identity.Actor, query RunsOnQuery) (*RunsOnPage, error)
	// ConfigurePlacement sets how Releases are applied to the Target; nil
	// removes it, so Deployments there are tracked only.
	ConfigurePlacement(ctx context.Context, actor identity.Actor, organizationID, applicationID, targetID string, config *ApplyConfig) (*Placement, error)
	RemovePlacement(ctx context.Context, actor identity.Actor, organizationID, applicationID, targetID string) error
}

// CreateApplicationInput describes a new Application and the Lifecycle its
// default Channel follows.
type CreateApplicationInput struct {
	OrganizationID string
	ProjectID      string
	// OwnerTeamID defaults to the Project's owning Team.
	OwnerTeamID string
	// Slug is derived from DisplayName when empty.
	Slug             string
	DisplayName      string
	SourceID         string
	WorkloadSelector map[string]string
	// DefaultLifecycleID is the Lifecycle the default Channel follows. It
	// must be active and in the Project's Workspace. When it is empty, the
	// delivery.default_lifecycle setting resolved for the Project names it.
	DefaultLifecycleID string
}

// CreateArtifactInput describes an artifact someone declares for an Application.
type CreateArtifactInput struct {
	OrganizationID string
	ApplicationID  string
	Name           string
	ArtifactKind   ArtifactKind
	ArtifactName   string
	// RegistryID must be an active Registry in the Application's Workspace.
	RegistryID string
	Required   bool
}

// CreateChannelInput describes a new, non-default Channel.
type CreateChannelInput struct {
	OrganizationID string
	ApplicationID  string
	// LifecycleID must be an active Lifecycle in the Application's Workspace.
	LifecycleID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description string
	Rules       []ChannelRuleProps
}

// CreatePlacementInput places an Application on a Target.
type CreatePlacementInput struct {
	OrganizationID string
	ApplicationID  string
	TargetID       string
	// ApplyConfig says how Ivaldi applies Releases there; nil means
	// Deployments there are tracked only, until it is configured.
	ApplyConfig *ApplyConfig
}

// BuildRepository stores Builds. A Build belongs to the Organization, not to
// an Application: every Application that releases an artifact name shares
// its Builds.
type BuildRepository interface {
	// CreateBuild inserts the Build unless one with its artifact name and
	// digest exists, and reports whether it inserted it. It never fails
	// because of that duplicate, so a caller in a transaction can then read
	// the stored one.
	CreateBuild(ctx context.Context, b *Build) (bool, error)
	GetBuild(ctx context.Context, organizationID, buildID string) (*Build, error)
	// GetBuildByDigestForUpdate returns the Build of artifactName with
	// digest, locked until the transaction ends, or ErrNotFound.
	GetBuildByDigestForUpdate(ctx context.Context, organizationID, artifactName, digest string) (*Build, error)
	// GetBuilds returns the Builds with the IDs, in no particular order,
	// leaving out IDs that match none.
	GetBuilds(ctx context.Context, organizationID string, buildIDs []string) ([]*Build, error)
	// UpdateBuild writes a Build's provenance and CI details.
	UpdateBuild(ctx context.Context, b *Build) error
}

// ReleaseRepository stores Releases with their Builds.
type ReleaseRepository interface {
	// CreateRelease inserts the Release and its Builds. A version the
	// Application already has is ErrAlreadyExists.
	CreateRelease(ctx context.Context, r *Release) error
	GetRelease(ctx context.Context, organizationID, releaseID string) (*Release, error)
	// GetReleaseForUpdate is GetRelease that also locks the Release until
	// the transaction ends.
	GetReleaseForUpdate(ctx context.Context, organizationID, releaseID string) (*Release, error)
	ListReleases(ctx context.Context, query ListReleasesQuery) (*pagination.Result[*Release], error)
	// UpdateRelease writes a Release's status, publish decision, and
	// timestamps. Its Channel, version, and Builds never change.
	UpdateRelease(ctx context.Context, r *Release) error
}

// ListReleasesQuery lists an Application's Releases.
type ListReleasesQuery struct {
	OrganizationID string
	ApplicationID  string
	// ChannelID keeps the Releases in that Channel.
	ChannelID string
	// Status keeps the Releases with that status.
	Status release.Status
	// Page sorts by "created_at" (the default) or "version", with the ID
	// breaking ties. Versions sort as text.
	Page pagination.Query
}

// TagResolver resolves a tag to a digest through the artifact's Registry.
// It is Ivaldi's interface so the registry client behind it can change.
type TagResolver interface {
	ResolveTag(ctx context.Context, organizationID string, artifact *Artifact, tag string) (string, error)
}

// NoTagResolver refuses every tag with ErrTagsUnsupported. It stands in
// until a registry client resolves tags.
func NoTagResolver() TagResolver { return noTagResolver{} }

type noTagResolver struct{}

func (noTagResolver) ResolveTag(context.Context, string, *Artifact, string) (string, error) {
	return "", ErrTagsUnsupported
}

// BuildInput names one artifact's Build: by digest, or by tag, which is
// resolved to a digest. CI fills in the details it has.
type BuildInput struct {
	// Artifact is the name of one of the Application's artifacts, such as "api".
	Artifact string
	Digest   string
	Tag      string
	Details  BuildDetails
}

// ReportBuildInput reports a Build of one of an Application's artifacts.
type ReportBuildInput struct {
	OrganizationID string
	ApplicationID  string
	Build          BuildInput
}

// CreateReleaseInput creates a Release of an Application from its Builds.
type CreateReleaseInput struct {
	OrganizationID string
	ApplicationID  string
	Version        string
	// ChannelID names the Channel to join. Empty means the one active
	// Channel whose rules the Release meets.
	ChannelID string
	Builds    []BuildInput
	// Publish publishes the Release as soon as it is created, such as from
	// a tag pipeline. It needs the permission to publish.
	Publish bool
	// TriggeredBy and TriggeredByEmail are who the caller says triggered
	// the Release, such as the CI user who ran the pipeline. They are
	// recorded as reported and not verified.
	TriggeredBy      string
	TriggeredByEmail string
}

// ReleaseDetail is a Release with the Builds it carries, in the order of
// Release.Builds.
type ReleaseDetail struct {
	Release *Release
	Builds  []*Build
}

// ReleaseService records Builds and creates, publishes, and withdraws
// Releases. Reading follows the Application's read permission; recording
// Builds, creating, and withdrawing need release.manage; publishing needs
// release.publish. All are checked on the Application's Project. An input's
// ApplicationID may also be an active Application's slug, which the
// authorizing layer resolves to the ID, as for ApplicationService.
type ReleaseService interface {
	// ReportBuild records a Build of one of the Application's artifacts, or
	// fills in an observed one. A report naming another commit for a
	// recorded digest is refused with ErrBuildConflict, and audited.
	ReportBuild(ctx context.Context, actor identity.Actor, in ReportBuildInput) (*Build, error)
	// CreateRelease creates a draft from Builds named by digest, recording
	// Builds it has not seen, and publishes it when asked to.
	CreateRelease(ctx context.Context, actor identity.Actor, in CreateReleaseInput) (*ReleaseDetail, error)
	GetRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*ReleaseDetail, error)
	ListReleases(ctx context.Context, actor identity.Actor, query ListReleasesQuery) (*pagination.Result[*ReleaseDetail], error)
	// PublishRelease publishes a draft that still meets its Channel's rules
	// and has a Build of every required artifact.
	PublishRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*ReleaseDetail, error)
	// WithdrawRelease stops a published Release from being deployed again.
	WithdrawRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*ReleaseDetail, error)
}
