package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/versioncontrol"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const (
	orgID      = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	otherOrgID = "org_01ARZ3NDEKTSV4RRFFQ69G5FAW"
)

var (
	ada   = identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV", Kind: identity.ActorUser}
	grace = identity.Actor{PrincipalID: "prn_01ARZ3NDEKTSV4RRFFQ69G5FAW", Kind: identity.ActorUser}
)

type allow struct{}

func (allow) Authorize(context.Context, authorization.Request) (authorization.Decision, error) {
	return authorization.Decision{ID: "dcn_test", Outcome: authorization.OutcomeAllow}, nil
}

type writer struct{}

func (writer) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
func (writer) Record(context.Context, ...events.Envelope) error                   { return nil }

type auditor struct{}

func (auditor) Record(context.Context, audit.Event) error { return nil }

type connectionStore struct {
	created []*versioncontrol.Connection
}

func (s *connectionStore) CreateConnection(_ context.Context, c *versioncontrol.Connection) error {
	s.created = append(s.created, c)
	return nil
}
func (s *connectionStore) GetConnection(context.Context, string, string) (*versioncontrol.Connection, error) {
	return nil, versioncontrol.ErrNotFound
}
func (s *connectionStore) GetConnectionForUpdate(context.Context, string, string) (*versioncontrol.Connection, error) {
	return nil, versioncontrol.ErrNotFound
}
func (s *connectionStore) GetConnectionByWebhookRouteKey(context.Context, string) (*versioncontrol.Connection, error) {
	return nil, versioncontrol.ErrNotFound
}
func (s *connectionStore) ListConnections(context.Context, string, bool) ([]*versioncontrol.Connection, error) {
	return nil, nil
}
func (s *connectionStore) UpdateConnection(context.Context, *versioncontrol.Connection) error {
	return nil
}

// sealer keeps sealed values in memory, standing in for IAM's codec.
type sealer struct {
	values map[string][]byte
}

func (s *sealer) Encode(name string, v any, _ time.Duration) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	token := fmt.Sprintf("%s#%d", name, len(s.values))
	s.values[token] = b
	return token, nil
}

func (s *sealer) Decode(_ string, value string, v any) error {
	b, ok := s.values[value]
	if !ok {
		return errors.New("invalid state")
	}
	return json.Unmarshal(b, v)
}

// installer confirms only the installations its person can access.
type installer struct {
	accessible map[string]string
	verified   int
}

func (i *installer) InstallURL(state string) string {
	return "https://github.com/apps/ivaldi/installations/new?state=" + state
}

func (i *installer) VerifyInstallation(_ context.Context, code, installationID string) (vcs.Installation, error) {
	i.verified++
	account, ok := i.accessible[installationID]
	if code != "good-code" || !ok {
		return vcs.Installation{}, vcs.ErrUnauthorized
	}
	return vcs.Installation{
		Provider: vcs.ProviderGitHub, HostURL: "https://github.com", InstallationID: installationID, AccountName: account,
	}, nil
}

func newService(t *testing.T, store *connectionStore, inst *installer) versioncontrol.ConnectionService {
	t.Helper()
	service, err := NewConnectionService(
		WithConnectionRepo(store),
		WithInstaller(vcs.ProviderGitHub, inst),
		WithStateSealer(&sealer{values: map[string][]byte{}}),
		WithConnectedRepositories(connectedKeys(nil)),
		WithAuthorizer(allow{}),
		WithTransactor(writer{}),
		WithOutboxRecorder(writer{}),
		WithAuditRecorder(auditor{}),
	)
	require.NoError(t, err)
	return service
}

func stateOf(t *testing.T, start *versioncontrol.InstallationStart) string {
	t.Helper()
	const prefix = "https://github.com/apps/ivaldi/installations/new?state="
	require.Contains(t, start.URL, prefix)
	return start.URL[len(prefix):]
}

func TestCompleteInstallationConnectsAVerifiedInstallation(t *testing.T) {
	store := &connectionStore{}
	service := newService(t, store, &installer{accessible: map[string]string{"42": "acme"}})
	ctx := context.Background()

	start, err := service.StartInstallation(ctx, ada, orgID, vcs.ProviderGitHub)
	require.NoError(t, err)
	c, err := service.CompleteInstallation(ctx, ada, versioncontrol.CompleteInstallationInput{
		OrganizationID: orgID, Provider: vcs.ProviderGitHub, State: stateOf(t, start), Code: "good-code", InstallationID: "42",
	})
	require.NoError(t, err)

	assert.Len(t, store.created, 1)
	assert.Equal(t, "42", c.ExternalAccountID())
	assert.Equal(t, "acme", *c.ExternalAccountName(), "the account name comes from the provider")
	assert.Equal(t, versioncontrol.AuthKindAppInstallation, c.AuthKind())
	assert.Equal(t, "github:github.com:42", *c.WebhookRouteKey(), "the route key keeps the installation to one Organization")
}

func TestCompleteInstallationRefusesWhatThePersonCannotProve(t *testing.T) {
	ctx := context.Background()
	inst := &installer{accessible: map[string]string{"42": "acme"}}
	store := &connectionStore{}
	service := newService(t, store, inst)
	start, err := service.StartInstallation(ctx, ada, orgID, vcs.ProviderGitHub)
	require.NoError(t, err)
	state := stateOf(t, start)

	for name, tc := range map[string]struct {
		actor identity.Actor
		in    versioncontrol.CompleteInstallationInput
	}{
		"someone else's install": {grace, versioncontrol.CompleteInstallationInput{
			OrganizationID: orgID, Provider: vcs.ProviderGitHub, State: state, Code: "good-code", InstallationID: "42",
		}},
		"another Organization": {ada, versioncontrol.CompleteInstallationInput{
			OrganizationID: otherOrgID, Provider: vcs.ProviderGitHub, State: state, Code: "good-code", InstallationID: "42",
		}},
		"a forged state": {ada, versioncontrol.CompleteInstallationInput{
			OrganizationID: orgID, Provider: vcs.ProviderGitHub, State: "forged", Code: "good-code", InstallationID: "42",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := service.CompleteInstallation(ctx, tc.actor, tc.in)
			assert.ErrorIs(t, err, versioncontrol.ErrInvalid)
		})
	}
	assert.Zero(t, inst.verified, "the provider is not asked about a state that is not the caller's")

	t.Run("an installation the person cannot access", func(t *testing.T) {
		_, err := service.CompleteInstallation(ctx, ada, versioncontrol.CompleteInstallationInput{
			OrganizationID: orgID, Provider: vcs.ProviderGitHub, State: state, Code: "good-code", InstallationID: "43",
		})
		assert.ErrorIs(t, err, versioncontrol.ErrInvalid)
	})
	assert.Empty(t, store.created)
}

func TestCreateConnectionRefusesAnAppInstallation(t *testing.T) {
	store := &connectionStore{}
	service := newService(t, store, &installer{})
	_, err := service.CreateConnection(context.Background(), ada, versioncontrol.CreateConnectionInput{
		OrganizationID: orgID, Provider: vcs.ProviderGitHub, HostURL: "https://github.com",
		AuthKind: versioncontrol.AuthKindAppInstallation, ExternalAccountID: "42", LookupFrom: vcs.LookupFromControlPlane,
	})
	assert.ErrorIs(t, err, versioncontrol.ErrInvalid)
	assert.Empty(t, store.created)
}
