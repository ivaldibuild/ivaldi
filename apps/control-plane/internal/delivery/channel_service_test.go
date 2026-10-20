package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// svcChannelSetup is an Application with two artifacts, one archived, and
// another Application with an artifact of its own.
type svcChannelSetup struct {
	*svcFixture
	lifecycle   *Lifecycle
	app         *Application
	defaultCh   *Channel
	api         *Artifact
	oldArtifact *Artifact
	foreign     *Artifact
}

func newSvcChannelSetup(t *testing.T) *svcChannelSetup {
	t.Helper()
	f := newSvcFixture(t)
	s := &svcChannelSetup{svcFixture: f}
	s.lifecycle = f.lifecycle(t, "mainline")
	s.app, s.defaultCh = f.app(t, "api", s.lifecycle)
	s.api = f.artifact(t, s.app, "api")
	s.oldArtifact = f.artifact(t, s.app, "old")
	_, err := f.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, s.oldArtifact.ID())
	require.NoError(t, err)
	other, _ := f.app(t, "worker", s.lifecycle)
	s.foreign = f.artifact(t, other, "worker")
	return s
}

func TestServiceCreateChannel(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		s := newSvcChannelSetup(t)

		c, err := s.svc.CreateChannel(svcCtx, svcAlice, CreateChannelInput{
			OrganizationID: svcOrg, ApplicationID: s.app.ID(), LifecycleID: s.lifecycle.ID(), DisplayName: "Hotfix", Description: "Urgent fixes",
			Rules: []ChannelRuleProps{svcGitRule(new(s.api.ID())), svcGitRule(nil)},
		})

		require.NoError(t, err)
		assert.True(t, id.Valid(Channel{}.Prefix(), c.ID()))
		assert.Equal(t, "hotfix", c.Slug())
		assert.False(t, c.IsDefault())
		assert.Len(t, c.Rules(), 2)
		assert.Equal(t, []string{s.api.ID()}, c.ArtifactIDs())
		ev := s.audit.last()
		assert.Equal(t, EventChannelCreated, ev.Type)
		assert.Equal(t, c.Snapshot(), ev.After)
		stored, err := s.store.GetChannel(svcCtx, svcOrg, c.ID())
		require.NoError(t, err)
		assert.Equal(t, c.Snapshot(), stored.Snapshot(), "rules are stored with their IDs")
	})

	refusals := []struct {
		name    string
		in      func(*svcChannelSetup) CreateChannelInput
		wantErr error
	}{
		{"rule scoped to another application's artifact", func(s *svcChannelSetup) CreateChannelInput {
			return CreateChannelInput{LifecycleID: s.lifecycle.ID(), Rules: []ChannelRuleProps{svcGitRule(new(s.foreign.ID()))}}
		}, ErrNotFound},
		{"rule scoped to a missing artifact", func(s *svcChannelSetup) CreateChannelInput {
			return CreateChannelInput{LifecycleID: s.lifecycle.ID(), Rules: []ChannelRuleProps{svcGitRule(new("art_01ARZ3NDEKTSV4RRFFQ69G5FAW"))}}
		}, ErrNotFound},
		{"rule scoped to an archived artifact", func(s *svcChannelSetup) CreateChannelInput {
			return CreateChannelInput{LifecycleID: s.lifecycle.ID(), Rules: []ChannelRuleProps{svcGitRule(new(s.oldArtifact.ID()))}}
		}, ErrArchived},
		{"lifecycle in another workspace", func(s *svcChannelSetup) CreateChannelInput {
			other, _ := s.addWorkspace(t, "data", "etl")
			return CreateChannelInput{LifecycleID: s.lifecycleIn(t, other.ID(), "l").ID()}
		}, ErrWorkspaceMismatch},
		{"archived lifecycle", func(s *svcChannelSetup) CreateChannelInput {
			l := s.lifecycle2(t)
			return CreateChannelInput{LifecycleID: l.ID()}
		}, ErrArchived},
		{"slug taken", func(s *svcChannelSetup) CreateChannelInput {
			return CreateChannelInput{LifecycleID: s.lifecycle.ID(), Slug: DefaultChannelSlug}
		}, ErrSlugTaken},
		{"archived application", func(s *svcChannelSetup) CreateChannelInput {
			_, err := s.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, s.app.ID())
			require.NoError(t, err)
			return CreateChannelInput{LifecycleID: s.lifecycle.ID()}
		}, ErrArchived},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			s := newSvcChannelSetup(t)
			in := tt.in(s)
			in.OrganizationID = svcOrg
			in.ApplicationID = s.app.ID()
			in.DisplayName = "Hotfix"
			state, events, resources := s.marks()

			_, err := s.svc.CreateChannel(svcCtx, svcAlice, in)

			require.ErrorIs(t, err, tt.wantErr)
			s.requireUnchanged(t, state, events, resources)
		})
	}
}

