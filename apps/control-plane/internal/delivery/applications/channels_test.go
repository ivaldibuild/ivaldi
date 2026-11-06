package applications

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// channelSetup is an Application with two artifacts, one archived, and
// another Application with an artifact of its own.
type channelSetup struct {
	*world
	lifecycle   *delivery.Lifecycle
	app         *delivery.Application
	defaultCh   *delivery.Channel
	api         *delivery.Artifact
	oldArtifact *delivery.Artifact
	foreign     *delivery.Artifact
}

func newChannelSetup(t *testing.T) *channelSetup {
	t.Helper()
	s := &channelSetup{world: newWorld(t)}
	s.lifecycle = s.world.lifecycle(t, wsActive, "mainline", false)
	s.app, s.defaultCh = s.world.app(t, prjActive, "api", s.lifecycle)
	s.api = s.world.artifact(t, s.app, "api")
	s.oldArtifact = s.world.artifact(t, s.app, "old")
	_, err := s.svc.ArchiveArtifact(context.Background(), ada, org, s.oldArtifact.ID())
	require.NoError(t, err)
	other, _ := s.world.app(t, prjActive, "worker", s.lifecycle)
	s.foreign = s.world.artifact(t, other, "worker")
	s.reset()
	return s
}

func (s *channelSetup) archiveChannel(t *testing.T, c *delivery.Channel) {
	t.Helper()
	_, err := s.svc.ArchiveChannel(context.Background(), ada, org, c.ID())
	require.NoError(t, err)
}

func TestCreateChannel(t *testing.T) {
	ctx := context.Background()

	t.Run("created", func(t *testing.T) {
		s := newChannelSetup(t)

		c, err := s.svc.CreateChannel(ctx, ada, delivery.CreateChannelInput{
			OrganizationID: org, ApplicationID: s.app.ID(), LifecycleID: s.lifecycle.ID(), DisplayName: "Hotfix", Description: "Urgent fixes",
			Rules: []delivery.ChannelRuleProps{gitRule(ptr(s.api.ID())), gitRule(nil)},
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(delivery.Channel{}.Prefix(), c.ID()))
		assert.Equal(t, "hotfix", c.Slug())
		assert.False(t, c.IsDefault())
		assert.Len(t, c.Rules(), 2)
		assert.Equal(t, []string{s.api.ID()}, c.ArtifactIDs())
		ev := s.lastAudit()
		assert.Equal(t, delivery.EventChannelCreated, ev.Type)
		assert.Equal(t, c.Snapshot(), ev.After)
		assert.Equal(t, "dec-"+prjActive, ev.DecisionID)
		assert.Equal(t, c.Snapshot(), s.channels[c.ID()], "rules are stored with their IDs")
	})

	refusals := []struct {
		name    string
		in      func(*testing.T, *channelSetup) delivery.CreateChannelInput
		wantErr error
	}{
		{"rule scoped to another application's artifact", func(_ *testing.T, s *channelSetup) delivery.CreateChannelInput {
			return delivery.CreateChannelInput{LifecycleID: s.lifecycle.ID(), Rules: []delivery.ChannelRuleProps{gitRule(ptr(s.foreign.ID()))}}
		}, delivery.ErrNotFound},
		{"rule scoped to a missing artifact", func(_ *testing.T, s *channelSetup) delivery.CreateChannelInput {
			return delivery.CreateChannelInput{LifecycleID: s.lifecycle.ID(), Rules: []delivery.ChannelRuleProps{gitRule(ptr("art_01JZ0000000000000000000099"))}}
		}, delivery.ErrNotFound},
		{"rule scoped to an archived artifact", func(_ *testing.T, s *channelSetup) delivery.CreateChannelInput {
			return delivery.CreateChannelInput{LifecycleID: s.lifecycle.ID(), Rules: []delivery.ChannelRuleProps{gitRule(ptr(s.oldArtifact.ID()))}}
		}, delivery.ErrArchived},
		{"lifecycle in another workspace", func(t *testing.T, s *channelSetup) delivery.CreateChannelInput {
			return delivery.CreateChannelInput{LifecycleID: s.world.lifecycle(t, wsOther, "l", false).ID()}
		}, delivery.ErrWorkspaceMismatch},
		{"archived lifecycle", func(t *testing.T, s *channelSetup) delivery.CreateChannelInput {
			return delivery.CreateChannelInput{LifecycleID: s.world.lifecycle(t, wsActive, "archived", true).ID()}
		}, delivery.ErrArchived},
		{"slug taken", func(_ *testing.T, s *channelSetup) delivery.CreateChannelInput {
			return delivery.CreateChannelInput{LifecycleID: s.lifecycle.ID(), Slug: delivery.DefaultChannelSlug}
		}, delivery.ErrSlugTaken},
		{"archived application", func(t *testing.T, s *channelSetup) delivery.CreateChannelInput {
			s.archiveApp(t, s.app)
			return delivery.CreateChannelInput{LifecycleID: s.lifecycle.ID()}
		}, delivery.ErrArchived},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			s := newChannelSetup(t)
			in := tt.in(t, s)
			in.OrganizationID = org
			in.ApplicationID = s.app.ID()
			in.DisplayName = "Hotfix"
			before := s.state.clone()

			_, err := s.svc.CreateChannel(ctx, ada, in)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, before, s.state)
		})
	}
}

