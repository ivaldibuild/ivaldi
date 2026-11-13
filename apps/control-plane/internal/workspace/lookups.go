package workspace

import "context"

// Lookups reads Workspaces and Projects for other capabilities, such as
// delivery checking that a Workspace is active before creating something in
// it, or labelling Applications with their Project. It does not authorize:
// the calling capability has already authorized its own operation.
type Lookups interface {
	LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*Workspace, error)
	LookupProject(ctx context.Context, organizationID, projectID string) (*Project, error)
	// LookupProjects returns the Projects with the IDs, archived ones
	// included, in no particular order. IDs that match nothing are left out.
	LookupProjects(ctx context.Context, organizationID string, projectIDs []string) ([]*Project, error)
}

// NewLookups returns Lookups that read through repo.
func NewLookups(repo Repository) Lookups {
	return lookups{repo: repo}
}

type lookups struct {
	repo Repository
}

func (l lookups) LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*Workspace, error) {
	return l.repo.GetWorkspace(ctx, organizationID, workspaceID)
}

func (l lookups) LookupProject(ctx context.Context, organizationID, projectID string) (*Project, error) {
	return l.repo.GetProject(ctx, organizationID, projectID)
}

func (l lookups) LookupProjects(ctx context.Context, organizationID string, projectIDs []string) ([]*Project, error) {
	return l.repo.GetProjects(ctx, organizationID, projectIDs)
}