// lifecycle2 returns an archived Lifecycle in the setup's Workspace.
func (s *svcChannelSetup) lifecycle2(t *testing.T) *Lifecycle {
	t.Helper()
	l := s.svcFixture.lifecycle(t, "archived")
	_, err := s.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
	require.NoError(t, err)
	return l
}

func TestServiceGetAndListChannels(t *testing.T) {
	s := newSvcChannelSetup(t)
	c := s.channel(t, s.app, s.lifecycle, "hotfix")
	_, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, c.ID())
	require.NoError(t, err)

	got, err := s.svc.GetChannel(svcCtx, svcAlice, svcOrg, s.defaultCh.ID())
	require.NoError(t, err)
	assert.Equal(t, s.defaultCh.Snapshot(), got.Snapshot())
	_, err = s.svc.GetChannel(svcCtx, svcAlice, svcOrg, "chn_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	require.ErrorIs(t, err, ErrNotFound)

	active, err := s.svc.ListChannels(svcCtx, svcAlice, svcOrg, s.app.ID(), false)
	require.NoError(t, err)
	assert.Len(t, active, 1)
	all, err := s.svc.ListChannels(svcCtx, svcAlice, svcOrg, s.app.ID(), true)
	require.NoError(t, err)
	assert.Len(t, all, 2)
}

func TestServiceChannelRules(t *testing.T) {
	s := newSvcChannelSetup(t)
	c := s.channel(t, s.app, s.lifecycle, "hotfix", svcGitRule(nil))
	ruleID := c.Rules()[0].ID

	t.Run("update the channel", func(t *testing.T) {
		got, err := s.svc.UpdateChannel(svcCtx, svcAlice, svcOrg, c.ID(), UpdateChannelProps{Description: new("Fixes")})
		require.NoError(t, err)
		assert.Equal(t, "Fixes", *got.Description())
		assert.Equal(t, EventChannelUpdated, s.audit.last().Type)

		_, err = s.svc.UpdateChannel(svcCtx, svcAlice, svcOrg, c.ID(), UpdateChannelProps{Slug: new(DefaultChannelSlug)})
		require.ErrorIs(t, err, ErrSlugTaken)
	})

	t.Run("add", func(t *testing.T) {
		got, err := s.svc.AddChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), svcGitRule(new(s.api.ID())))
		require.NoError(t, err)
		assert.Len(t, got.Rules(), 2)
		ev := s.audit.last()
		assert.Equal(t, EventChannelUpdated, ev.Type)
		assert.Len(t, ev.Before.(ChannelSnapshot).Rules, 1)
		assert.Len(t, ev.After.(ChannelSnapshot).Rules, 2)
	})

	t.Run("add checks the artifact", func(t *testing.T) {
		state, events, resources := s.marks()
		_, err := s.svc.AddChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), svcGitRule(new(s.foreign.ID())))
		require.ErrorIs(t, err, ErrNotFound)
		_, err = s.svc.AddChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), svcGitRule(new(s.oldArtifact.ID())))
		require.ErrorIs(t, err, ErrArchived)
		s.requireUnchanged(t, state, events, resources)
	})

	t.Run("update checks the artifact", func(t *testing.T) {
		_, err := s.svc.UpdateChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), ruleID, svcGitRule(new(s.foreign.ID())))
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("update", func(t *testing.T) {
		got, err := s.svc.UpdateChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), ruleID, ChannelRuleProps{Kind: "version", ArtifactID: new(s.api.ID()), Config: []byte(`{"pattern":"^v"}`)})
		require.NoError(t, err)
		r := got.Rules()[0]
		assert.Equal(t, ruleID, r.ID, "the rule keeps its ID")
		assert.Equal(t, s.api.ID(), *r.ArtifactID)
	})

	t.Run("an unknown rule", func(t *testing.T) {
		_, err := s.svc.RemoveChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), "chr_01ARZ3NDEKTSV4RRFFQ69G5FAW")
		require.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("remove", func(t *testing.T) {
		got, err := s.svc.RemoveChannelRule(svcCtx, svcAlice, svcOrg, c.ID(), ruleID)
		require.NoError(t, err)
		assert.Len(t, got.Rules(), 1)
		stored, err := s.store.GetChannel(svcCtx, svcOrg, c.ID())
		require.NoError(t, err)
		assert.Equal(t, got.Snapshot(), stored.Snapshot())
	})
}

