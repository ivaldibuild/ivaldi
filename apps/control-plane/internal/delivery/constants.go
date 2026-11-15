package delivery

// DefaultChannelSlug and DefaultChannelName are given to the Channel created
// with each Application. Both can be changed later.
const (
	DefaultChannelSlug = "default"
	DefaultChannelName = "Default"
)

// Settings this capability defines, registered by RegisterSettings and
// stored and resolved by the workspace capability.
const (
	// SettingDefaultLifecycle is the Lifecycle a new Application's default
	// Channel follows when its creator names none: a Lifecycle ID, set for a
	// Workspace or a Project.
	SettingDefaultLifecycle = "delivery.default_lifecycle"
)

// Limits on what one record may hold. Text lengths match the columns.
const (
	maxDisplayNameLength   = 255
	maxTriggeredByLength   = 255
	maxPublishedNameLength = 512
	maxPoolLength          = 255
	maxLabelValueLength    = 63
	maxPhases              = 50
	maxChannelRules        = 20
	maxSelectorLabels      = 20
	maxSelectorKeyLength   = 253
	maxValuesFiles         = 20
	maxValuesFileLength    = 512
	// MaxVariablesPerOwner bounds how many active variables a Project or an
	// Application can have, which also bounds a Release's snapshot.
	MaxVariablesPerOwner   = 200
	maxVariableNameLength  = 253
	maxValuesPathLength    = 512
	maxVariableValueLength = 16384
	maxVariableValues      = 50
	maxScopeNames          = 50
	// maxClusterCapabilitiesBytes bounds the facts an agent reports about
	// itself, so a malformed or hostile report cannot fill a row.
	maxClusterCapabilitiesBytes = 64 * 1024
	// maxAgentFailureReasonLength bounds the explanation shown to whoever is
	// enrolling a Cluster's agent.
	maxAgentFailureReasonLength = 512
	// maxCapabilityReasonLength bounds why an agent says it cannot observe or
	// execute in a Target's namespace.
	maxCapabilityReasonLength = 512
)

// AssignmentPageSize is how many in-flight Deployments are read per query
// when listing a Target agent's assignments.
const AssignmentPageSize = 100

// Aggregate types name the domain objects that raise events. An envelope
// carries the type and ID of the object whose change raised it.
const (
	AggregateEnvironment      = "delivery.Environment"
	AggregateTarget           = "delivery.Target"
	AggregateCluster          = "delivery.Cluster"
	AggregateLifecycle        = "delivery.Lifecycle"
	AggregateRegistry         = "delivery.Registry"
	AggregateApplication      = "delivery.Application"
	AggregateArtifact         = "delivery.Artifact"
	AggregateChannel          = "delivery.Channel"
	AggregatePlacement        = "delivery.Placement"
	AggregateBuild            = "delivery.Build"
	AggregateRelease          = "delivery.Release"
	AggregateDeployment       = "delivery.Deployment"
	AggregateVariable         = "delivery.Variable"
	AggregateReleaseVariables = "delivery.ReleaseVariables"
)

// Permissions this capability asks the authorizer about. The full catalog,
// and which roles include each one, belongs to IAM. Targets follow their
// Environment's permissions; artifacts, Channels, and placements follow
// their Application's, which are checked on its Project.
const (
	PermissionEnvironmentRead   = "environment.read"
	PermissionEnvironmentManage = "environment.manage"
	PermissionLifecycleRead     = "lifecycle.read"
	PermissionLifecycleManage   = "lifecycle.manage"
	PermissionRegistryRead      = "registry.read"
	PermissionRegistryManage    = "registry.manage"
	PermissionApplicationRead   = "application.read"
	PermissionApplicationManage = "application.manage"
	// PermissionReleaseManage records Builds, creates Releases, and
	// withdraws them; PermissionReleasePublish publishes them. Both are
	// checked on the Application's Project; reading Releases follows
	// PermissionApplicationRead.
	PermissionReleaseManage  = "release.manage"
	PermissionReleasePublish = "release.publish"
	// PermissionDeploymentCreate requests an executed Deployment and cancels
	// one before it is applied; PermissionDeploymentRead lists and reads
	// Deployments. Both follow the Application or the Target's Environment.
	PermissionDeploymentRead   = "deployment.read"
	PermissionDeploymentCreate = "deployment.create"
	// PermissionClusterRead reads Clusters and their agent status on the
	// Organization; PermissionClusterManage creates, changes, archives, and
	// restores them, and creates their enrolment tokens.
	PermissionClusterRead   = "cluster.read"
	PermissionClusterManage = "cluster.manage"
	// PermissionVariableRead reads a Project's variables and its
	// Applications'; PermissionVariableManage changes them. Both are checked
	// on the Project, and a change to a value scoped to Environments, or to
	// Targets in them, also needs PermissionVariableManage on each.
	PermissionVariableRead   = "variable.read"
	PermissionVariableManage = "variable.manage"
)

