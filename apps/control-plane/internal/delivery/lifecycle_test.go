package delivery

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func domEnvIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = id.New("env")
	}
	return ids
}

func domLifecycleProps(phases ...NewLifecyclePhaseProps) NewLifecycleProps {
	return NewLifecycleProps{
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		Slug:           "mainline",
		DisplayName:    "Mainline",
		Description:    new("The usual path"),
		Phases:         phases,
	}
}

// domNewLifecycle returns a Lifecycle with a phase per Environment group.
func domNewLifecycle(t *testing.T, groups ...[]string) *Lifecycle {
	t.Helper()
	var phases []NewLifecyclePhaseProps
	for i, envs := range groups {
		phases = append(phases, NewLifecyclePhaseProps{Name: "Phase " + string(rune('A'+i)), EnvironmentIDs: envs})
	}
	l, err := NewLifecycle(domLifecycleProps(phases...), nil)
	require.NoError(t, err)
	return l
}

func domPhaseNames(l *Lifecycle) []string {
	var names []string
	for _, p := range l.Phases() {
		names = append(names, p.Name)
	}
	return names
}

func TestNewLifecycle(t *testing.T) {
	clock.Fix(t, domT0)
	envs := domEnvIDs(3)

	l, err := NewLifecycle(domLifecycleProps(
		NewLifecyclePhaseProps{Name: "Dev", EnvironmentIDs: envs[:1]},
		NewLifecyclePhaseProps{Name: "Prod", Optional: true, MinEnvironments: new(1), EnvironmentIDs: []string{envs[2], envs[1]}},
	), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("lfc", l.ID()), "a new lifecycle gets an lfc_ ID: %q", l.ID())
	assert.Equal(t, domOrg, l.OrganizationID())
	assert.Equal(t, domWorkspace, l.WorkspaceID())
	assert.Equal(t, "mainline", l.Slug())
	assert.Equal(t, "Mainline", l.DisplayName())
	require.NotNil(t, l.Description())
	assert.Equal(t, "The usual path", *l.Description())
	assert.Equal(t, domT0, l.CreatedAt())
	assert.Equal(t, domT0, l.UpdatedAt())
	assert.Nil(t, l.DeletedAt())
	assert.False(t, l.Archived())

	phases := l.Phases()
	require.Len(t, phases, 2)
	assert.Equal(t, []string{"Dev", "Prod"}, domPhaseNames(l), "phases keep their order")
	for i, p := range phases {
		assert.True(t, id.Valid("lfp", p.ID), "a new phase gets an lfp_ ID: %q", p.ID)
		assert.Equal(t, i, p.Position)
		assert.Equal(t, domT0, p.CreatedAt)
		assert.Equal(t, domT0, p.UpdatedAt)
	}
	assert.False(t, phases[0].Optional)
	assert.Nil(t, phases[0].MinEnvironments)
	assert.True(t, phases[1].Optional)
	require.NotNil(t, phases[1].MinEnvironments)
	assert.Equal(t, 1, *phases[1].MinEnvironments)
	assert.Equal(t, sortedStrings([]string{envs[2], envs[1]}), phases[1].EnvironmentIDs, "a phase's environments are sorted")
	assert.True(t, slices.IsSorted(phases[1].EnvironmentIDs))

	assert.Equal(t, append([]string{envs[0]}, phases[1].EnvironmentIDs...), l.EnvironmentIDs(), "environments come in phase order")
}

func TestNewLifecycleWithoutPhases(t *testing.T) {
	props := domLifecycleProps()
	props.Slug = ""
	props.DisplayName = "Fast Track!"
	props.Description = new("")

	l, err := NewLifecycle(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "fast-track", l.Slug(), "the slug is derived from the display name")
	assert.Nil(t, l.Description(), "an empty description is none")
	assert.Empty(t, l.Phases())
	assert.Empty(t, l.EnvironmentIDs())
}

func TestNewLifecycleWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	lfcID := id.New("lfc")
	phaseID := id.New("lfp")
	props := domLifecycleProps(NewLifecyclePhaseProps{
		ID: &phaseID, Name: "Dev", EnvironmentIDs: domEnvIDs(1), CreatedAt: new(domT0), UpdatedAt: new(domT1),
	})
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.DeletedAt = new(domT1)

	l, err := NewLifecycle(props, &lfcID)

	require.NoError(t, err)
	assert.Equal(t, lfcID, l.ID())
	assert.Equal(t, domT0, l.CreatedAt())
	assert.Equal(t, domT1, l.UpdatedAt())
	require.NotNil(t, l.DeletedAt())
	assert.Equal(t, domT1, *l.DeletedAt())
	assert.True(t, l.Archived())
	phase := l.Phases()[0]
	assert.Equal(t, phaseID, phase.ID, "a given phase ID is kept")
	assert.Equal(t, domT0, phase.CreatedAt)
	assert.Equal(t, domT1, phase.UpdatedAt)
}