func TestGetAndListChannels(t *testing.T) {
	ctx := context.Background()
	s := newChannelSetup(t)
	c := s.channel(t, s.app, s.lifecycle, "hotfix")
	s.archiveChannel(t, c)

	got, err := s.svc.GetChannel(ctx, ada, org, s.defaultCh.ID())
	require.NoError(t, err)
	assert.Equal(t, s.defaultCh.Snapshot(), got.Snapshot())
	_, err = s.svc.GetChannel(ctx, ada, org, "chn_01JZ0000000000000000000099")
	require.ErrorIs(t, err, delivery.ErrNotFound)

	active, err := s.svc.ListChannels(ctx, ada, org, s.app.ID(), false)
	require.NoError(t, err)
	assert.Len(t, active, 1)
	all, err := s.svc.ListChannels(ctx, ada, org, s.app.ID(), true)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestChannelRules(t *testing.T) {
	ctx := context.Background()
	s := newChannelSetup(t)
	c := s.channel(t, s.app, s.lifecycle, "hotfix", gitRule(nil))
	ruleID := c.Rules()[0].ID

	t.Run("update the channel", func(t *testing.T) {
		got, err := s.svc.UpdateChannel(ctx, ada, org, c.ID(), delivery.UpdateChannelProps{Description: ptr("Fixes")})
		require.NoError(t, err)
		assert.Equal(t, "Fixes", *got.Description())
		assert.Equal(t, delivery.EventChannelUpdated, s.lastAudit().Type)

		_, err = s.svc.UpdateChannel(ctx, ada, org, c.ID(), delivery.UpdateChannelProps{Slug: ptr(delivery.DefaultChannelSlug)})
		require.ErrorIs(t, err, delivery.ErrSlugTaken)
	})

	t.Run("add", func(t *testing.T) {
		got, err := s.svc.AddChannelRule(ctx, ada, org, c.ID(), gitRule(ptr(s.api.ID())))
		require.NoError(t, err)
		assert.Len(t, got.Rules(), 2)
		ev := s.lastAudit()
		assert.Equal(t, delivery.EventChannelUpdated, ev.Type)
		assert.Len(t, ev.Before.(delivery.ChannelSnapshot).Rules, 1)
		assert.Len(t, ev.After.(delivery.ChannelSnapshot).Rules, 2)
	})

	t.Run("add checks the artifact", func(t *testing.T) {
		before := s.state.clone()
		_, err := s.svc.AddChannelRule(ctx, ada, org, c.ID(), gitRule(ptr(s.foreign.ID())))
		require.ErrorIs(t, err, delivery.ErrNotFound)
		_, err = s.svc.AddChannelRule(ctx, ada, org, c.ID(), gitRule(ptr(s.oldArtifact.ID())))
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, s.state)
	})

	t.Run("update checks the artifact", func(t *testing.T) {
		_, err := s.svc.UpdateChannelRule(ctx, ada, org, c.ID(), ruleID, gitRule(ptr(s.foreign.ID())))
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("update", func(t *testing.T) {
		got, err := s.svc.UpdateChannelRule(ctx, ada, org, c.ID(), ruleID, delivery.ChannelRuleProps{Kind: "version", ArtifactID: ptr(s.api.ID()), Config: []byte(`{"pattern":"^v"}`)})
		require.NoError(t, err)
		r := got.Rules()[0]
		assert.Equal(t, ruleID, r.ID, "the rule keeps its ID")
		assert.Equal(t, s.api.ID(), *r.ArtifactID)
	})

	t.Run("an unknown rule", func(t *testing.T) {
		_, err := s.svc.RemoveChannelRule(ctx, ada, org, c.ID(), "chr_01JZ0000000000000000000099")
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})

	t.Run("remove", func(t *testing.T) {
		got, err := s.svc.RemoveChannelRule(ctx, ada, org, c.ID(), ruleID)
		require.NoError(t, err)
		assert.Len(t, got.Rules(), 1)
		assert.Equal(t, got.Snapshot(), s.channels[c.ID()])
	})
}

func TestSetChannelLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newChannelSetup(t)
	next := s.world.lifecycle(t, wsActive, "next", false)
	foreign := s.world.lifecycle(t, wsOther, "foreign", false)
	archived := s.world.lifecycle(t, wsActive, "archived", true)

	got, err := s.svc.SetChannelLifecycle(ctx, ada, org, s.defaultCh.ID(), next.ID())
	require.NoError(t, err)
	assert.Equal(t, next.ID(), got.LifecycleID())
	ev := s.lastAudit()
	assert.Equal(t, delivery.EventChannelUpdated, ev.Type)
	assert.Equal(t, s.lifecycle.ID(), ev.Before.(delivery.ChannelSnapshot).LifecycleID)

	before := s.state.clone()
	_, err = s.svc.SetChannelLifecycle(ctx, ada, org, s.defaultCh.ID(), foreign.ID())
	require.ErrorIs(t, err, delivery.ErrWorkspaceMismatch)
	_, err = s.svc.SetChannelLifecycle(ctx, ada, org, s.defaultCh.ID(), archived.ID())
	require.ErrorIs(t, err, delivery.ErrArchived)
	_, err = s.svc.SetChannelLifecycle(ctx, ada, org, s.defaultCh.ID(), "lfc_01JZ0000000000000000000099")
	require.ErrorIs(t, err, delivery.ErrNotFound)
	assert.Equal(t, before, s.state)
}

func TestSetDefaultChannel(t *testing.T) {
	ctx := context.Background()
	s := newChannelSetup(t)
	hotfix := s.channel(t, s.app, s.lifecycle, "hotfix")
	s.reset()

	got, err := s.svc.SetDefaultChannel(ctx, ada, org, hotfix.ID())

	require.NoError(t, err)
	assert.True(t, got.IsDefault())
	assert.False(t, s.channels[s.defaultCh.ID()].IsDefault, "the old default is cleared")
	current, err := s.GetDefaultChannel(ctx, org, s.app.ID())
	require.NoError(t, err)
	assert.Equal(t, hotfix.ID(), current.ID())
	require.Len(t, s.audit, 1, "one record for the move")
	assert.Equal(t, delivery.EventChannelDefaultMoved, s.audit[0].Type)
	assert.Equal(t, hotfix.ID(), s.audit[0].ResourceID)
	assert.Equal(t, s.defaultCh.ID(), s.audit[0].Before)
	assert.Equal(t, hotfix.ID(), s.audit[0].After)
	assert.Equal(t, "dec-"+prjActive, s.audit[0].DecisionID)
	assert.Equal(t, []string{"delivery.v1.channel.default_cleared", "delivery.v1.channel.made_default"}, s.eventTypes(), "each changed channel records its events")
	assert.Equal(t, hotfix.ID(), s.events[1].AggregateID)

	t.Run("making the default the default does nothing", func(t *testing.T) {
		before := s.state.clone()
		got, err := s.svc.SetDefaultChannel(ctx, ada, org, hotfix.ID())
		require.NoError(t, err)
		assert.True(t, got.IsDefault())
		assert.Equal(t, before, s.state)
	})

	t.Run("archiving the default is refused", func(t *testing.T) {
		before := s.state.clone()
		_, err := s.svc.ArchiveChannel(ctx, ada, org, hotfix.ID())
		require.ErrorIs(t, err, delivery.ErrDefaultChannel)
		assert.Equal(t, before, s.state)
	})

	t.Run("the old default can now be archived", func(t *testing.T) {
		got, err := s.svc.ArchiveChannel(ctx, ada, org, s.defaultCh.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, delivery.EventChannelArchived, s.lastAudit().Type)
	})

	t.Run("an archived channel cannot become the default", func(t *testing.T) {
		before := s.state.clone()
		_, err := s.svc.SetDefaultChannel(ctx, ada, org, s.defaultCh.ID())
		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, s.state)
	})

	t.Run("refused for an archived application", func(t *testing.T) {
		other := s.channel(t, s.app, s.lifecycle, "other")
		s.archiveApp(t, s.app)
		_, err := s.svc.SetDefaultChannel(ctx, ada, org, other.ID())
		require.ErrorIs(t, err, delivery.ErrArchived)
	})

	t.Run("a missing channel", func(t *testing.T) {
		_, err := s.svc.SetDefaultChannel(ctx, ada, org, "chn_01JZ0000000000000000000099")
		require.ErrorIs(t, err, delivery.ErrNotFound)
	})
}

