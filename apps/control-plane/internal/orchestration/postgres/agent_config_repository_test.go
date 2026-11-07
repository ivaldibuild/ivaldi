package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration/migrations"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/database/databasetest"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

var t0 = time.Date(2026, 11, 7, 10, 0, 0, 123456000, time.UTC)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestAgentConfigRepository(t *testing.T) {
	clock.Fix(t, t0)
	ctx := context.Background()
	db := databasetest.New(t, migrations.FS)
	repo := NewAgentConfigRepository(db)
	org := id.New(iam.Organization{}.Prefix())
	other := id.New(iam.Organization{}.Prefix())
	agentID := id.New(iam.Agent{}.Prefix())

	config := must(orchestration.NewAgentConfig(orchestration.NewAgentConfigProps{
		OrganizationID: org, AgentID: agentID, SystemPrompt: "Be careful and cite evidence.",
	}))
	require.NoError(t, repo.Upsert(ctx, config))

	got, err := repo.Get(ctx, org, agentID)
	require.NoError(t, err)
	assert.Equal(t, config.Snapshot(), got.Snapshot())
	assert.Empty(t, got.PullEvents(), "a loaded config records no event")

	_, err = repo.Get(ctx, other, agentID)
	assert.ErrorIs(t, err, orchestration.ErrNotFound, "another organization sees nothing")

	more := must(orchestration.NewAgentConfig(orchestration.NewAgentConfigProps{
		OrganizationID: org, AgentID: id.New(iam.Agent{}.Prefix()), SystemPrompt: "Second",
	}))
	require.NoError(t, repo.Upsert(ctx, more))
	configs, err := repo.ListByAgentIDs(ctx, org, []string{agentID, more.AgentID(), id.New(iam.Agent{}.Prefix())})
	require.NoError(t, err)
	assert.Len(t, configs, 2, "missing rows are omitted")
	assert.Equal(t, "Be careful and cite evidence.", configs[agentID].SystemPrompt())

	clock.Fix(t, t0.Add(time.Minute))
	require.NoError(t, config.SetSystemPrompt("Prefer the graph."))
	require.NoError(t, repo.Upsert(ctx, config))
	got, err = repo.Get(ctx, org, agentID)
	require.NoError(t, err)
	assert.Equal(t, "Prefer the graph.", got.SystemPrompt())
	assert.Equal(t, config.Snapshot().CreatedAt, got.Snapshot().CreatedAt, "the creation time survives an update")
	assert.Equal(t, config.Snapshot().UpdatedAt, got.Snapshot().UpdatedAt)
}