func TestNewLifecycleIsRefused(t *testing.T) {
	envs := domEnvIDs(2)
	tests := []struct {
		name   string
		mutate func(*NewLifecycleProps)
		id     *string
	}{
		{"no organization", func(p *NewLifecycleProps) { p.OrganizationID = "" }, nil},
		{"no workspace", func(p *NewLifecycleProps) { p.WorkspaceID = "" }, nil},
		{"no display name", func(p *NewLifecycleProps) { p.DisplayName = "" }, nil},
		{"bad slug", func(p *NewLifecycleProps) { p.Slug = "-main" }, nil},
		{"ID with wrong prefix", func(*NewLifecycleProps) {}, new(id.New("env"))},
		{"malformed ID", func(*NewLifecycleProps) {}, new("lfc_123")},
		{"phase without a name", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{EnvironmentIDs: envs[:1]}}
		}, nil},
		{"phase name too long", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: strings.Repeat("x", maxDisplayNameLength+1), EnvironmentIDs: envs[:1]}}
		}, nil},
		{"phase without environments", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: "Dev"}}
		}, nil},
		{"phase with a non-environment ID", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: "Dev", EnvironmentIDs: []string{id.New("tgt")}}}
		}, nil},
		{"phase with a malformed environment ID", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: "Dev", EnvironmentIDs: []string{"staging"}}}
		}, nil},
		{"environment twice in one phase", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: "Dev", EnvironmentIDs: []string{envs[0], envs[0]}}}
		}, nil},
		{"environment in two phases", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{
				{Name: "Dev", EnvironmentIDs: envs[:1]},
				{Name: "Prod", EnvironmentIDs: []string{envs[1], envs[0]}},
			}
		}, nil},
		{"min environments of zero", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: "Dev", MinEnvironments: new(0), EnvironmentIDs: envs}}
		}, nil},
		{"min environments above the count", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{Name: "Dev", MinEnvironments: new(3), EnvironmentIDs: envs}}
		}, nil},
		{"phase ID with wrong prefix", func(p *NewLifecycleProps) {
			p.Phases = []NewLifecyclePhaseProps{{ID: new(id.New("lfc")), Name: "Dev", EnvironmentIDs: envs[:1]}}
		}, nil},
		{"too many phases", func(p *NewLifecycleProps) {
			for i, env := range domEnvIDs(maxPhases + 1) {
				p.Phases = append(p.Phases, NewLifecyclePhaseProps{Name: "Phase " + strings.Repeat("x", i+1), EnvironmentIDs: []string{env}})
			}
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domLifecycleProps()
			tt.mutate(&props)
			_, err := NewLifecycle(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("min environments may equal the count", func(t *testing.T) {
		_, err := NewLifecycle(domLifecycleProps(NewLifecyclePhaseProps{Name: "Dev", MinEnvironments: new(2), EnvironmentIDs: envs}), nil)
		assert.NoError(t, err)
	})

	t.Run("max phases is allowed", func(t *testing.T) {
		props := domLifecycleProps()
		for _, env := range domEnvIDs(maxPhases) {
			props.Phases = append(props.Phases, NewLifecyclePhaseProps{Name: "Phase", EnvironmentIDs: []string{env}})
		}
		_, err := NewLifecycle(props, nil)
		assert.NoError(t, err)
	})
}

func TestLifecycleGettersReturnCopies(t *testing.T) {
	envs := domEnvIDs(2)
	props := domLifecycleProps(NewLifecyclePhaseProps{Name: "Dev", MinEnvironments: new(1), EnvironmentIDs: envs})
	l, err := NewLifecycle(props, nil)
	require.NoError(t, err)
	before := l.Snapshot()

	*props.Description = "changed"
	*props.Phases[0].MinEnvironments = 2
	props.Phases[0].EnvironmentIDs[0] = "changed"

	*l.Description() = "changed"
	phases := l.Phases()
	*phases[0].MinEnvironments = 2
	phases[0].EnvironmentIDs[0] = "changed"
	l.EnvironmentIDs()[0] = "changed"
	snap := l.Snapshot()
	*snap.Description = "changed"
	snap.Phases[0].EnvironmentIDs[0] = "changed"

	assert.Equal(t, before, l.Snapshot())
}

func TestLifecycleSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	envs := domEnvIDs(1)
	l := domNewLifecycle(t, envs)

	assert.Equal(t, LifecycleSnapshot{
		ID:             l.ID(),
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		Slug:           "mainline",
		DisplayName:    "Mainline",
		Description:    new("The usual path"),
		Phases: []LifecyclePhaseSnapshot{{
			ID: l.Phases()[0].ID, Position: 0, Name: "Phase A", EnvironmentIDs: envs, CreatedAt: domT0, UpdatedAt: domT0,
		}},
		CreatedAt: domT0,
		UpdatedAt: domT0,
	}, l.Snapshot())
}

func TestLifecycleUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	l := domNewLifecycle(t)
	clock.Fix(t, domT1)

	require.NoError(t, l.Update(UpdateLifecycleProps{Slug: new("fast"), DisplayName: new("Fast"), Description: new("Quick")}))

	assert.Equal(t, "fast", l.Slug())
	assert.Equal(t, "Fast", l.DisplayName())
	assert.Equal(t, "Quick", *l.Description())
	assert.Equal(t, domT1, l.UpdatedAt())

	t.Run("unset fields are kept", func(t *testing.T) {
		require.NoError(t, l.Update(UpdateLifecycleProps{DisplayName: new("Faster")}))
		assert.Equal(t, "fast", l.Slug())
		assert.Equal(t, "Quick", *l.Description())
	})

	t.Run("an empty description clears it", func(t *testing.T) {
		require.NoError(t, l.Update(UpdateLifecycleProps{Description: new("")}))
		assert.Nil(t, l.Description())
	})
}

