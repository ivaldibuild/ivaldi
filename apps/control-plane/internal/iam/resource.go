package iam

// ResourceType is the type of an entry in IAM's resource hierarchy:
// Organization → Workspace → Project → Source, with Environments under their
// Workspace. IAM validates it, so the full list lives here.
type ResourceType string

const (
	ResourceOrganization ResourceType = "organization"
	ResourceWorkspace    ResourceType = "workspace"
	ResourceProject      ResourceType = "project"
	ResourceSource       ResourceType = "source"
	ResourceEnvironment  ResourceType = "environment"
)

// Resource is an entry in IAM's resource hierarchy. Its ID is the ID of the
// entity it mirrors, owned by another capability.
type Resource struct {
	ID             string
	OrganizationID string
	Type           ResourceType
	// ParentID is the resource it sits under, such as the Organization's ID
	// for a Workspace, or the Workspace ID for a Project.
	ParentID string
	// Archived is set when reading: the entity the resource mirrors is
	// archived, so grants on it do not apply.
	Archived bool
}
