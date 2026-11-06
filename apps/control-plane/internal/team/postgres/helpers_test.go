package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Organizations and principals belong to IAM: the team schema keeps no
// foreign keys to them.
const (
	orgA = "org_a"
	orgB = "org_b"
)

var (
	alice = id.New("prn")
	bob   = id.New("prn")
	carol = id.New("prn")
)

// t0 has microseconds, which PostgreSQL keeps, so round trips check precision.
var t0 = time.Date(2026, 10, 20, 9, 30, 15, 123456000, time.UTC)

// at fixes the domain clock d after t0, until the test ends, and returns it.
func at(t *testing.T, d time.Duration) time.Time {
	t.Helper()
	clock.Fix(t, t0.Add(d))
	return t0.Add(d)
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type fixture struct {
	ctx   context.Context
	db    *gorm.DB
	tx    *database.Transactor
	teams team.TeamRepository
}

// newFixture gives the test a fresh database with the team schema, and
// fixes the clock at t0. It skips the test without PostgreSQL.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := databasetest.New(t, migrations.FS)
	at(t, 0)
	return &fixture{ctx: context.Background(), db: db, tx: database.NewTransactor(db), teams: NewTeamRepository(db)}
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int64
	require.NoError(t, f.db.Raw(query, args...).Scan(&n).Error)
	return int(n)
}

// assertLocked checks that another connection cannot lock the row now.
func (f *fixture) assertLocked(t *testing.T, rowID string) {
	t.Helper()
	err := f.db.Exec("SELECT 1 FROM team.teams WHERE id = ? FOR UPDATE NOWAIT", rowID).Error
	require.Error(t, err, "the row is locked by the transaction")
	assert.Contains(t, err.Error(), "55P03", "lock_not_available")
}

func (f *fixture) assertNotLocked(t *testing.T, rowID string) {
	t.Helper()
	require.NoError(t, f.db.Exec("SELECT 1 FROM team.teams WHERE id = ? FOR UPDATE NOWAIT", rowID).Error)
}

func member(principalID string, role team.Role) team.NewTeamMemberProps {
	return team.NewTeamMemberProps{PrincipalID: principalID, Role: role}
}

func newTeam(org, slug string, members ...team.NewTeamMemberProps) *team.Team {
	return must(team.NewTeam(team.NewTeamProps{
		OrganizationID: org,
		Slug:           slug,
		DisplayName:    "Team " + slug,
		Members:        members,
	}, nil))
}

func (f *fixture) createTeam(t *testing.T, org, slug string, members ...team.NewTeamMemberProps) *team.Team {
	t.Helper()
	tm := newTeam(org, slug, members...)
	require.NoError(t, f.teams.CreateTeam(f.ctx, tm))
	return tm
}

func idsOf(items []*team.Team) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.ID()
	}
	return out
}