func TestLifecycleUpdateIsAtomic(t *testing.T) {
	tests := []struct {
		name  string
		props UpdateLifecycleProps
	}{
		{"bad slug", UpdateLifecycleProps{Slug: new("BAD"), DisplayName: new("New"), Description: new("New")}},
		{"bad display name", UpdateLifecycleProps{Slug: new("new"), DisplayName: new(""), Description: new("New")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			l := domNewLifecycle(t)
			before := l.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, l.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, l.Snapshot())
		})
	}
}

func TestLifecycleRefusesChangesWhenArchived(t *testing.T) {
	envs := domEnvIDs(2)
	l := domNewLifecycle(t, envs[:1])
	phaseID := l.Phases()[0].ID
	l.Archive()
	before := l.Snapshot()

	assert.ErrorIs(t, l.Update(UpdateLifecycleProps{DisplayName: new("New")}), ErrArchived)
	_, err := l.AddPhase(LifecyclePhaseProps{Name: "Prod", EnvironmentIDs: envs[1:]})
	assert.ErrorIs(t, err, ErrArchived)
	assert.ErrorIs(t, l.UpdatePhase(phaseID, LifecyclePhaseProps{Name: "Dev", EnvironmentIDs: envs[1:]}), ErrArchived)
	assert.ErrorIs(t, l.RemovePhase(phaseID), ErrArchived)
	assert.ErrorIs(t, l.MovePhase(phaseID, 0), ErrArchived)
	assert.Equal(t, before, l.Snapshot())
}

func TestLifecycleAddPhase(t *testing.T) {
	clock.Fix(t, domT0)
	envs := domEnvIDs(3)
	l := domNewLifecycle(t, envs[:1])
	clock.Fix(t, domT1)

	phase, err := l.AddPhase(LifecyclePhaseProps{Name: "Prod", Optional: true, MinEnvironments: new(1), EnvironmentIDs: []string{envs[2], envs[1]}})

	require.NoError(t, err)
	assert.True(t, id.Valid("lfp", phase.ID))
	assert.Equal(t, 1, phase.Position, "a new phase goes last")
	assert.Equal(t, "Prod", phase.Name)
	assert.True(t, phase.Optional)
	assert.Equal(t, 1, *phase.MinEnvironments)
	assert.Equal(t, sortedStrings(envs[1:]), phase.EnvironmentIDs)
	assert.Equal(t, domT1, phase.CreatedAt)
	assert.Equal(t, domT1, phase.UpdatedAt)
	assert.Equal(t, []string{"Phase A", "Prod"}, domPhaseNames(l))
	assert.Equal(t, phase, l.Phases()[1])
	assert.Equal(t, domT1, l.UpdatedAt())
	assert.Equal(t, domT0, l.Phases()[0].UpdatedAt, "other phases are untouched")
}

