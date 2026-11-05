package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
)

// testHash is an argon2id hash in PHC format; the domain refuses anything else.
const testHash = "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$aGFzaGhhc2hoYXNo"

// t0 has microseconds, which PostgreSQL keeps, so round trips check precision.
var t0 = time.Date(2026, 10, 20, 9, 30, 15, 123456000, time.UTC)

// at fixes the domain clock d after t0, until the test ends, and returns it.
func at(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	clock.Fix(t, t0.Add(d))
	return t0.Add(d)
}

// must returns v, panicking on err; it keeps builders short.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type fixture struct {
	ctx       context.Context
	db        *gorm.DB
	orgs      iam.OrganizationRepository
	accounts  iam.AccountRepository
	passwords iam.PasswordCredentialRepository
	users     iam.UserRepository
	providers iam.IdentityProviderRepository
	externals iam.ExternalIdentityRepository
	platform  iam.AccountIdentityRepository
	grants    iam.GrantRepository
	resources iam.ResourceRegistrar
	clients   iam.OAuthClientRepository
	codes     iam.OAuthAuthorizationCodeRepository
	refreshes iam.OAuthRefreshTokenRepository
}

// newFixture gives the test a fresh database with the iam schema, and fixes
// the clock at t0. It skips the test without PostgreSQL.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := databasetest.New(t, migrations.FS)
	at(t, 0)
	return &fixture{
		ctx:       context.Background(),
		db:        db,
		orgs:      NewOrganizationRepository(db),
		accounts:  NewAccountRepository(db),
		passwords: NewPasswordCredentialRepository(db),
		users:     NewUserRepository(db),
		providers: NewIdentityProviderRepository(db),
		externals: NewExternalIdentityRepository(db),
		platform:  NewAccountIdentityRepository(db),
		grants:    NewGrantRepository(db),
		resources: NewResourceRegistrar(db),
		clients:   NewOAuthClientRepository(db),
		codes:     NewOAuthAuthorizationCodeRepository(db),
		refreshes: NewOAuthRefreshTokenRepository(db),
	}
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Raw(query, args...).Scan(&n).Error)
	return int(n)
}

func newOrg(slug string) *iam.Organization {
	return must(iam.NewOrganization(iam.NewOrganizationProps{Slug: slug, DisplayName: "Org " + slug}, nil))
}

func (f *fixture) writeOrg(t *testing.T, slug string) *iam.Organization {
	t.Helper()
	o := newOrg(slug)
	require.NoError(t, f.orgs.Write(f.ctx, o))
	return o
}

func newAccount(email string) *iam.Account {
	return must(iam.NewAccount(iam.NewAccountProps{Email: email, DisplayName: "Person " + email}, nil))
}

func (f *fixture) writeAccount(t *testing.T, email string) *iam.Account {
	t.Helper()
	a := newAccount(email)
	require.NoError(t, f.accounts.Write(f.ctx, a))
	return a
}

func newUser(orgID, accountID string) *iam.User {
	return must(iam.NewUser(iam.NewUserProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: orgID, DisplayName: "Member"},
		AccountID:      accountID,
	}, nil))
}

// The repositories only write these records, so tests read them back here.

func (f *fixture) loadOrg(t *testing.T, id string) *iam.Organization {
	t.Helper()
	var e organizationEntity
	require.NoError(t, f.db.Take(&e, "id = ?", id).Error)
	return must(newOrganizationMapper().ToDomain(&e))
}

func (f *fixture) loadAccount(t *testing.T, id string) *iam.Account {
	t.Helper()
	var e accountEntity
	require.NoError(t, f.db.Take(&e, "id = ?", id).Error)
	return must(newAccountMapper().ToDomain(&e))
}

func (f *fixture) loadPassword(t *testing.T, accountID string) *iam.PasswordCredential {
	t.Helper()
	var e passwordCredentialEntity
	require.NoError(t, f.db.Take(&e, "account_id = ?", accountID).Error)
	return must(newPasswordCredentialMapper().ToDomain(&e))
}

func (f *fixture) loadUser(t *testing.T, id string) *iam.User {
	t.Helper()
	var p principalEntity
	require.NoError(t, f.db.Take(&p, "id = ?", id).Error)
	var u userEntity
	require.NoError(t, f.db.Take(&u, "principal_id = ?", id).Error)
	return must(newUserMapper().ToDomain(&p, &u))
}

// idsOf returns the IDs of objects in order.
func idsOf[T interface{ ID() string }](items []T) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID()
	}
	return ids
}
