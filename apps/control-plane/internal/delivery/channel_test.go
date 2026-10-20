package delivery

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

func domChannelProps(rules ...NewChannelRuleProps) NewChannelProps {
	return NewChannelProps{
		OrganizationID: domOrg,
		ApplicationID:  id.New("app"),
		LifecycleID:    id.New("lfc"),
		Slug:           "mainline",
		DisplayName:    "Mainline",
		Description:    new("Regular releases"),
		Rules:          rules,
	}
}

func domNewChannel(t *testing.T, rules ...NewChannelRuleProps) *Channel {
	t.Helper()
	c, err := NewChannel(domChannelProps(rules...), nil)
	require.NoError(t, err)
	return c
}

func domVersionRule(config string) ChannelRuleProps {
	return ChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(config)}
}

func TestNewChannel(t *testing.T) {
	clock.Fix(t, domT0)
	art := id.New("art")
	props := domChannelProps(
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{ "range": "1.0" }`)},
		NewChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: &art, Config: json.RawMessage(`{"patterns": ["refs/heads/main"]}`)},
	)
	props.IsDefault = true

	c, err := NewChannel(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("chn", c.ID()), "a new channel gets a chn_ ID: %q", c.ID())
	assert.Equal(t, domOrg, c.OrganizationID())
	assert.Equal(t, props.ApplicationID, c.ApplicationID())
	assert.Equal(t, props.LifecycleID, c.LifecycleID())
	assert.Equal(t, "mainline", c.Slug())
	assert.Equal(t, "Mainline", c.DisplayName())
	assert.Equal(t, "Regular releases", *c.Description())
	assert.True(t, c.IsDefault())
	assert.Equal(t, domT0, c.CreatedAt())
	assert.Equal(t, domT0, c.UpdatedAt())
	assert.Nil(t, c.ArchivedAt())
	assert.False(t, c.Archived())

	rules := c.Rules()
	require.Len(t, rules, 2)
	for _, r := range rules {
		assert.True(t, id.Valid("chr", r.ID), "a new rule gets a chr_ ID: %q", r.ID)
		assert.Equal(t, domT0, r.CreatedAt)
		assert.Equal(t, domT0, r.UpdatedAt)
	}
	assert.Equal(t, release.RuleVersion, rules[0].Kind)
	assert.Nil(t, rules[0].ArtifactID)
	assert.Equal(t, `{"range":"1.0"}`, string(rules[0].Config), "rule configuration is stored as canonical JSON")
	assert.Equal(t, release.RuleGitRef, rules[1].Kind)
	assert.Equal(t, art, *rules[1].ArtifactID)
	assert.Equal(t, `{"patterns":["refs/heads/main"]}`, string(rules[1].Config))
	assert.Equal(t, []string{art}, c.ArtifactIDs())
}

func TestNewChannelOptions(t *testing.T) {
	t.Run("slug derived from display name", func(t *testing.T) {
		props := domChannelProps()
		props.Slug = ""
		props.DisplayName = "Hot Fix"
		c, err := NewChannel(props, nil)
		require.NoError(t, err)
		assert.Equal(t, "hot-fix", c.Slug())
	})

	t.Run("empty description is none", func(t *testing.T) {
		props := domChannelProps()
		props.Description = new("")
		c, err := NewChannel(props, nil)
		require.NoError(t, err)
		assert.Nil(t, c.Description())
	})

	t.Run("empty artifact scope is none", func(t *testing.T) {
		c := domNewChannel(t, NewChannelRuleProps{Kind: release.RuleVersion, ArtifactID: new(""), Config: json.RawMessage(`{"pattern":"^v"}`)})
		assert.Nil(t, c.Rules()[0].ArtifactID)
		assert.Empty(t, c.ArtifactIDs())
	})

	t.Run("max rules is allowed", func(t *testing.T) {
		rules := make([]NewChannelRuleProps, maxChannelRules)
		for i := range rules {
			rules[i] = NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)}
		}
		c := domNewChannel(t, rules...)
		assert.Len(t, c.Rules(), maxChannelRules)
	})

	t.Run("an archived channel that is not the default", func(t *testing.T) {
		props := domChannelProps()
		props.ArchivedAt = new(domT0)
		c, err := NewChannel(props, nil)
		require.NoError(t, err)
		assert.True(t, c.Archived())
	})
}

func TestNewChannelWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("chn")
	ruleID := id.New("chr")
	props := domChannelProps(NewChannelRuleProps{
		ID: &ruleID, Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`), CreatedAt: new(domT0), UpdatedAt: new(domT1),
	})
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.ArchivedAt = new(domT1)

	c, err := NewChannel(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, c.ID())
	assert.Equal(t, domT0, c.CreatedAt())
	assert.Equal(t, domT1, c.UpdatedAt())
	assert.Equal(t, domT1, *c.ArchivedAt())
	rule := c.Rules()[0]
	assert.Equal(t, ruleID, rule.ID, "a given rule ID is kept")
	assert.Equal(t, domT0, rule.CreatedAt)
	assert.Equal(t, domT1, rule.UpdatedAt)
}

