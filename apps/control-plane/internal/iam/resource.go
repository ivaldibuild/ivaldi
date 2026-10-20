// Package iam is the control plane's identity and access capability
// (ADR 0003, docs/design/iam-data-model.md).
//
// So far it holds only the contracts other capabilities depend on; the
// implementation will follow the workspace capability's pattern.
package iam

import "context"

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
}

// ResourceRegistrar keeps IAM's copy of the resource hierarchy in step with
// the capabilities that own the entities, such as Workspaces, Projects,
// Sources, and Environments. Calls run in the caller's transaction, so an
// entity never exists without its resource.
type ResourceRegistrar interface {
	RegisterResource(ctx context.Context, r Resource) error
	// ArchiveResource soft-deletes the resource, so grants on it stop applying.
	ArchiveResource(ctx context.Context, organizationID, resourceID string) error
	RestoreResource(ctx context.Context, organizationID, resourceID string) error
}
