package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
)

const (
	orgA   = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	orgB   = "org_01ARZ3NDEKTSV4RRFFQ69G5FAW"
	source = "src_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

var t0 = time.Date(2026, 11, 8, 9, 30, 15, 123456000, time.UTC)

func newFixture(t *testing.T) (*gorm.DB, provenance.ChangeRepository) {
	t.Helper()
	db := databasetest.New(t, migrations.FS)
	clock.Fix(t, t0)
	return db, NewChangeRepository(db)
}

func newChange(t *testing.T, commits ...string) *provenance.Change {
	t.Helper()
	sessionID := "ses_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	workItemID := "wki_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	change, err := provenance.NewChange(provenance.NewChangeProps{
		OrganizationID: orgA, SourceID: source, SessionID: &sessionID, WorkItemID: &workItemID,
		Title: "Add checkout", CommitSHAs: commits,
	}, nil)
	require.NoError(t, err)
	return change
}

func TestChangeRepository(t *testing.T) {
	_, repo := newFixture(t)
	ctx := context.Background()
	change := newChange(t, "aaaaaaa", "bbbbbbb")
	require.NoError(t, repo.CreateChange(ctx, change))

	got, err := repo.GetChange(ctx, orgA, change.ID())
	require.NoError(t, err)
	assert.Equal(t, change.Snapshot(), got.Snapshot())

	_, err = repo.GetChange(ctx, orgB, change.ID())
	assert.ErrorIs(t, err, provenance.ErrNotFound)

	found, err := repo.ChangesForCommits(ctx, orgA, source, []string{"bbbbbbb", "ccccccc"})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, change.ID(), found[0].ID())

	found, err = repo.ChangesForCommits(ctx, orgB, source, []string{"bbbbbbb"})
	require.NoError(t, err)
	assert.Empty(t, found)

	page, err := repo.ListChanges(ctx, provenance.ListChangesQuery{OrganizationID: orgA, SourceID: source})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, change.ID(), page.Items[0].ID())
}

func TestChangeRepositoryRefusesACommitTwice(t *testing.T) {
	_, repo := newFixture(t)
	ctx := context.Background()
	require.NoError(t, repo.CreateChange(ctx, newChange(t, "aaaaaaa")))

	err := repo.CreateChange(ctx, newChange(t, "aaaaaaa", "bbbbbbb"))
	assert.ErrorIs(t, err, provenance.ErrAlreadyExists)
}
