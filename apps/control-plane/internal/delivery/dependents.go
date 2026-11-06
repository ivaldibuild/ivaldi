package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// NewDependents returns the workspace.Dependent for this capability: a
// Workspace cannot be archived while it has active Environments,
// Lifecycles, or Registries, nor a Project while it has active Applications.
func NewDependents(environments EnvironmentRepository, lifecycles LifecycleRepository, registries RegistryRepository, applications ApplicationRepository) (workspace.Dependent, error) {
	switch {
	case environments == nil:
		return nil, errors.New("delivery dependents: an EnvironmentRepository is required")
	case lifecycles == nil:
		return nil, errors.New("delivery dependents: a LifecycleRepository is required")
	case registries == nil:
		return nil, errors.New("delivery dependents: a RegistryRepository is required")
	case applications == nil:
		return nil, errors.New("delivery dependents: an ApplicationRepository is required")
	}
	return &dependents{environments: environments, lifecycles: lifecycles, registries: registries, applications: applications}, nil
}

type dependents struct {
	environments EnvironmentRepository
	lifecycles   LifecycleRepository
	registries   RegistryRepository
	applications ApplicationRepository
}

func (d *dependents) ActiveDependents(ctx context.Context, target workspace.ArchiveTarget) ([]workspace.Dependency, error) {
	if target.ProjectID != "" {
		n, err := d.applications.CountActiveApplications(ctx, target.OrganizationID, target.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("count applications: %w", err)
		}
		return []workspace.Dependency{{Capability: "delivery", Kind: "Application", Count: n}}, nil
	}
	counts := []struct {
		kind  string
		count func(ctx context.Context, organizationID, workspaceID string) (int, error)
	}{
		{"Environment", d.environments.CountActiveEnvironments},
		{"Lifecycle", d.lifecycles.CountActiveLifecycles},
		{"Registry", d.registries.CountActiveRegistries},
	}
	var deps []workspace.Dependency
	for _, c := range counts {
		n, err := c.count(ctx, target.OrganizationID, target.WorkspaceID)
		if err != nil {
			return nil, fmt.Errorf("count %s records: %w", c.kind, err)
		}
		deps = append(deps, workspace.Dependency{Capability: "delivery", Kind: c.kind, Count: n})
	}
	return deps, nil
}