func TestNewChannelIsRefused(t *testing.T) {
	tooMany := make([]NewChannelRuleProps, maxChannelRules+1)
	for i := range tooMany {
		tooMany[i] = NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)}
	}
	tests := []struct {
		name   string
		mutate func(*NewChannelProps)
		id     *string
		err    error
	}{
		{"no organization", func(p *NewChannelProps) { p.OrganizationID = "" }, nil, ErrInvalid},
		{"no application", func(p *NewChannelProps) { p.ApplicationID = "" }, nil, ErrInvalid},
		{"application ID with wrong prefix", func(p *NewChannelProps) { p.ApplicationID = id.New("art") }, nil, ErrInvalid},
		{"no lifecycle", func(p *NewChannelProps) { p.LifecycleID = "" }, nil, ErrInvalid},
		{"lifecycle ID with wrong prefix", func(p *NewChannelProps) { p.LifecycleID = id.New("lfp") }, nil, ErrInvalid},
		{"ID with wrong prefix", func(*NewChannelProps) {}, new(id.New("chr")), ErrInvalid},
		{"malformed ID", func(*NewChannelProps) {}, new("chn_?"), ErrInvalid},
		{"no display name", func(p *NewChannelProps) { p.DisplayName = "" }, nil, ErrInvalid},
		{"bad slug", func(p *NewChannelProps) { p.Slug = "Main" }, nil, ErrInvalid},
		{"default and archived", func(p *NewChannelProps) { p.IsDefault = true; p.ArchivedAt = new(domT0) }, nil, ErrDefaultChannel},
		{"too many rules", func(p *NewChannelProps) { p.Rules = tooMany }, nil, ErrInvalid},
		{"rule ID with wrong prefix", func(p *NewChannelProps) {
			p.Rules = []NewChannelRuleProps{{ID: new(id.New("chn")), Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)}}
		}, nil, ErrInvalid},
		{"rule scoped to a non-artifact", func(p *NewChannelProps) {
			p.Rules = []NewChannelRuleProps{{Kind: release.RuleVersion, ArtifactID: new(id.New("app")), Config: json.RawMessage(`{"pattern":"^v"}`)}}
		}, nil, ErrInvalid},
		{"rule with unknown kind", func(p *NewChannelProps) {
			p.Rules = []NewChannelRuleProps{{Kind: "label", Config: json.RawMessage(`{}`)}}
		}, nil, ErrInvalid},
		{"rule with invalid config", func(p *NewChannelProps) {
			p.Rules = []NewChannelRuleProps{{Kind: release.RuleVersion, Config: json.RawMessage(`{"range":"1.0","pattern":"^v"}`)}}
		}, nil, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domChannelProps()
			tt.mutate(&props)
			_, err := NewChannel(props, tt.id)
			assert.ErrorIs(t, err, tt.err)
		})
	}
}