// Actions name what an actor asks the authorizer to allow. Policy rules
// and approval requests match on them, and they appear in its decisions.
const (
	ActionVariableAddValue     = "variable.add_value"
	ActionVariableArchive      = "variable.archive"
	ActionVariableCreate       = "variable.create"
	ActionVariableList         = "variable.list"
	ActionVariableRead         = "variable.read"
	ActionVariableRemoveValue  = "variable.remove_value"
	ActionVariableUpdate       = "variable.update"
	ActionVariableUpdateValue  = "variable.update_value"
	ActionReleaseVariablesRead = "release.read_variables"
)

const (
	ActionApplicationArchive    = "application.archive"
	ActionApplicationCreate     = "application.create"
	ActionApplicationLinkSource = "application.link_source"
	ActionApplicationList       = "application.list"
	ActionApplicationRead       = "application.read"
	ActionApplicationRestore    = "application.restore"
	ActionApplicationRunsOn     = "application.runs_on"
	ActionApplicationTransfer   = "application.transfer"
	ActionApplicationUpdate     = "application.update"
	ActionBuildReport           = "build.report"
	ActionReleaseCreate         = "release.create"
	ActionReleaseList           = "release.list"
	ActionReleasePublish        = "release.publish"
	ActionReleaseRead           = "release.read"
	ActionReleaseWithdraw       = "release.withdraw"
	ActionDeploymentAssignments = "deployment.assignments"
	ActionDeploymentCancel      = "deployment.cancel"
	ActionDeploymentCreate      = "deployment.create"
	ActionDeploymentList        = "deployment.list"
	ActionDeploymentRead        = "deployment.read"
	ActionDeploymentReport      = "deployment.report"
	ActionDeploymentChanges     = "deployment.changes"
	ActionTargetReport          = "target.report"
	ActionArtifactArchive       = "artifact.archive"
	ActionArtifactCreate        = "artifact.create"
	ActionArtifactDeclare       = "artifact.declare"
	ActionArtifactList          = "artifact.list"
	ActionArtifactRestore       = "artifact.restore"
	ActionArtifactSetRegistry   = "artifact.set_registry"
	ActionArtifactUpdate        = "artifact.update"
	ActionChannelAddRule        = "channel.add_rule"
	ActionChannelArchive        = "channel.archive"
	ActionChannelCreate         = "channel.create"
	ActionChannelList           = "channel.list"
	ActionChannelRead           = "channel.read"
	ActionChannelRemoveRule     = "channel.remove_rule"
	ActionChannelRestore        = "channel.restore"
	ActionChannelSetDefault     = "channel.set_default"
	ActionChannelSetLifecycle   = "channel.set_lifecycle"
	ActionChannelUpdate         = "channel.update"
	ActionChannelUpdateRule     = "channel.update_rule"
	ActionEnvironmentArchive    = "environment.archive"
	ActionEnvironmentCreate     = "environment.create"
	ActionEnvironmentList       = "environment.list"
	ActionEnvironmentRead       = "environment.read"
	ActionEnvironmentRestore    = "environment.restore"
	ActionEnvironmentUpdate     = "environment.update"
	ActionLifecycleAddPhase     = "lifecycle.add_phase"
	ActionLifecycleArchive      = "lifecycle.archive"
	ActionLifecycleCreate       = "lifecycle.create"
	ActionLifecycleList         = "lifecycle.list"
	ActionLifecycleMovePhase    = "lifecycle.move_phase"
	ActionLifecycleRead         = "lifecycle.read"
	ActionLifecycleRemovePhase  = "lifecycle.remove_phase"
	ActionLifecycleRestore      = "lifecycle.restore"
	ActionLifecycleUpdate       = "lifecycle.update"
	ActionLifecycleUpdatePhase  = "lifecycle.update_phase"
	ActionPlacementConfigure    = "placement.configure"
	ActionPlacementCreate       = "placement.create"
	ActionPlacementList         = "placement.list"
	ActionPlacementRemove       = "placement.remove"
	ActionRegistryArchive       = "registry.archive"
	ActionRegistryCreate        = "registry.create"
	ActionRegistryList          = "registry.list"
	ActionRegistryRead          = "registry.read"
	ActionRegistryRestore       = "registry.restore"
	ActionRegistrySetCredential = "registry.set_credential"
	ActionRegistrySetLookup     = "registry.set_lookup"
	ActionRegistryUpdate        = "registry.update"
	ActionTargetArchive         = "target.archive"
	ActionTargetCreate          = "target.create"
	ActionTargetList            = "target.list"
	ActionTargetRead            = "target.read"
	ActionTargetRestore         = "target.restore"
	ActionTargetUpdate          = "target.update"

	ActionClusterCreate          = "cluster.create"
	ActionClusterList            = "cluster.list"
	ActionClusterRead            = "cluster.read"
	ActionClusterUpdate          = "cluster.update"
	ActionClusterArchive         = "cluster.archive"
	ActionClusterRestore         = "cluster.restore"
	ActionClusterCreateEnrolment = "cluster.create_enrolment"
)

