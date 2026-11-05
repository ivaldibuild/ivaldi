package onboarding

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/hasher/hashertest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/migrations"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

var onboardingTables = []string{"organizations", "accounts", "password_credentials", "principals", "users"}

// newPostgresOnboarding wires the service to the PostgreSQL repositories. It
// skips the test without PostgreSQL.
func newPostgresOnboarding(t *testing.T, outbox *fakeOutbox) (*onboardingService, *gorm.DB) {
	t.Helper()
	db := databasetest.New(t, migrations.FS)
	logger := logging.NewMockLogger()
	logger.On("Error", mock.Anything, mock.Anything).Maybe()
	svc, err := NewOnboardingService(
		WithLogger(logger),
		WithOrganizationRepo(postgres.NewOrganizationRepository(db)),
		WithAccountRepo(postgres.NewAccountRepository(db)),
		WithPasswordCredentialRepo(postgres.NewPasswordCredentialRepository(db)),
		WithUserRepo(postgres.NewUserRepository(db)),
		WithTransactor(database.NewTransactor(db)),
		WithOutboxRecorder(outbox),
		WithPasswordHasher(hashertest.NewFake()),
	)
	require.NoError(t, err)
	return svc.(*onboardingService), db
}

func countRows(t *testing.T, db *gorm.DB, table string) int {
	t.Helper()
	var n int64
	require.NoError(t, db.Raw("SELECT count(*) FROM iam."+table).Scan(&n).Error)
	return int(n)
}

func TestOnboardOrganizationInPostgres(t *testing.T) {
	svc, db := newPostgresOnboarding(t, &fakeOutbox{})
	ctx := context.Background()

	require.NoError(t, svc.OnboardOrganization(ctx, input()))
	for _, table := range onboardingTables {
		assert.Equal(t, 1, countRows(t, db, table), table)
	}

	second := input()
	second.OrganizationName, second.OrganizationDisplayName = "Difference Engines", "Difference Engines"
	_, ok := errors.AsType[*ErrAccountConflict](svc.OnboardOrganization(ctx, second))
	assert.True(t, ok, "the owner's email already has an account")
}

func TestOnboardOrganizationInPostgresWritesNothingWhenAnyStepFails(t *testing.T) {
	svc, db := newPostgresOnboarding(t, &fakeOutbox{err: errBoom})

	err := svc.OnboardOrganization(context.Background(), input())

	require.ErrorIs(t, err, errBoom)
	for _, table := range onboardingTables {
		assert.Zero(t, countRows(t, db, table), "%s is rolled back with the events", table)
	}
}