func TestChannelRuleConfigIsValidated(t *testing.T) {
	tests := []struct {
		name   string
		kind   release.RuleKind
		config string
	}{
		{"no config", release.RuleVersion, ``},
		{"not JSON", release.RuleVersion, `range=1.0`},
		{"not an object", release.RuleVersion, `"1.0"`},
		{"two objects", release.RuleVersion, `{"range":"1.0"} {"range":"2.0"}`},
		{"unknown field", release.RuleVersion, `{"range":"1.0","channel":"beta"}`},
		{"empty version rule", release.RuleVersion, `{}`},
		{"bad range", release.RuleVersion, `{"range":"[1.0"}`},
		{"bad pattern", release.RuleVersion, `{"pattern":"("}`},
		{"pre-release with a pattern", release.RuleVersion, `{"pattern":"^v","pre_release":"^$"}`},
		{"git_ref without patterns", release.RuleGitRef, `{"patterns":[]}`},
		{"git_ref with a short ref", release.RuleGitRef, `{"patterns":["main"]}`},
		{"version config on a git_ref rule", release.RuleGitRef, `{"range":"1.0"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := domNewChannel(t)
			before := c.Snapshot()

			_, err := c.AddRule(ChannelRuleProps{Kind: tt.kind, Config: json.RawMessage(tt.config)})

			assert.ErrorIs(t, err, ErrInvalid)
			assert.Equal(t, before, c.Snapshot())
		})
	}
}

func TestChannelRuleConfigIsNormalized(t *testing.T) {
	tests := []struct {
		name   string
		kind   release.RuleKind
		config string
		want   string
	}{
		{"whitespace", release.RuleVersion, "{ \"range\" : \"1.0\" }\n", `{"range":"1.0"}`},
		{"range with pre-release", release.RuleVersion, `{"pre_release":"^$", "range":"[2.0,3.0)"}`, `{"range":"[2.0,3.0)","pre_release":"^$"}`},
		{"pattern", release.RuleVersion, `{"pattern":"^build-\\d+$"}`, `{"pattern":"^build-\\d+$"}`},
		{"git_ref", release.RuleGitRef, "{\n  \"patterns\": [\"refs/heads/main\", \"refs/heads/hotfix/**\"]\n}", `{"patterns":["refs/heads/main","refs/heads/hotfix/**"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := domNewChannel(t)
			rule, err := c.AddRule(ChannelRuleProps{Kind: tt.kind, Config: json.RawMessage(tt.config)})
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(rule.Config))
		})
	}
}

func TestChannelGettersReturnCopies(t *testing.T) {
	art := id.New("art")
	props := domChannelProps(NewChannelRuleProps{Kind: release.RuleVersion, ArtifactID: &art, Config: json.RawMessage(`{"pattern":"^v"}`)})
	props.ArchivedAt = new(domT0)
	c, err := NewChannel(props, nil)
	require.NoError(t, err)
	before := c.Snapshot()

	*props.Description = "changed"
	*props.Rules[0].ArtifactID = "changed"
	*c.Description() = "changed"
	*c.ArchivedAt() = domT2
	rules := c.Rules()
	*rules[0].ArtifactID = "changed"
	rules[0].Config[0] = 'X'
	c.ArtifactIDs()[0] = "changed"
	snap := c.Snapshot()
	*snap.Description = "changed"
	snap.Rules[0].Config[1] = 'X'

	assert.Equal(t, before, c.Snapshot())
}

func TestChannelSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	props := domChannelProps(NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)})
	c, err := NewChannel(props, nil)
	require.NoError(t, err)

	assert.Equal(t, ChannelSnapshot{
		ID:             c.ID(),
		OrganizationID: domOrg,
		ApplicationID:  props.ApplicationID,
		LifecycleID:    props.LifecycleID,
		Slug:           "mainline",
		DisplayName:    "Mainline",
		Description:    new("Regular releases"),
		Rules: []ChannelRuleSnapshot{{
			ID: c.Rules()[0].ID, Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`), CreatedAt: domT0, UpdatedAt: domT0,
		}},
		CreatedAt: domT0,
		UpdatedAt: domT0,
	}, c.Snapshot())
}

func TestChannelUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t)
	clock.Fix(t, domT1)

	require.NoError(t, c.Update(UpdateChannelProps{Slug: new("hotfix"), DisplayName: new("Hotfix"), Description: new("Urgent fixes")}))

	assert.Equal(t, "hotfix", c.Slug())
	assert.Equal(t, "Hotfix", c.DisplayName())
	assert.Equal(t, "Urgent fixes", *c.Description())
	assert.Equal(t, domT1, c.UpdatedAt())

	t.Run("unset fields are kept", func(t *testing.T) {
		require.NoError(t, c.Update(UpdateChannelProps{DisplayName: new("Hot")}))
		assert.Equal(t, "hotfix", c.Slug())
		assert.Equal(t, "Urgent fixes", *c.Description())
	})

	t.Run("an empty description clears it", func(t *testing.T) {
		require.NoError(t, c.Update(UpdateChannelProps{Description: new("")}))
		assert.Nil(t, c.Description())
	})
}

func TestChannelUpdateIsAtomic(t *testing.T) {
	tests := []struct {
		name  string
		props UpdateChannelProps
	}{
		{"bad slug", UpdateChannelProps{Slug: new("Hot Fix"), DisplayName: new("Hotfix"), Description: new("x")}},
		{"bad display name", UpdateChannelProps{Slug: new("hotfix"), DisplayName: new(""), Description: new("x")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			c := domNewChannel(t)
			before := c.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, c.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, c.Snapshot())
		})
	}
}

func TestChannelRefusesChangesWhenArchived(t *testing.T) {
	c := domNewChannel(t, NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)})
	ruleID := c.Rules()[0].ID
	require.NoError(t, c.Archive())
	before := c.Snapshot()

	assert.ErrorIs(t, c.Update(UpdateChannelProps{DisplayName: new("New")}), ErrArchived)
	assert.ErrorIs(t, c.UseLifecycle(id.New("lfc")), ErrArchived)
	_, err := c.AddRule(domVersionRule(`{"pattern":"^x"}`))
	assert.ErrorIs(t, err, ErrArchived)
	assert.ErrorIs(t, c.UpdateRule(ruleID, domVersionRule(`{"pattern":"^x"}`)), ErrArchived)
	assert.ErrorIs(t, c.RemoveRule(ruleID), ErrArchived)
	assert.ErrorIs(t, c.MakeDefault(), ErrArchived)
	assert.Equal(t, before, c.Snapshot())
}

func TestChannelUseLifecycle(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t)
	lfc := id.New("lfc")

	clock.Fix(t, domT1)
	require.NoError(t, c.UseLifecycle(lfc))
	assert.Equal(t, lfc, c.LifecycleID())
	assert.Equal(t, domT1, c.UpdatedAt())

	before := c.Snapshot()
	assert.ErrorIs(t, c.UseLifecycle(id.New("lfp")), ErrInvalid)
	assert.ErrorIs(t, c.UseLifecycle(""), ErrInvalid)
	assert.Equal(t, before, c.Snapshot())
}

func TestChannelAddRule(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t, NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)})
	art := id.New("art")

	clock.Fix(t, domT1)
	rule, err := c.AddRule(ChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: &art, Config: json.RawMessage(`{"patterns":["refs/heads/main"]}`)})

	require.NoError(t, err)
	assert.True(t, id.Valid("chr", rule.ID))
	assert.Equal(t, release.RuleGitRef, rule.Kind)
	assert.Equal(t, art, *rule.ArtifactID)
	assert.Equal(t, domT1, rule.CreatedAt)
	assert.Equal(t, domT1, rule.UpdatedAt)
	require.Len(t, c.Rules(), 2)
	assert.Equal(t, rule, c.Rules()[1], "rules keep the order they were added")
	assert.Equal(t, domT1, c.UpdatedAt())

	art = "changed"
	assert.NotEqual(t, "changed", *c.Rules()[1].ArtifactID, "the given scope is copied")

	t.Run("scope must be an artifact ID", func(t *testing.T) {
		before := c.Snapshot()
		_, err := c.AddRule(ChannelRuleProps{Kind: release.RuleVersion, ArtifactID: new(id.New("reg")), Config: json.RawMessage(`{"pattern":"^v"}`)})
		assert.ErrorIs(t, err, ErrInvalid)
		assert.Equal(t, before, c.Snapshot())
	})
}

func TestChannelAddRuleLimit(t *testing.T) {
	c := domNewChannel(t)
	for range maxChannelRules {
		_, err := c.AddRule(domVersionRule(`{"pattern":"^v"}`))
		require.NoError(t, err)
	}
	before := c.Snapshot()

	_, err := c.AddRule(domVersionRule(`{"pattern":"^v"}`))

	assert.ErrorIs(t, err, ErrInvalid)
	assert.Equal(t, before, c.Snapshot())
}

func TestChannelUpdateRule(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t,
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^v"}`)},
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"range":"1.0"}`)},
	)
	target := c.Rules()[1]
	art := id.New("art")

	clock.Fix(t, domT1)
	require.NoError(t, c.UpdateRule(target.ID, ChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: &art, Config: json.RawMessage(`{ "patterns": ["refs/tags/*"] }`)}))

	got := c.Rules()[1]
	assert.Equal(t, target.ID, got.ID, "the rule keeps its ID")
	assert.Equal(t, release.RuleGitRef, got.Kind)
	assert.Equal(t, art, *got.ArtifactID)
	assert.Equal(t, `{"patterns":["refs/tags/*"]}`, string(got.Config))
	assert.Equal(t, domT0, got.CreatedAt)
	assert.Equal(t, domT1, got.UpdatedAt)
	assert.Equal(t, domT1, c.UpdatedAt())
	assert.Equal(t, domT0, c.Rules()[0].UpdatedAt, "other rules are untouched")

	t.Run("a nil scope removes it", func(t *testing.T) {
		require.NoError(t, c.UpdateRule(target.ID, domVersionRule(`{"range":"2.0"}`)))
		assert.Nil(t, c.Rules()[1].ArtifactID)
		assert.Empty(t, c.ArtifactIDs())
	})

	tests := []struct {
		name   string
		ruleID string
		props  ChannelRuleProps
		err    error
	}{
		{"unknown rule", id.New("chr"), domVersionRule(`{"pattern":"^v"}`), ErrNotFound},
		{"invalid config", target.ID, domVersionRule(`{"range":"nope"}`), ErrInvalid},
		{"unknown kind", target.ID, ChannelRuleProps{Kind: "label", Config: json.RawMessage(`{}`)}, ErrInvalid},
		{"scope not an artifact", target.ID, ChannelRuleProps{Kind: release.RuleVersion, ArtifactID: new(id.New("app")), Config: json.RawMessage(`{"pattern":"^v"}`)}, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := c.Snapshot()
			assert.ErrorIs(t, c.UpdateRule(tt.ruleID, tt.props), tt.err)
			assert.Equal(t, before, c.Snapshot())
		})
	}
}

func TestChannelRemoveRule(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t,
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^a"}`)},
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^b"}`)},
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^c"}`)},
	)
	rules := c.Rules()

	clock.Fix(t, domT1)
	require.NoError(t, c.RemoveRule(rules[1].ID))

	assert.Equal(t, []ChannelRuleSnapshot{rules[0], rules[2]}, c.Rules())
	assert.Equal(t, domT1, c.UpdatedAt())

	before := c.Snapshot()
	assert.ErrorIs(t, c.RemoveRule(rules[1].ID), ErrNotFound)
	assert.Equal(t, before, c.Snapshot())
}

