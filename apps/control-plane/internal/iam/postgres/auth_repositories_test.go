package postgres

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

const testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func hashOf(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func challengeOf(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func (f *fixture) writeUser(t *testing.T, org *iam.Organization, account *iam.Account) *iam.User {
	t.Helper()
	u := newUser(org.ID(), account.ID())
	require.NoError(t, f.users.Write(f.ctx, u))
	return u
}

func (f *fixture) writeClient(t *testing.T, clientID string) *iam.OAuthClient {
	t.Helper()
	c := must(iam.NewOAuthClient(iam.NewOAuthClientProps{
		ClientID:     clientID,
		Registration: iam.RegistrationFirstParty,
		Name:         "Client " + clientID,
		RedirectURIs: []string{"http://127.0.0.1/callback", "https://app.example.com/cb"},
	}, nil))
	require.NoError(t, f.clients.Write(f.ctx, c))
	return c
}

func TestReadMethodsOfExistingRepositories(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	acme := f.writeOrg(t, "acme")
	at(t, time.Minute)
	other := f.writeOrg(t, "other")
	ada := f.writeAccount(t, "Ada@Example.com")

	t.Run("organizations", func(t *testing.T) {
		n, err := f.orgs.Count(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 2, n)

		oldest, err := f.orgs.Oldest(ctx)
		require.NoError(t, err)
		assert.Equal(t, acme.ID(), oldest.ID())

		got, err := f.orgs.GetForUpdate(ctx, acme.ID())
		require.NoError(t, err)
		assert.Equal(t, acme.Snapshot(), got.Snapshot())
		_, err = f.orgs.GetForUpdate(ctx, "org_missing")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("accounts", func(t *testing.T) {
		got, err := f.accounts.Get(ctx, ada.ID())
		require.NoError(t, err)
		assert.Equal(t, ada.Snapshot(), got.Snapshot())

		byEmail, err := f.accounts.GetByEmail(ctx, "ada@EXAMPLE.com")
		require.NoError(t, err)
		assert.Equal(t, ada.ID(), byEmail.ID(), "emails match ignoring case")

		_, err = f.accounts.GetByEmail(ctx, "nobody@example.com")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("passwords", func(t *testing.T) {
		c := must(iam.NewPasswordCredential(iam.NewPasswordCredentialProps{AccountID: ada.ID(), PasswordHash: testHash}))
		require.NoError(t, f.passwords.Write(ctx, c))

		got, err := f.passwords.Get(ctx, ada.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), got.Snapshot())

		_, err = f.passwords.Get(ctx, "acc_missing")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("users and memberships", func(t *testing.T) {
		inAcme := f.writeUser(t, acme, ada)
		at(t, 2*time.Minute)
		inOther := f.writeUser(t, other, ada)
		require.NoError(t, inOther.Suspend())
		require.NoError(t, f.users.Write(ctx, inOther))

		got, err := f.users.Get(ctx, acme.ID(), inAcme.ID())
		require.NoError(t, err)
		assert.Equal(t, inAcme.Snapshot(), got.Snapshot())
		_, err = f.users.Get(ctx, other.ID(), inAcme.ID())
		assert.ErrorIs(t, err, iam.ErrNotFound, "a User is only found in its own Organization")

		byAccount, err := f.users.GetByAccount(ctx, other.ID(), ada.ID())
		require.NoError(t, err)
		assert.Equal(t, inOther.ID(), byAccount.ID())

		memberships, err := f.users.ListMemberships(ctx, ada.ID())
		require.NoError(t, err)
		assert.Equal(t, []iam.Membership{
			{OrganizationID: acme.ID(), OrganizationSlug: "acme", OrganizationName: "Org acme", OrganizationStatus: iam.OrganizationActive, PrincipalID: inAcme.ID(), Status: iam.PrincipalActive},
			{OrganizationID: other.ID(), OrganizationSlug: "other", OrganizationName: "Org other", OrganizationStatus: iam.OrganizationActive, PrincipalID: inOther.ID(), Status: iam.PrincipalSuspended},
		}, memberships, "oldest first, with statuses so sign-in can leave out suspended ones")
		assert.True(t, memberships[0].Active())
		assert.False(t, memberships[1].Active())

		none, err := f.users.ListMemberships(ctx, "acc_nobody")
		require.NoError(t, err)
		assert.Empty(t, none)
	})
}

func TestIdentityProviderRepository(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	org := f.writeOrg(t, "acme")

	managed := must(iam.NewIdentityProvider(iam.NewIdentityProviderProps{
		OrganizationID:         org.ID(),
		Protocol:               iam.ProtocolGitHub,
		Name:                   "GitHub",
		Config:                 iam.IdentityProviderConfig{ClientID: "Iv1.abc", AllowedOrganizations: []string{"acme"}},
		ManagedByConfiguration: true,
		JITProvisioning:        true,
	}, nil))
	require.NoError(t, f.providers.Write(ctx, managed))
	at(t, time.Minute)
	builtin := must(iam.NewIdentityProvider(iam.NewIdentityProviderProps{OrganizationID: org.ID(), Protocol: iam.ProtocolBuiltin, Name: "Password"}, nil))
	require.NoError(t, f.providers.Write(ctx, builtin))

	t.Run("round trip, managed flag included", func(t *testing.T) {
		got, err := f.providers.Get(ctx, org.ID(), managed.ID())
		require.NoError(t, err)
		assert.Equal(t, managed.Snapshot(), got.Snapshot())
		assert.True(t, got.ManagedByConfiguration())
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.identity_providers WHERE id = ? AND config->>'managed_by' = 'configuration'", managed.ID()))
	})

	t.Run("list oldest first", func(t *testing.T) {
		all, err := f.providers.List(ctx, org.ID())
		require.NoError(t, err)
		assert.Equal(t, []string{managed.ID(), builtin.ID()}, idsOf(all))
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		_, err := f.providers.Get(ctx, "org_other", managed.ID())
		assert.ErrorIs(t, err, iam.ErrNotFound)
		none, err := f.providers.List(ctx, "org_other")
		require.NoError(t, err)
		assert.Empty(t, none)
	})

	t.Run("write updates a stored provider", func(t *testing.T) {
		stored := must(f.providers.Get(ctx, org.ID(), managed.ID()))
		at(t, time.Hour)
		require.NoError(t, stored.Unconfigure())
		require.NoError(t, f.providers.Write(ctx, stored))

		got := must(f.providers.Get(ctx, org.ID(), managed.ID()))
		assert.Equal(t, stored.Snapshot(), got.Snapshot())
		assert.False(t, got.Active())
	})
}

func TestIdentityRepositories(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	org := f.writeOrg(t, "acme")
	ada := f.writeAccount(t, "ada@example.com")
	user := f.writeUser(t, org, ada)
	idp := must(iam.NewIdentityProvider(iam.NewIdentityProviderProps{
		OrganizationID: org.ID(), Protocol: iam.ProtocolOIDC, Name: "SSO",
		Config: iam.IdentityProviderConfig{Issuer: "https://sso.example.com", ClientID: "ivaldi"},
	}, nil))
	require.NoError(t, f.providers.Write(ctx, idp))

	t.Run("external identity", func(t *testing.T) {
		x := must(iam.NewExternalIdentity(iam.NewExternalIdentityProps{OrganizationID: org.ID(), PrincipalID: user.ID(), IdentityProviderID: idp.ID(), Subject: "sub-1"}, nil))
		require.NoError(t, f.externals.Write(ctx, x))

		got, err := f.externals.GetBySubject(ctx, org.ID(), idp.ID(), "sub-1")
		require.NoError(t, err)
		assert.Equal(t, x.Snapshot(), got.Snapshot())

		at(t, time.Hour)
		got.RecordLogin()
		require.NoError(t, f.externals.Write(ctx, got))
		again := must(f.externals.GetBySubject(ctx, org.ID(), idp.ID(), "sub-1"))
		assert.Equal(t, got.Snapshot(), again.Snapshot(), "the last sign-in is saved")

		_, err = f.externals.GetBySubject(ctx, "org_other", idp.ID(), "sub-1")
		assert.ErrorIs(t, err, iam.ErrNotFound)

		dup := must(iam.NewExternalIdentity(iam.NewExternalIdentityProps{OrganizationID: org.ID(), PrincipalID: user.ID(), IdentityProviderID: idp.ID(), Subject: "sub-1"}, nil))
		assert.ErrorIs(t, f.externals.Write(ctx, dup), iam.ErrAlreadyExists, "a subject links to one User per provider")
	})

	t.Run("account identity", func(t *testing.T) {
		a := must(iam.NewAccountIdentity(iam.NewAccountIdentityProps{AccountID: ada.ID(), Provider: iam.PlatformGitHub, Subject: "583231"}, nil))
		require.NoError(t, f.platform.Write(ctx, a))

		got, err := f.platform.GetBySubject(ctx, iam.PlatformGitHub, "583231")
		require.NoError(t, err)
		assert.Equal(t, a.Snapshot(), got.Snapshot())

		_, err = f.platform.GetBySubject(ctx, iam.PlatformGoogle, "583231")
		assert.ErrorIs(t, err, iam.ErrNotFound, "subjects are per provider")

		dup := must(iam.NewAccountIdentity(iam.NewAccountIdentityProps{AccountID: ada.ID(), Provider: iam.PlatformGitHub, Subject: "583231"}, nil))
		assert.ErrorIs(t, f.platform.Write(ctx, dup), iam.ErrAlreadyExists)

		orphan := must(iam.NewAccountIdentity(iam.NewAccountIdentityProps{AccountID: newAccount("ghost@example.com").ID(), Provider: iam.PlatformGoogle, Subject: "g-1"}, nil))
		assert.ErrorIs(t, f.platform.Write(ctx, orphan), iam.ErrNotFound)
	})
}

func TestResourcesAndGrants(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	org := f.writeOrg(t, "acme")
	owner := f.writeUser(t, org, f.writeAccount(t, "ada@example.com"))
	ownerRole, _ := iam.BuiltInRoleID(iam.RoleOwner)
	grant := func() *iam.Grant {
		return must(iam.NewGrant(iam.NewGrantProps{
			OrganizationID: org.ID(), PrincipalID: new(owner.ID()), RoleID: ownerRole,
			ResourceID: org.ID(), GrantedByPrincipalID: owner.ID(),
		}, nil))
	}

	t.Run("a grant needs its resource", func(t *testing.T) {
		assert.ErrorIs(t, f.grants.Write(ctx, grant()), iam.ErrNotFound)
	})

	require.NoError(t, f.resources.RegisterResource(ctx, org.Resource()))

	t.Run("register, archive, and restore a resource", func(t *testing.T) {
		assert.ErrorIs(t, f.resources.RegisterResource(ctx, org.Resource()), iam.ErrAlreadyExists)
		ws := iam.Resource{ID: "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", OrganizationID: org.ID(), Type: iam.ResourceWorkspace, ParentID: org.ID()}
		require.NoError(t, f.resources.RegisterResource(ctx, ws))
		assert.ErrorIs(t, f.resources.RegisterResource(ctx, iam.Resource{ID: "prj_x", OrganizationID: org.ID(), Type: iam.ResourceProject, ParentID: "wsp_missing"}), iam.ErrNotFound)

		require.NoError(t, f.resources.ArchiveResource(ctx, org.ID(), ws.ID))
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.resources WHERE id = ? AND deleted_at IS NOT NULL", ws.ID))
		require.NoError(t, f.resources.RestoreResource(ctx, org.ID(), ws.ID))
		assert.Equal(t, 1, f.count(t, "SELECT count(*) FROM iam.resources WHERE id = ? AND deleted_at IS NULL", ws.ID))
		assert.ErrorIs(t, f.resources.ArchiveResource(ctx, "org_other", ws.ID), iam.ErrNotFound)
	})

	t.Run("owner grant", func(t *testing.T) {
		has, err := f.grants.ExistsForRole(ctx, org.ID(), ownerRole, org.ID())
		require.NoError(t, err)
		assert.False(t, has)

		require.NoError(t, f.grants.Write(ctx, grant()))

		has, err = f.grants.ExistsForRole(ctx, org.ID(), ownerRole, org.ID())
		require.NoError(t, err)
		assert.True(t, has)
		assert.ErrorIs(t, f.grants.Write(ctx, grant()), iam.ErrAlreadyExists, "the same role twice")
	})
}

func TestOAuthRepositories(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctx
	org := f.writeOrg(t, "acme")
	user := f.writeUser(t, org, f.writeAccount(t, "ada@example.com"))
	client := f.writeClient(t, "ivaldictl")

	t.Run("clients", func(t *testing.T) {
		got, err := f.clients.GetByClientID(ctx, "ivaldictl")
		require.NoError(t, err)
		assert.Equal(t, client.Snapshot(), got.Snapshot(), "redirect URIs round-trip as a text array")
		byID, err := f.clients.Get(ctx, client.ID())
		require.NoError(t, err)
		assert.Equal(t, client.ID(), byID.ID())

		require.NoError(t, got.Update("Ivaldi CLI", []string{"http://[::1]/callback"}))
		require.NoError(t, f.clients.Write(ctx, got))
		assert.Equal(t, got.Snapshot(), must(f.clients.GetByClientID(ctx, "ivaldictl")).Snapshot())

		dup := must(iam.NewOAuthClient(iam.NewOAuthClientProps{ClientID: "ivaldictl", Registration: iam.RegistrationFirstParty, Name: "Dup", RedirectURIs: []string{"http://127.0.0.1/cb"}}, nil))
		assert.ErrorIs(t, f.clients.Write(ctx, dup), iam.ErrAlreadyExists)
		_, err = f.clients.GetByClientID(ctx, "unknown")
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("authorization codes", func(t *testing.T) {
		code := must(iam.NewOAuthAuthorizationCode(iam.NewOAuthAuthorizationCodeProps{
			CodeHash: hashOf("code-1"), OrganizationID: org.ID(), PrincipalID: user.ID(), ClientID: client.ID(),
			RedirectURI: "http://127.0.0.1:5000/callback", CodeChallenge: challengeOf(testVerifier),
			Resource: "https://ivaldi.example.com", Scopes: []string{"b", "a"}, ExpiresAt: t0.Add(time.Minute),
		}))
		require.NoError(t, f.codes.Write(ctx, code))

		got, err := f.codes.GetForUpdate(ctx, hashOf("code-1"))
		require.NoError(t, err)
		assert.Equal(t, code.Snapshot(), got.Snapshot())

		require.NoError(t, got.Redeem(client.ID(), "http://127.0.0.1:5000/callback", testVerifier))
		require.NoError(t, f.codes.Write(ctx, got))
		used := must(f.codes.GetForUpdate(ctx, hashOf("code-1")))
		assert.True(t, used.Used(), "redemption is saved")

		_, err = f.codes.GetForUpdate(ctx, hashOf("code-2"))
		assert.ErrorIs(t, err, iam.ErrNotFound)
	})

	t.Run("refresh tokens rotate and revoke by family", func(t *testing.T) {
		first := must(iam.NewOAuthRefreshToken(iam.NewOAuthRefreshTokenProps{
			OrganizationID: org.ID(), PrincipalID: user.ID(), ClientID: client.ID(), TokenHash: hashOf("rt-1"),
			Resource: "https://ivaldi.example.com", ExpiresAt: t0.Add(24 * time.Hour),
		}, nil))
		require.NoError(t, f.refreshes.Write(ctx, first))

		stored, err := f.refreshes.GetByHashForUpdate(ctx, hashOf("rt-1"))
		require.NoError(t, err)
		assert.Equal(t, first.Snapshot(), stored.Snapshot())

		second, err := stored.Rotate(client.ID(), hashOf("rt-2"), stored.ExpiresAt())
		require.NoError(t, err)
		assert.ErrorIs(t, f.refreshes.Write(ctx, stored), iam.ErrNotFound, "the rotated token refers to a replacement not yet written")
		require.NoError(t, f.refreshes.Write(ctx, second))
		require.NoError(t, f.refreshes.Write(ctx, stored))

		active, err := f.refreshes.ListActiveByFamily(ctx, org.ID(), first.ID())
		require.NoError(t, err)
		assert.Equal(t, []string{first.ID(), second.ID()}, idsOf(active), "the replaced token is not revoked until a replay")

		for _, tok := range active {
			tok.Revoke()
			require.NoError(t, f.refreshes.Write(ctx, tok))
		}
		active, err = f.refreshes.ListActiveByFamily(ctx, org.ID(), first.ID())
		require.NoError(t, err)
		assert.Empty(t, active)
		assert.True(t, must(f.refreshes.GetByHashForUpdate(ctx, hashOf("rt-2"))).Revoked())

		none, err := f.refreshes.ListActiveByFamily(ctx, "org_other", first.ID())
		require.NoError(t, err)
		assert.Empty(t, none)
	})
}
