package postgres

import "time"

// Rows of the delivery schema. They carry GORM's mapping so the domain types
// do not. IDs and timestamps are set by the domain, never by GORM. JSON
// columns are raw bytes, decoded by the mappers.

type environmentEntity struct {
	ID               string     `gorm:"column:id;primaryKey"`
	OrganizationID   string     `gorm:"column:organization_id"`
	WorkspaceID      string     `gorm:"column:workspace_id"`
	Slug             string     `gorm:"column:slug"`
	DisplayName      string     `gorm:"column:display_name"`
	Tier             string     `gorm:"column:tier"`
	ExecutionEnabled bool       `gorm:"column:execution_enabled"`
	CreatedAt        time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

func (environmentEntity) TableName() string { return "delivery.environments" }

type lifecycleEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	WorkspaceID    string     `gorm:"column:workspace_id"`
	Slug           string     `gorm:"column:slug"`
	DisplayName    string     `gorm:"column:display_name"`
	Description    *string    `gorm:"column:description"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
}

func (lifecycleEntity) TableName() string { return "delivery.lifecycles" }

type lifecyclePhaseEntity struct {
	ID              string    `gorm:"column:id;primaryKey"`
	OrganizationID  string    `gorm:"column:organization_id"`
	LifecycleID     string    `gorm:"column:lifecycle_id"`
	Position        int       `gorm:"column:position"`
	Name            string    `gorm:"column:name"`
	Optional        bool      `gorm:"column:optional"`
	MinEnvironments *int      `gorm:"column:min_environments"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (lifecyclePhaseEntity) TableName() string { return "delivery.lifecycle_phases" }

// lifecyclePhaseEnvironmentEntity's created_at is left to the database
// default: the row has no state of its own beyond its phase's.
type lifecyclePhaseEnvironmentEntity struct {
	OrganizationID string `gorm:"column:organization_id"`
	LifecycleID    string `gorm:"column:lifecycle_id"`
	PhaseID        string `gorm:"column:phase_id;primaryKey"`
	EnvironmentID  string `gorm:"column:environment_id;primaryKey"`
}

func (lifecyclePhaseEnvironmentEntity) TableName() string {
	return "delivery.lifecycle_phase_environments"
}

type targetEntity struct {
	ID               string     `gorm:"column:id;primaryKey"`
	OrganizationID   string     `gorm:"column:organization_id"`
	EnvironmentID    string     `gorm:"column:environment_id"`
	Slug             string     `gorm:"column:slug"`
	DisplayName      string     `gorm:"column:display_name"`
	Runtime          string     `gorm:"column:runtime"`
	Locator          []byte     `gorm:"column:locator;type:jsonb"`
	AgentPrincipalID *string    `gorm:"column:agent_principal_id"`
	LastReportedAt   *time.Time `gorm:"column:last_reported_at"`
	CreatedAt        time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

func (targetEntity) TableName() string { return "delivery.targets" }

type registryEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	WorkspaceID    string     `gorm:"column:workspace_id"`
	Slug           string     `gorm:"column:slug"`
	DisplayName    string     `gorm:"column:display_name"`
	Kind           string     `gorm:"column:kind"`
	URL            string     `gorm:"column:url"`
	CredentialRef  *string    `gorm:"column:credential_ref"`
	LookupFrom     string     `gorm:"column:lookup_from"`
	LookupPool     *string    `gorm:"column:lookup_pool"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
}

func (registryEntity) TableName() string { return "delivery.registries" }

type applicationEntity struct {
	ID               string     `gorm:"column:id;primaryKey"`
	OrganizationID   string     `gorm:"column:organization_id"`
	WorkspaceID      string     `gorm:"column:workspace_id"`
	ProjectID        string     `gorm:"column:project_id"`
	OwnerTeamID      string     `gorm:"column:owner_team_id"`
	Slug             string     `gorm:"column:slug"`
	DisplayName      string     `gorm:"column:display_name"`
	SourceID         *string    `gorm:"column:source_id"`
	WorkloadSelector []byte     `gorm:"column:workload_selector;type:jsonb"`
	CreatedAt        time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt        time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt       *time.Time `gorm:"column:archived_at"`
}

func (applicationEntity) TableName() string { return "delivery.applications" }

type artifactEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	ApplicationID  string     `gorm:"column:application_id"`
	Name           string     `gorm:"column:name"`
	ArtifactKind   string     `gorm:"column:artifact_kind"`
	ArtifactName   string     `gorm:"column:artifact_name"`
	RegistryID     *string    `gorm:"column:registry_id"`
	Required       bool       `gorm:"column:required"`
	Source         string     `gorm:"column:source"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt     *time.Time `gorm:"column:archived_at"`
}

func (artifactEntity) TableName() string { return "delivery.application_artifacts" }

type channelEntity struct {
	ID             string     `gorm:"column:id;primaryKey"`
	OrganizationID string     `gorm:"column:organization_id"`
	ApplicationID  string     `gorm:"column:application_id"`
	LifecycleID    string     `gorm:"column:lifecycle_id"`
	Slug           string     `gorm:"column:slug"`
	DisplayName    string     `gorm:"column:display_name"`
	Description    *string    `gorm:"column:description"`
	IsDefault      bool       `gorm:"column:is_default"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;autoUpdateTime:false"`
	ArchivedAt     *time.Time `gorm:"column:archived_at"`
}

func (channelEntity) TableName() string { return "delivery.channels" }

type channelRuleEntity struct {
	ID             string    `gorm:"column:id;primaryKey"`
	OrganizationID string    `gorm:"column:organization_id"`
	ApplicationID  string    `gorm:"column:application_id"`
	ChannelID      string    `gorm:"column:channel_id"`
	Kind           string    `gorm:"column:kind"`
	ArtifactID     *string   `gorm:"column:artifact_id"`
	Config         []byte    `gorm:"column:config;type:jsonb"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (channelRuleEntity) TableName() string { return "delivery.channel_rules" }

type placementEntity struct {
	OrganizationID      string    `gorm:"column:organization_id"`
	ApplicationID       string    `gorm:"column:application_id;primaryKey"`
	TargetID            string    `gorm:"column:target_id;primaryKey"`
	Source              string    `gorm:"column:source"`
	ApplyConfig         []byte    `gorm:"column:apply_config;type:jsonb"`
	CurrentDeploymentID *string   `gorm:"column:current_deployment_id"`
	CurrentReleaseID    *string   `gorm:"column:current_release_id"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime:false"`
	UpdatedAt           time.Time `gorm:"column:updated_at;autoUpdateTime:false"`
}

func (placementEntity) TableName() string { return "delivery.application_targets" }

// utc returns t in UTC. The driver returns times in the local zone, and the
// domain records UTC.
func utc(t time.Time) *time.Time {
	u := t.UTC()
	return &u
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return utc(*t)
}
