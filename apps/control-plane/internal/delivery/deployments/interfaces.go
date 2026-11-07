package deployments

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

// The narrow contracts this package needs from delivery's repositories.

type Applications interface {
	GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error)
	GetApplicationByIdentifier(ctx context.Context, organizationID, identifier string) (*delivery.Application, error)
	GetPlacement(ctx context.Context, organizationID, applicationID, targetID string) (*delivery.Placement, error)
	ListArtifacts(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Artifact, error)
	GetChannel(ctx context.Context, organizationID, channelID string) (*delivery.Channel, error)
	UpdatePlacementCurrent(ctx context.Context, p *delivery.Placement) error
}

type Environments interface {
	GetEnvironment(ctx context.Context, organizationID, environmentID string) (*delivery.Environment, error)
}

type Targets interface {
	GetTarget(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
	UpdateTarget(ctx context.Context, t *delivery.Target) error
}

type Lifecycles interface {
	GetLifecycle(ctx context.Context, organizationID, lifecycleID string) (*delivery.Lifecycle, error)
}

type Releases interface {
	GetRelease(ctx context.Context, organizationID, releaseID string) (*delivery.Release, error)
}

type Builds interface {
	GetBuilds(ctx context.Context, organizationID string, buildIDs []string) ([]*delivery.Build, error)
}
