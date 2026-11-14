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
	GetDefaultChannel(ctx context.Context, organizationID, applicationID string) (*delivery.Channel, error)
	UpdatePlacementCurrent(ctx context.Context, p *delivery.Placement) error
	// CreateArtifact records an image the agent saw that the Application did
	// not declare.
	CreateArtifact(ctx context.Context, a *delivery.Artifact) error
	// CreatePlacementIfAbsent records an Application the agent saw running
	// on a Target, when nobody had placed it, and reports whether it inserted
	// it. It never fails because the placement exists.
	CreatePlacementIfAbsent(ctx context.Context, p *delivery.Placement) (bool, error)
}

type Environments interface {
	GetEnvironment(ctx context.Context, organizationID, environmentID string) (*delivery.Environment, error)
}

type Targets interface {
	GetTarget(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
	// GetTargetForUpdate is GetTarget that also locks the row until the
	// transaction ends.
	GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
	UpdateTarget(ctx context.Context, t *delivery.Target) error
}

// Clusters is where a Target's runtime agent lives: a Target is served by the
// agent of the Cluster it names.
type Clusters interface {
	GetCluster(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error)
	// GetClusterForShare is GetCluster that takes a shared lock until the
	// transaction ends, so re-enrolment cannot change the agent meanwhile.
	GetClusterForShare(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error)
}

type Lifecycles interface {
	GetLifecycle(ctx context.Context, organizationID, lifecycleID string) (*delivery.Lifecycle, error)
}

type Releases interface {
	GetRelease(ctx context.Context, organizationID, releaseID string) (*delivery.Release, error)
	GetReleaseByVersion(ctx context.Context, organizationID, applicationID, version string) (*delivery.Release, error)
	CreateRelease(ctx context.Context, r *delivery.Release) error
}

type Builds interface {
	GetBuilds(ctx context.Context, organizationID string, buildIDs []string) ([]*delivery.Build, error)
	// CreateBuild inserts the Build unless one with its artifact name and
	// digest exists, and reports whether it inserted it.
	CreateBuild(ctx context.Context, b *delivery.Build) (bool, error)
	// GetBuildByDigestForUpdate returns the Build of artifactName with
	// digest, locked until the transaction ends.
	GetBuildByDigestForUpdate(ctx context.Context, organizationID, artifactName, digest string) (*delivery.Build, error)
}
