package connections

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

// installStateName binds sealed install state to its purpose, so no other
// sealed value can be replayed as one.
const installStateName = "versioncontrol/installation"

// installState is what an install flow carries through the provider: who
// started it, for which Organization and provider.
type installState struct {
	OrganizationID string           `json:"o"`
	PrincipalID    string           `json:"p"`
	Provider       vcs.ProviderKind `json:"k"`
}

func (s *service) installer(provider vcs.ProviderKind) (vcs.Installer, error) {
	installer, ok := s.installers[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %s", versioncontrol.ErrProviderUnsupported, provider)
	}
	return installer, nil
}

func (s *service) StartInstallation(_ context.Context, actor identity.Actor, organizationID string, provider vcs.ProviderKind) (*versioncontrol.InstallationStart, error) {
	installer, err := s.installer(provider)
	if err != nil {
		return nil, err
	}
	if actor.PrincipalID == "" {
		return nil, versioncontrol.NewErrInvalidInput("an install is started by a principal")
	}
	state, err := s.states.Encode(installStateName, installState{
		OrganizationID: organizationID, PrincipalID: actor.PrincipalID, Provider: provider,
	}, versioncontrol.InstallationTTL)
	if err != nil {
		return nil, fmt.Errorf("seal install state: %w", err)
	}
	return &versioncontrol.InstallationStart{
		URL:       installer.InstallURL(state),
		ExpiresAt: clock.Now().Add(versioncontrol.InstallationTTL),
	}, nil
}

// CompleteInstallation checks that the state is the actor's own, for this
// Organization, so no one can complete another person's install. Then the
// provider confirms the actor can access the installation, so no one can
// claim an installation by its ID.
func (s *service) CompleteInstallation(ctx context.Context, actor identity.Actor, in versioncontrol.CompleteInstallationInput) (*versioncontrol.Connection, error) {
	installer, err := s.installer(in.Provider)
	if err != nil {
		return nil, err
	}
	var state installState
	if err := s.states.Decode(installStateName, in.State, &state); err != nil ||
		actor.PrincipalID == "" ||
		state.OrganizationID != in.OrganizationID ||
		state.PrincipalID != actor.PrincipalID ||
		state.Provider != in.Provider {
		return nil, versioncontrol.NewErrInvalidInput("the install expired or was started by someone else; start it again")
	}
	installation, err := installer.VerifyInstallation(ctx, in.Code, in.InstallationID)
	if err != nil {
		return nil, translateInstallError(err, in.InstallationID)
	}
	c, err := versioncontrol.NewInstalledConnection(in.OrganizationID, &actor.PrincipalID, installation)
	if err != nil {
		return nil, err
	}
	if err := s.create(ctx, actor, c); err != nil {
		return nil, err
	}
	return c, nil
}

func translateInstallError(err error, installationID string) error {
	switch {
	case errors.Is(err, vcs.ErrUnauthorized):
		return versioncontrol.NewErrInvalidInput(fmt.Sprintf("the provider did not confirm that you can access installation %s", installationID))
	case errors.Is(err, vcs.ErrUnsupportedProvider):
		return fmt.Errorf("%w: %w", versioncontrol.ErrProviderUnsupported, err)
	default:
		return fmt.Errorf("%w: %w", versioncontrol.ErrProviderUnavailable, err)
	}
}
