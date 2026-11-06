package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// ActiveWorkspace checks that a Workspace exists and is active, for creating
// or restoring what belongs to it.
func ActiveWorkspace(ctx context.Context, workspaces Workspaces, organizationID, workspaceID string) error {
	w, err := workspaces.LookupWorkspace(ctx, organizationID, workspaceID)
	if errors.Is(err, workspace.ErrNotFound) {
		return fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}
	if err != nil {
		return fmt.Errorf("look up workspace %s: %w", workspaceID, err)
	}
	if w.Archived() {
		return fmt.Errorf("%w: workspace %s", ErrArchived, workspaceID)
	}
	return nil
}

// ActiveProject returns a Project if it exists and is active, for creating
// or restoring what belongs to it.
func ActiveProject(ctx context.Context, workspaces Workspaces, organizationID, projectID string) (*workspace.Project, error) {
	p, err := workspaces.LookupProject(ctx, organizationID, projectID)
	if errors.Is(err, workspace.ErrNotFound) {
		return nil, fmt.Errorf("%w: project %s", ErrNotFound, projectID)
	}
	if err != nil {
		return nil, fmt.Errorf("look up project %s: %w", projectID, err)
	}
	if p.Archived() {
		return nil, fmt.Errorf("%w: project %s", ErrArchived, projectID)
	}
	return p, nil
}