func TestServiceSetChannelLifecycle(t *testing.T) {
	s := newSvcChannelSetup(t)
	next := s.svcFixture.lifecycle(t, "next")
	other, _ := s.addWorkspace(t, "data", "etl")
	foreign := s.lifecycleIn(t, other.ID(), "foreign")
	archived := s.lifecycle2(t)

	got, err := s.svc.SetChannelLifecycle(svcCtx, svcAlice, svcOrg, s.defaultCh.ID(), next.ID())
	require.NoError(t, err)
	assert.Equal(t, next.ID(), got.LifecycleID())
	ev := s.audit.last()
	assert.Equal(t, EventChannelUpdated, ev.Type)
	assert.Equal(t, s.lifecycle.ID(), ev.Before.(ChannelSnapshot).LifecycleID)

	state, events, resources := s.marks()
	_, err = s.svc.SetChannelLifecycle(svcCtx, svcAlice, svcOrg, s.defaultCh.ID(), foreign.ID())
	require.ErrorIs(t, err, ErrWorkspaceMismatch)
	_, err = s.svc.SetChannelLifecycle(svcCtx, svcAlice, svcOrg, s.defaultCh.ID(), archived.ID())
	require.ErrorIs(t, err, ErrArchived)
	_, err = s.svc.SetChannelLifecycle(svcCtx, svcAlice, svcOrg, s.defaultCh.ID(), "lfc_01ARZ3NDEKTSV4RRFFQ69G5FAW")
	require.ErrorIs(t, err, ErrNotFound)
	s.requireUnchanged(t, state, events, resources)
}

func TestServiceSetDefaultChannel(t *testing.T) {
	s := newSvcChannelSetup(t)
	hotfix := s.channel(t, s.app, s.lifecycle, "hotfix")
	before := s.audit.count()

	got, err := s.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, hotfix.ID())

	require.NoError(t, err)
	assert.True(t, got.IsDefault())
	old, err := s.store.GetChannel(svcCtx, svcOrg, s.defaultCh.ID())
	require.NoError(t, err)
	assert.False(t, old.IsDefault(), "the old default is cleared")
	current, err := s.store.GetDefaultChannel(svcCtx, svcOrg, s.app.ID())
	require.NoError(t, err)
	assert.Equal(t, hotfix.ID(), current.ID())
	events := s.audit.since(before)
	require.Len(t, events, 1, "one event for the move")
	assert.Equal(t, EventChannelDefaultMoved, events[0].Type)
	assert.Equal(t, hotfix.ID(), events[0].ResourceID)
	assert.Equal(t, s.defaultCh.ID(), events[0].Before)
	assert.Equal(t, hotfix.ID(), events[0].After)

	t.Run("archiving the default is refused", func(t *testing.T) {
		state, events, resources := s.marks()
		_, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, hotfix.ID())
		require.ErrorIs(t, err, ErrDefaultChannel)
		s.requireUnchanged(t, state, events, resources)
	})

	t.Run("the old default can now be archived", func(t *testing.T) {
		got, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, s.defaultCh.ID())
		require.NoError(t, err)
		assert.True(t, got.Archived())
		assert.Equal(t, EventChannelArchived, s.audit.last().Type)
	})

	t.Run("an archived channel cannot become the default", func(t *testing.T) {
		state, events, resources := s.marks()
		_, err := s.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, s.defaultCh.ID())
		require.ErrorIs(t, err, ErrArchived)
		s.requireUnchanged(t, state, events, resources)
	})

	t.Run("refused for an archived application", func(t *testing.T) {
		other := s.channel(t, s.app, s.lifecycle, "other")
		_, err := s.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, s.app.ID())
		require.NoError(t, err)
		_, err = s.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, other.ID())
		require.ErrorIs(t, err, ErrArchived)
	})

	t.Run("a missing channel", func(t *testing.T) {
		_, err := s.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, "chn_01ARZ3NDEKTSV4RRFFQ69G5FAW")
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestServiceRestoreChannel(t *testing.T) {
	t.Run("restored", func(t *testing.T) {
		s := newSvcChannelSetup(t)
		c := s.channel(t, s.app, s.lifecycle, "hotfix")
		_, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, c.ID())
		require.NoError(t, err)

		got, err := s.svc.RestoreChannel(svcCtx, svcAlice, svcOrg, c.ID())

		require.NoError(t, err)
		assert.False(t, got.Archived())
		ev := s.audit.last()
		assert.Equal(t, EventChannelRestored, ev.Type)
		assert.NotNil(t, ev.Before.(ChannelSnapshot).ArchivedAt)
	})

	t.Run("refused for an archived application", func(t *testing.T) {
		s := newSvcChannelSetup(t)
		c := s.channel(t, s.app, s.lifecycle, "hotfix")
		_, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, c.ID())
		require.NoError(t, err)
		_, err = s.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, s.app.ID())
		require.NoError(t, err)
		state, events, resources := s.marks()

		_, err = s.svc.RestoreChannel(svcCtx, svcAlice, svcOrg, c.ID())

		require.ErrorIs(t, err, ErrArchived)
		s.requireUnchanged(t, state, events, resources)
	})

	t.Run("refused while its lifecycle is archived", func(t *testing.T) {
		s := newSvcChannelSetup(t)
		l := s.svcFixture.lifecycle(t, "hotfix-path")
		c := s.channel(t, s.app, l, "hotfix")
		_, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, c.ID())
		require.NoError(t, err)
		_, err = s.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, l.ID())
		require.NoError(t, err)
		state, events, resources := s.marks()

		_, err = s.svc.RestoreChannel(svcCtx, svcAlice, svcOrg, c.ID())

		require.ErrorIs(t, err, ErrArchived)
		s.requireUnchanged(t, state, events, resources)
	})
}

