package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

func publicKey(t *testing.T) []byte {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := identity.MarshalWorkloadPublicKeyDER(public)
	require.NoError(t, err)
	return der
}

func gatewaySettings(keys ...[]byte) Settings {
	return Settings{Mode: ModeCloud, PlatformWorkloads: []PlatformWorkloadSettings{
		{Name: "gateway", Component: iam.ComponentGateway, PublicKeys: keys},
	}}
}

// platformState returns the gateway platform workload and its active keys.
func (e *env) platformState(t *testing.T) (*iam.Workload, []string) {
	t.Helper()
	ctx := context.Background()
	listed, err := iampg.NewWorkloadRepository(e.db).ListPlatform(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	creds, err := iampg.NewWorkloadCredentialRepository(e.db).ListPlatformByPrincipal(ctx, listed[0].ID())
	require.NoError(t, err)
	var active []string
	for _, c := range creds {
		if !c.Revoked() {
			active = append(active, c.Identifier())
		}
	}
	return listed[0], active
}

func keyID(t *testing.T, der []byte) string {
	t.Helper()
	public, err := identity.ParseWorkloadPublicKeyDER(der)
	require.NoError(t, err)
	id, err := identity.WorkloadKeyThumbprint(public)
	require.NoError(t, err)
	return id
}

func TestPlatformWorkloadsFollowTheConfiguration(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	first, second := publicKey(t), publicKey(t)

	_, err := e.bootstrapper(t, gatewaySettings(first)).Bootstrap(ctx)
	require.NoError(t, err)
	gateway, keys := e.platformState(t)
	assert.True(t, gateway.Platform())
	assert.Equal(t, iam.ComponentGateway, gateway.Component())
	assert.Equal(t, []string{keyID(t, first)}, keys)
	assert.Contains(t, e.outbox.types(), "iam.v1.workload.created")

	t.Run("an unchanged configuration writes nothing", func(t *testing.T) {
		e.outbox.reset()
		_, err := e.bootstrapper(t, gatewaySettings(first)).Bootstrap(ctx)
		require.NoError(t, err)
		assert.Empty(t, e.outbox.types())
	})

	t.Run("rotating a key revokes the one no longer listed", func(t *testing.T) {
		_, err := e.bootstrapper(t, gatewaySettings(first, second)).Bootstrap(ctx)
		require.NoError(t, err)
		_, keys := e.platformState(t)
		assert.ElementsMatch(t, []string{keyID(t, first), keyID(t, second)}, keys)

		_, err = e.bootstrapper(t, gatewaySettings(second)).Bootstrap(ctx)
		require.NoError(t, err)
		_, keys = e.platformState(t)
		assert.Equal(t, []string{keyID(t, second)}, keys)
	})

	t.Run("a workload removed from the configuration is suspended", func(t *testing.T) {
		_, err := e.bootstrapper(t, Settings{Mode: ModeCloud}).Bootstrap(ctx)
		require.NoError(t, err)
		gateway, _ := e.platformState(t)
		assert.Equal(t, iam.PrincipalSuspended, gateway.Status())

		_, err = e.bootstrapper(t, gatewaySettings(second)).Bootstrap(ctx)
		require.NoError(t, err)
		gateway, _ = e.platformState(t)
		assert.Equal(t, iam.PrincipalActive, gateway.Status(), "listing it again reactivates it")
	})

	t.Run("a name cannot change component", func(t *testing.T) {
		s := gatewaySettings(second)
		s.PlatformWorkloads[0].Component = iam.ComponentIndexer
		_, err := e.bootstrapper(t, s).Bootstrap(ctx)
		assert.Error(t, err)
	})
}