func TestLifecycleAddPhaseIsRefused(t *testing.T) {
	envs := domEnvIDs(2)
	tests := []struct {
		name  string
		props LifecyclePhaseProps
	}{
		{"no name", LifecyclePhaseProps{EnvironmentIDs: envs[1:]}},
		{"no environments", LifecyclePhaseProps{Name: "Prod"}},
		{"not an environment ID", LifecyclePhaseProps{Name: "Prod", EnvironmentIDs: []string{id.New("app")}}},
		{"environment already in another phase", LifecyclePhaseProps{Name: "Prod", EnvironmentIDs: []string{envs[1], envs[0]}}},
		{"min environments too high", LifecyclePhaseProps{Name: "Prod", MinEnvironments: new(2), EnvironmentIDs: envs[1:]}},
		{"min environments too low", LifecyclePhaseProps{Name: "Prod", MinEnvironments: new(-1), EnvironmentIDs: envs[1:]}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			l := domNewLifecycle(t, envs[:1])
			before := l.Snapshot()
			clock.Fix(t, domT1)

			_, err := l.AddPhase(tt.props)

			assert.ErrorIs(t, err, ErrInvalid)
			assert.Equal(t, before, l.Snapshot(), "a failed change leaves the phases alone")
		})
	}

	t.Run("too many phases", func(t *testing.T) {
		envs := domEnvIDs(maxPhases + 1)
		groups := make([][]string, maxPhases)
		for i := range groups {
			groups[i] = []string{envs[i]}
		}
		l := domNewLifecycle(t, groups...)
		before := l.Snapshot()

		_, err := l.AddPhase(LifecyclePhaseProps{Name: "One more", EnvironmentIDs: envs[maxPhases:]})

		assert.ErrorIs(t, err, ErrInvalid)
		assert.Equal(t, before, l.Snapshot())
	})
}

func TestLifecycleUpdatePhase(t *testing.T) {
	clock.Fix(t, domT0)
	envs := domEnvIDs(4)
	l := domNewLifecycle(t, envs[:1], envs[1:2])
	phase := l.Phases()[1]
	clock.Fix(t, domT1)

	require.NoError(t, l.UpdatePhase(phase.ID, LifecyclePhaseProps{
		Name: "Production", Optional: true, MinEnvironments: new(2), EnvironmentIDs: []string{envs[3], envs[2]},
	}))

	got := l.Phases()[1]
	assert.Equal(t, phase.ID, got.ID, "the phase keeps its ID")
	assert.Equal(t, 1, got.Position)
	assert.Equal(t, "Production", got.Name)
	assert.True(t, got.Optional)
	assert.Equal(t, 2, *got.MinEnvironments)
	assert.Equal(t, sortedStrings(envs[2:]), got.EnvironmentIDs, "environments are replaced and sorted")
	assert.Equal(t, domT0, got.CreatedAt)
	assert.Equal(t, domT1, got.UpdatedAt)
	assert.Equal(t, domT1, l.UpdatedAt())
	assert.Equal(t, domT0, l.Phases()[0].UpdatedAt, "other phases are untouched")

	t.Run("a phase may keep its own environments", func(t *testing.T) {
		require.NoError(t, l.UpdatePhase(phase.ID, LifecyclePhaseProps{Name: "Production", EnvironmentIDs: envs[2:]}))
		assert.Nil(t, l.Phases()[1].MinEnvironments, "nil min environments means all")
		assert.False(t, l.Phases()[1].Optional)
	})
}

func TestLifecycleUpdatePhaseIsRefused(t *testing.T) {
	envs := domEnvIDs(3)
	tests := []struct {
		name    string
		phaseID func(*Lifecycle) string
		props   LifecyclePhaseProps
		err     error
	}{
		{"unknown phase", func(*Lifecycle) string { return id.New("lfp") }, LifecyclePhaseProps{Name: "X", EnvironmentIDs: envs[2:]}, ErrNotFound},
		{"no name", func(l *Lifecycle) string { return l.Phases()[1].ID }, LifecyclePhaseProps{EnvironmentIDs: envs[2:]}, ErrInvalid},
		{"no environments", func(l *Lifecycle) string { return l.Phases()[1].ID }, LifecyclePhaseProps{Name: "X"}, ErrInvalid},
		{"environment in another phase", func(l *Lifecycle) string { return l.Phases()[1].ID }, LifecyclePhaseProps{Name: "X", EnvironmentIDs: envs[:1]}, ErrInvalid},
		{"min environments too high", func(l *Lifecycle) string { return l.Phases()[1].ID }, LifecyclePhaseProps{Name: "X", MinEnvironments: new(2), EnvironmentIDs: envs[2:]}, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			l := domNewLifecycle(t, envs[:1], envs[1:2])
			before := l.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, l.UpdatePhase(tt.phaseID(l), tt.props), tt.err)
			assert.Equal(t, before, l.Snapshot(), "a failed change leaves the phases alone")
		})
	}
}