// Audit event types this capability emits.
const (
	EventEnvironmentCreated  = "environment.created"
	EventEnvironmentUpdated  = "environment.updated"
	EventEnvironmentArchived = "environment.archived"
	EventEnvironmentRestored = "environment.restored"

	EventTargetCreated  = "target.created"
	EventTargetUpdated  = "target.updated"
	EventTargetArchived = "target.archived"
	EventTargetRestored = "target.restored"

	EventClusterCreated       = "cluster.created"
	EventClusterUpdated       = "cluster.updated"
	EventClusterAgentAssigned = "cluster.agent_assigned"
	EventClusterArchived      = "cluster.archived"
	EventClusterRestored      = "cluster.restored"

	EventVariableCreated   = "variable.created"
	EventVariableUpdated   = "variable.updated"
	EventVariableArchived  = "variable.archived"
	EventLifecycleCreated  = "lifecycle.created"
	EventLifecycleUpdated  = "lifecycle.updated"
	EventLifecycleArchived = "lifecycle.archived"
	EventLifecycleRestored = "lifecycle.restored"

	EventRegistryCreated  = "registry.created"
	EventRegistryUpdated  = "registry.updated"
	EventRegistryArchived = "registry.archived"
	EventRegistryRestored = "registry.restored"

	EventApplicationCreated      = "application.created"
	EventApplicationUpdated      = "application.updated"
	EventApplicationOwnerChanged = "application.owner_changed"
	EventApplicationArchived     = "application.archived"
	EventApplicationRestored     = "application.restored"

	EventArtifactCreated  = "artifact.created"
	EventArtifactUpdated  = "artifact.updated"
	EventArtifactArchived = "artifact.archived"
	EventArtifactRestored = "artifact.restored"

	EventChannelCreated      = "channel.created"
	EventChannelUpdated      = "channel.updated"
	EventChannelDefaultMoved = "channel.default_moved"
	EventChannelArchived     = "channel.archived"
	EventChannelRestored     = "channel.restored"

	EventPlacementCreated = "placement.created"
	EventPlacementUpdated = "placement.updated"
	EventPlacementRemoved = "placement.removed"

	EventBuildRecorded = "build.recorded"
	EventBuildReported = "build.reported"
	// EventBuildReportRefused records a report that gave a Build another
	// commit: a CI misconfiguration or tampering. It is committed although
	// the request fails.
	EventBuildReportRefused = "build.report_refused"

	EventReleaseCreated   = "release.created"
	EventReleasePublished = "release.published"
	EventReleaseWithdrawn = "release.withdrawn"

	EventDeploymentRequested     = "deployment.requested"
	EventDeploymentObserved      = "deployment.observed"
	EventDeploymentCancelled     = "deployment.cancelled"
	EventDeploymentFinished      = "deployment.finished"
	EventDeploymentFailed        = "deployment.failed"
	EventDeploymentSuperseded    = "deployment.superseded"
	EventDeploymentStatusChanged = "deployment.status_changed"
)
