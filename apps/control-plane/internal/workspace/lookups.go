package workspace

import "context"

// Lookups reads Workspaces and Projects for other capabilities, such as
// delivery checking that a Workspace is active before creating something in
// it. It does not authorize: the calling capability has already authorized
// its own operation.
type Lookups interface {
	LookupWorkspace(ctx context.Context, organizationID, workspaceID string) (*Workspace, error)
	LookupProject(ctx context.Context, organizationID, projectID string) (*Project, error)
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
