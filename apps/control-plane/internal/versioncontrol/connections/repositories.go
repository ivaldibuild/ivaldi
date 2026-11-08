package connections

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// ListRepositories asks the provider each time rather than keeping a copy:
// Ivaldi does not mirror what a provider holds, and the list changes
// whenever someone changes the installation's repository access.
func (s *service) ListRepositories(ctx context.Context, _ identity.Actor, organizationID, connectionID string) ([]versioncontrol.AvailableRepository, error) {
	connection, err := s.connections.GetConnection(ctx, organizationID, connectionID)
	if err != nil {
		return nil, err
	}
	if err := connection.CheckControlPlaneLookup(); err != nil {
		return nil, err
	}
	if s.provider == nil || connection.AuthKind() != versioncontrol.AuthKindAppInstallation {
		return nil, versioncontrol.ErrProviderUnsupported
	}
	repositories, err := s.provider.Repositories(ctx, connection.Endpoint())
	if err != nil {
		return nil, translateListError(err)
	}
	slices.SortFunc(repositories, func(a, b vcs.Repository) int {
		return strings.Compare(strings.ToLower(a.FullName), strings.ToLower(b.FullName))
	})

	keys := make([]string, 0, len(repositories))
	for _, r := range repositories {
		keys = append(keys, connection.RepositoryKey(r.ExternalID))
	}
	connected, err := s.connected.ConnectedRepositoryKeys(ctx, organizationID, keys)
	if err != nil {
		return nil, fmt.Errorf("read connected repositories: %w", err)
	}
	out := make([]versioncontrol.AvailableRepository, 0, len(repositories))
	for i, r := range repositories {
		out = append(out, versioncontrol.AvailableRepository{Repository: r, Connected: slices.Contains(connected, keys[i])})
	}
	return out, nil
}

func translateListError(err error) error {
	switch {
	case errors.Is(err, vcs.ErrTooManyRepositories):
		return versioncontrol.NewErrInvalidInput("the account holds too many repositories to list")
	case errors.Is(err, vcs.ErrUnsupportedProvider):
		return fmt.Errorf("%w: %w", versioncontrol.ErrProviderUnsupported, err)
	case errors.Is(err, vcs.ErrUnauthorized):
		// The installation was suspended or removed at the provider.
		return fmt.Errorf("%w: the provider refused the connection: %w", versioncontrol.ErrInactiveConnection, err)
	default:
		return fmt.Errorf("%w: %w", versioncontrol.ErrProviderUnavailable, err)
	}
}