func TestLifecycleRemovePhase(t *testing.T) {
	clock.Fix(t, domT0)
	envs := domEnvIDs(3)
	l := domNewLifecycle(t, envs[:1], envs[1:2], envs[2:])
	middle := l.Phases()[1]
	clock.Fix(t, domT1)

	require.NoError(t, l.RemovePhase(middle.ID))

	assert.Equal(t, []string{"Phase A", "Phase C"}, domPhaseNames(l))
	assert.Equal(t, 1, l.Phases()[1].Position, "later phases move up")
	assert.Equal(t, domT1, l.UpdatedAt())
	assert.NotContains(t, l.EnvironmentIDs(), envs[1])

	t.Run("its environments can join another phase", func(t *testing.T) {
		_, err := l.AddPhase(LifecyclePhaseProps{Name: "Again", EnvironmentIDs: envs[1:2]})
		assert.NoError(t, err)
	})

	t.Run("unknown phase", func(t *testing.T) {
		before := l.Snapshot()
		assert.ErrorIs(t, l.RemovePhase(middle.ID), ErrNotFound)
		assert.Equal(t, before, l.Snapshot())
	})
}

func TestLifecycleMovePhase(t *testing.T) {
	tests := []struct {
		name     string
		from     int
		position int
		want     []string
	}{
		{"first to last", 0, 2, []string{"Phase B", "Phase C", "Phase A"}},
		{"last to first", 2, 0, []string{"Phase C", "Phase A", "Phase B"}},
		{"middle to first", 1, 0, []string{"Phase B", "Phase A", "Phase C"}},
		{"first to middle", 0, 1, []string{"Phase B", "Phase A", "Phase C"}},
		{"in place", 1, 1, []string{"Phase A", "Phase B", "Phase C"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			envs := domEnvIDs(3)
			l := domNewLifecycle(t, envs[:1], envs[1:2], envs[2:])
			clock.Fix(t, domT1)

			require.NoError(t, l.MovePhase(l.Phases()[tt.from].ID, tt.position))

			assert.Equal(t, tt.want, domPhaseNames(l))
			for i, p := range l.Phases() {
				assert.Equal(t, i, p.Position)
			}
			assert.Equal(t, domT1, l.UpdatedAt())
		})
	}
}

func TestLifecycleMovePhaseIsRefused(t *testing.T) {
	tests := []struct {
		name     string
		phaseID  func(*Lifecycle) string
		position int
		err      error
	}{
		{"negative position", func(l *Lifecycle) string { return l.Phases()[0].ID }, -1, ErrInvalid},
		{"position past the end", func(l *Lifecycle) string { return l.Phases()[0].ID }, 2, ErrInvalid},
		{"unknown phase", func(*Lifecycle) string { return id.New("lfp") }, 0, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envs := domEnvIDs(2)
			l := domNewLifecycle(t, envs[:1], envs[1:])
			before := l.Snapshot()

			assert.ErrorIs(t, l.MovePhase(tt.phaseID(l), tt.position), tt.err)
			assert.Equal(t, before, l.Snapshot())
		})
	}
}

func TestLifecycleArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	l := domNewLifecycle(t)

	clock.Fix(t, domT1)
	l.Archive()
	assert.True(t, l.Archived())
	assert.Equal(t, domT1, *l.DeletedAt())
	assert.Equal(t, domT1, l.UpdatedAt())

	clock.Fix(t, domT2)
	l.Archive()
	assert.Equal(t, domT1, *l.DeletedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, l.UpdatedAt())

	l.Restore()
	assert.False(t, l.Archived())
	assert.Nil(t, l.DeletedAt())
	assert.Equal(t, domT2, l.UpdatedAt())

	clock.Fix(t, domT0)
	l.Restore()
	assert.Equal(t, domT2, l.UpdatedAt(), "restoring an active lifecycle does nothing")
}

func TestNewLifecycleRefusesRepeatedPhaseIDs(t *testing.T) {
	clock.Fix(t, domT0)
	phaseID := id.New(LifecyclePhase{}.Prefix())
	_, err := NewLifecycle(NewLifecycleProps{
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		DisplayName:    "Standard",
		Phases: []NewLifecyclePhaseProps{
			{ID: &phaseID, Name: "Dev", EnvironmentIDs: []string{id.New(Environment{}.Prefix())}},
			{ID: &phaseID, Name: "Prod", EnvironmentIDs: []string{id.New(Environment{}.Prefix())}},
		},
	}, nil)
	assert.ErrorIs(t, err, ErrInvalid)
}