func TestServiceRestoreChannelChecksItsRuleArtifacts(t *testing.T) {
	s := newSvcChannelSetup(t)
	c := s.channel(t, s.app, s.lifecycle, "hotfix", svcGitRule(new(s.api.ID())))
	_, err := s.svc.ArchiveChannel(svcCtx, svcAlice, svcOrg, c.ID())
	require.NoError(t, err)
	_, err = s.svc.ArchiveArtifact(svcCtx, svcAlice, svcOrg, s.api.ID())
	require.NoError(t, err, "only rules of active channels hold an artifact")
	state, events, resources := s.marks()

	_, err = s.svc.RestoreChannel(svcCtx, svcAlice, svcOrg, c.ID())

	require.ErrorIs(t, err, ErrArchived, "the channel's rules are scoped to an archived artifact")
	s.requireUnchanged(t, state, events, resources)
}

func TestServiceArchivedApplicationIsReadOnly(t *testing.T) {
	s := newSvcChannelSetup(t)
	_, err := s.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, s.app.ID())
	require.NoError(t, err)
	state, events, resources := s.marks()

	_, err = s.svc.UpdateChannel(svcCtx, svcAlice, svcOrg, s.defaultCh.ID(), UpdateChannelProps{DisplayName: new("Renamed")})
	assert.ErrorIs(t, err, ErrArchived)
	_, err = s.svc.AddChannelRule(svcCtx, svcAlice, svcOrg, s.defaultCh.ID(), svcGitRule(nil))
	assert.ErrorIs(t, err, ErrArchived)
	_, err = s.svc.UpdateArtifact(svcCtx, svcAlice, svcOrg, s.api.ID(), UpdateArtifactProps{Required: new(false)})
	assert.ErrorIs(t, err, ErrArchived)
	_, err = s.svc.DeclareArtifact(svcCtx, svcAlice, svcOrg, s.api.ID())
	assert.ErrorIs(t, err, ErrArchived)
	s.requireUnchanged(t, state, events, resources)
}

func TestServiceArchivedApplicationDoesNotHoldItsLifecycle(t *testing.T) {
	s := newSvcChannelSetup(t)
	solo := s.svcFixture.lifecycle(t, "solo")
	app, _ := s.svcFixture.app(t, "batch", solo)
	_, err := s.svc.ArchiveApplication(svcCtx, svcAlice, svcOrg, app.ID())
	require.NoError(t, err)

	_, err = s.svc.ArchiveLifecycle(svcCtx, svcAlice, svcOrg, solo.ID())
	require.NoError(t, err, "only active applications' channels hold a lifecycle")

	state, events, resources := s.marks()
	_, err = s.svc.RestoreApplication(svcCtx, svcAlice, svcOrg, app.ID())
	assert.ErrorIs(t, err, ErrArchived, "its default channel's lifecycle is archived")
	s.requireUnchanged(t, state, events, resources)

	_, err = s.svc.RestoreLifecycle(svcCtx, svcAlice, svcOrg, solo.ID())
	require.NoError(t, err)
	restored, err := s.svc.RestoreApplication(svcCtx, svcAlice, svcOrg, app.ID())
	require.NoError(t, err)
	assert.False(t, restored.Archived())
}

func TestServiceConfigurePlacementValidatesBeforeAuthorizing(t *testing.T) {
	s := newSvcChannelSetup(t)
	calls := len(s.authz.requests)

	_, err := s.svc.ConfigurePlacement(svcCtx, svcAlice, svcOrg, s.app.ID(), id.New(Target{}.Prefix()), &ApplyConfig{DefinitionArtifact: "Not A Name"})

	assert.ErrorIs(t, err, ErrInvalid)
	assert.Len(t, s.authz.requests, calls, "nothing was authorized")
}