func TestRestoreChannel(t *testing.T) {
	ctx := context.Background()

	t.Run("restored", func(t *testing.T) {
		s := newChannelSetup(t)
		c := s.channel(t, s.app, s.lifecycle, "hotfix")
		s.archiveChannel(t, c)

		got, err := s.svc.RestoreChannel(ctx, ada, org, c.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		ev := s.lastAudit()
		assert.Equal(t, delivery.EventChannelRestored, ev.Type)
		assert.NotNil(t, ev.Before.(delivery.ChannelSnapshot).ArchivedAt)
	})

	t.Run("refused for an archived application", func(t *testing.T) {
		s := newChannelSetup(t)
		c := s.channel(t, s.app, s.lifecycle, "hotfix")
		s.archiveChannel(t, c)
		s.archiveApp(t, s.app)
		before := s.state.clone()

		_, err := s.svc.RestoreChannel(ctx, ada, org, c.ID())

		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, s.state)
	})

	t.Run("refused while its lifecycle is archived", func(t *testing.T) {
		s := newChannelSetup(t)
		l := s.world.lifecycle(t, wsActive, "hotfix-path", false)
		c := s.channel(t, s.app, l, "hotfix")
		s.archiveChannel(t, c)
		l.Archive()
		s.setLifecycle(l)
		before := s.state.clone()

		_, err := s.svc.RestoreChannel(ctx, ada, org, c.ID())

		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, s.state)
	})

	t.Run("refused while a rule is scoped to an archived artifact", func(t *testing.T) {
		s := newChannelSetup(t)
		c := s.channel(t, s.app, s.lifecycle, "hotfix", gitRule(ptr(s.api.ID())))
		s.archiveChannel(t, c)
		_, err := s.svc.ArchiveArtifact(ctx, ada, org, s.api.ID())
		require.NoError(t, err, "only rules of active channels hold an artifact")
		before := s.state.clone()

		_, err = s.svc.RestoreChannel(ctx, ada, org, c.ID())

		require.ErrorIs(t, err, delivery.ErrArchived)
		assert.Equal(t, before, s.state)
	})
}