func TestChannelArtifactIDs(t *testing.T) {
	art1, art2 := id.New("art"), id.New("art")
	c := domNewChannel(t,
		NewChannelRuleProps{Kind: release.RuleVersion, ArtifactID: &art2, Config: json.RawMessage(`{"pattern":"^a"}`)},
		NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"pattern":"^b"}`)},
		NewChannelRuleProps{Kind: release.RuleGitRef, ArtifactID: &art1, Config: json.RawMessage(`{"patterns":["refs/heads/main"]}`)},
		NewChannelRuleProps{Kind: release.RuleVersion, ArtifactID: &art2, Config: json.RawMessage(`{"pattern":"^c"}`)},
	)

	assert.Equal(t, []string{art2, art1}, c.ArtifactIDs(), "each scoped artifact once, in rule order")
	assert.Empty(t, domNewChannel(t).ArtifactIDs())
}

func TestChannelDefault(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t)

	clock.Fix(t, domT1)
	require.NoError(t, c.MakeDefault())
	assert.True(t, c.IsDefault())
	assert.Equal(t, domT1, c.UpdatedAt())

	clock.Fix(t, domT2)
	require.NoError(t, c.MakeDefault())
	assert.Equal(t, domT1, c.UpdatedAt(), "making the default the default does nothing")

	t.Run("the default cannot be archived", func(t *testing.T) {
		before := c.Snapshot()
		assert.ErrorIs(t, c.Archive(), ErrDefaultChannel)
		assert.Equal(t, before, c.Snapshot())
	})

	c.ClearDefault()
	assert.False(t, c.IsDefault())
	assert.Equal(t, domT2, c.UpdatedAt())

	clock.Fix(t, domT0)
	c.ClearDefault()
	assert.Equal(t, domT2, c.UpdatedAt(), "clearing a channel that is not the default does nothing")

	require.NoError(t, c.Archive(), "once it is not the default it can be archived")
	assert.True(t, c.Archived())
}

func TestChannelArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	c := domNewChannel(t)

	clock.Fix(t, domT1)
	require.NoError(t, c.Archive())
	assert.True(t, c.Archived())
	assert.Equal(t, domT1, *c.ArchivedAt())
	assert.Equal(t, domT1, c.UpdatedAt())

	clock.Fix(t, domT2)
	require.NoError(t, c.Archive())
	assert.Equal(t, domT1, *c.ArchivedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, c.UpdatedAt())

	c.Restore()
	assert.False(t, c.Archived())
	assert.Nil(t, c.ArchivedAt())
	assert.Equal(t, domT2, c.UpdatedAt())

	clock.Fix(t, domT0)
	c.Restore()
	assert.Equal(t, domT2, c.UpdatedAt(), "restoring an active channel does nothing")
}

func TestNewChannelRefusesRepeatedRuleIDs(t *testing.T) {
	clock.Fix(t, domT0)
	ruleID := id.New(ChannelRule{}.Prefix())
	rule := NewChannelRuleProps{ID: &ruleID, Kind: release.RuleVersion, Config: []byte(`{"range":"1.0"}`)}
	_, err := NewChannel(NewChannelProps{
		OrganizationID: domOrg,
		ApplicationID:  id.New(Application{}.Prefix()),
		LifecycleID:    id.New(Lifecycle{}.Prefix()),
		DisplayName:    "Mainline",
		Rules:          []NewChannelRuleProps{rule, rule},
	}, nil)
	assert.ErrorIs(t, err, ErrInvalid)
}
