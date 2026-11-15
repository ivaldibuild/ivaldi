package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// Set in init, once delivery's own init has registered the ID prefixes.
var domVarProject, domVarApp, domEnvStaging, domEnvProd, domTargetA, domChannelHot string

func init() {
	domVarProject, domVarApp = id.New("prj"), id.New("app")
	domEnvStaging, domEnvProd = id.New("env"), id.New("env")
	domTargetA, domChannelHot = id.New("tgt"), id.New("chn")
}

func domProjectVariable(t *testing.T, name string, mutate func(*NewVariableProps)) *Variable {
	t.Helper()
	props := NewVariableProps{OrganizationID: "org_a", ProjectID: &domVarProject, Name: name, Kind: VariableKindText}
	if mutate != nil {
		mutate(&props)
	}
	v, err := NewVariable(props, nil)
	require.NoError(t, err)
	return v
}

func domApplicationVariable(t *testing.T, name string, mutate func(*NewVariableProps)) *Variable {
	t.Helper()
	return domProjectVariable(t, name, func(p *NewVariableProps) {
		p.ProjectID, p.ApplicationID = nil, &domVarApp
		if mutate != nil {
			mutate(p)
		}
	})
}

func scoped(envs, targets, channels []string) VariableScope {
	return VariableScope{EnvironmentIDs: envs, TargetIDs: targets, ChannelIDs: channels}
}

func TestNewVariable(t *testing.T) {
	valid := func() NewVariableProps {
		return NewVariableProps{OrganizationID: "org_a", ProjectID: &domVarProject, Name: "Database.Host", Kind: VariableKindText}
	}
	tests := []struct {
		name   string
		mutate func(*NewVariableProps)
		reason string
	}{
		{"no owner", func(p *NewVariableProps) { p.ProjectID = nil }, "a project or to an application"},
		{"two owners", func(p *NewVariableProps) { p.ApplicationID = &domVarApp }, "not both"},
		{"a space in the name", func(p *NewVariableProps) { p.Name = "Database Host" }, "name must be"},
		{"a system variable's name", func(p *NewVariableProps) { p.Name = "ivaldi.Release.Version" }, "reserved for system variables"},
		{"a secret", func(p *NewVariableProps) { p.Kind = VariableKindSecret }, "must be text"},
		{"a delivery on a text variable", func(p *NewVariableProps) { p.DeliverAs = new(VariableDeliverAsValue) }, "only a secret variable"},
		{"an empty key in the values path", func(p *NewVariableProps) { p.ValuesPath = new("database..host") }, "dot-separated keys"},
		{"a values path in Ivaldi's values", func(p *NewVariableProps) { p.ValuesPath = new("global.ivaldi.release") }, "reserved for Ivaldi"},
		{"an unclosed binding", func(p *NewVariableProps) { p.Values = []NewVariableValueProps{{Value: "#{Database.Name"}} }, "unclosed"},
		{"a project value scoped to a channel", func(p *NewVariableProps) {
			p.Values = []NewVariableValueProps{{Value: "x", Scope: scoped(nil, nil, []string{domChannelHot})}}
		}, "cannot be scoped to channels"},
		{"a scope naming something else", func(p *NewVariableProps) {
			p.Values = []NewVariableValueProps{{Value: "x", Scope: scoped([]string{domTargetA}, nil, nil)}}
		}, "not a environment ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := valid()
			tt.mutate(&props)
			_, err := NewVariable(props, nil)
			require.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), tt.reason)
		})
	}

	t.Run("sorts and deduplicates a scope", func(t *testing.T) {
		later, earlier := domEnvProd, domEnvStaging
		v := domProjectVariable(t, "Database.Host", func(p *NewVariableProps) {
			p.Values = []NewVariableValueProps{{Value: "db", Scope: scoped([]string{later, earlier, later}, nil, nil)}}
		})
		assert.Equal(t, scoped([]string{earlier, later}, []string{}, []string{}), v.Values()[0].Scope)
	})
}

// Two values tie when they scope the same dimensions and share a name in
// each, so some Deployment would match both equally.
func TestVariableRefusesValuesThatCouldTie(t *testing.T) {
	both := []string{domEnvStaging, domEnvProd}
	tests := []struct {
		name          string
		first, second VariableScope
		ties          string
	}{
		{"two unscoped values", scoped(nil, nil, nil), scoped(nil, nil, nil), "no scope"},
		{"overlapping environments", scoped(both, nil, nil), scoped([]string{domEnvProd}, nil, nil), "environment " + domEnvProd},
		{"overlapping in every dimension", scoped(both, nil, []string{domChannelHot}), scoped([]string{domEnvProd}, nil, []string{domChannelHot}), "channel " + domChannelHot},
		{"different environments", scoped([]string{domEnvStaging}, nil, nil), scoped([]string{domEnvProd}, nil, nil), ""},
		{"one is more specific", scoped(both, nil, nil), scoped(both, nil, []string{domChannelHot}), ""},
		{"different dimensions", scoped(both, nil, nil), scoped(nil, []string{domTargetA}, nil), ""},
		{"scoped and unscoped", scoped(nil, nil, nil), scoped(both, nil, nil), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := domApplicationVariable(t, "Log.Level", nil)
			first, err := v.AddValue(VariableValueProps{Value: "info", Scope: tt.first})
			require.NoError(t, err)
			_, err = v.AddValue(VariableValueProps{Value: "debug", Scope: tt.second})
			if tt.ties == "" {
				require.NoError(t, err)
				// Moving the first value onto the second's scope ties too.
				require.ErrorIs(t, v.UpdateValue(first.ID, VariableValueProps{Value: "info", Scope: tt.second}), ErrInvalid)
				return
			}
			require.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), tt.ties)
			assert.Len(t, v.Values(), 1)
		})
	}

	t.Run("a value does not tie with itself", func(t *testing.T) {
		v := domApplicationVariable(t, "Log.Level", nil)
		value, err := v.AddValue(VariableValueProps{Value: "info"})
		require.NoError(t, err)
		require.NoError(t, v.UpdateValue(value.ID, VariableValueProps{Value: "warn"}))
		assert.Equal(t, "warn", v.Values()[0].Value)
	})
}

func TestBindingNames(t *testing.T) {
	names, err := BindingNames("postgres://#{Database.Host}:5432/#{ Database.Name }?app=#{Database.Host}&literal=##{not}")
	require.NoError(t, err)
	assert.Equal(t, []string{"Database.Host", "Database.Name"}, names)

	_, err = BindingNames("#{}")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestCheckDeployable(t *testing.T) {
	path := func(p string) func(*NewVariableProps) {
		return func(props *NewVariableProps) { props.ValuesPath = &p }
	}
	t.Run("an application's variable may shadow its project's in value", func(t *testing.T) {
		project := []*Variable{domProjectVariable(t, "Database.Host", path("database.host"))}
		application := []*Variable{domApplicationVariable(t, "database.host", path("database.host"))}
		assert.NoError(t, CheckDeployable(project, application))
	})
	t.Run("but not in where the chart receives it", func(t *testing.T) {
		project := []*Variable{domProjectVariable(t, "Database.Host", path("database.host"))}
		application := []*Variable{domApplicationVariable(t, "Database.Host", path("db.host"))}
		err := CheckDeployable(project, application)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "shadows")
	})
	t.Run("two variables cannot write overlapping paths", func(t *testing.T) {
		project := []*Variable{domProjectVariable(t, "Database", path("db"))}
		for p, overlaps := range map[string]bool{"db": true, "db.host": true, "dbx": false} {
			err := CheckDeployable(project, []*Variable{domApplicationVariable(t, "Database.Host", path(p))})
			assert.Equal(t, overlaps, err != nil, p)
		}
	})
	t.Run("variables without a path never overlap", func(t *testing.T) {
		assert.NoError(t, CheckDeployable(
			[]*Variable{domProjectVariable(t, "A", nil), domProjectVariable(t, "B", nil)}, nil))
	})
}

func TestVariableEvents(t *testing.T) {
	build := func(t *testing.T) *Variable {
		return domApplicationVariable(t, "Log.Level", func(p *NewVariableProps) {
			p.Description = new("How much to log")
			p.Values = []NewVariableValueProps{{Value: "info"}}
		})
	}
	t.Run("created only when new", func(t *testing.T) {
		v := build(t)
		assert.Equal(t, []aggregate.Event{VariableCreated{v.Snapshot()}}, v.PullEvents())
		stored, err := NewVariable(NewVariableProps{OrganizationID: "org_a", ApplicationID: &domVarApp, Name: "Log.Level", Kind: VariableKindText}, new(id.New("var")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	value := func(v *Variable) string { return v.Values()[0].ID }
	var removed string
	checkEvents(t, build, []eventCase[*Variable]{
		{"update", func(t *testing.T, v *Variable) {
			require.NoError(t, v.Update(UpdateVariableProps{ValuesPath: new("log.level")}))
		}, func(v *Variable) aggregate.Event { return VariableUpdated{v.Snapshot()} }},
		{"update with the same values", func(t *testing.T, v *Variable) {
			require.NoError(t, v.Update(UpdateVariableProps{Description: new("How much to log"), ValuesPath: new("")}))
		}, nil},
		{"add a value", func(t *testing.T, v *Variable) {
			_, err := v.AddValue(VariableValueProps{Value: "debug", Scope: scoped([]string{domEnvStaging}, nil, nil)})
			require.NoError(t, err)
		}, func(v *Variable) aggregate.Event {
			return VariableValueAdded{Variable: v.Snapshot(), ValueID: v.Values()[1].ID}
		}},
		{"update a value", func(t *testing.T, v *Variable) {
			require.NoError(t, v.UpdateValue(value(v), VariableValueProps{Value: "warn"}))
		}, func(v *Variable) aggregate.Event {
			return VariableValueUpdated{Variable: v.Snapshot(), ValueID: value(v)}
		}},
		{"update a value with the same content", func(t *testing.T, v *Variable) {
			require.NoError(t, v.UpdateValue(value(v), VariableValueProps{Value: "info"}))
		}, nil},
		{"remove a value", func(t *testing.T, v *Variable) {
			removed = value(v)
			require.NoError(t, v.RemoveValue(removed))
		}, func(v *Variable) aggregate.Event {
			return VariableValueRemoved{Variable: v.Snapshot(), ValueID: removed}
		}},
		{"archive", func(_ *testing.T, v *Variable) { v.Archive() },
			func(v *Variable) aggregate.Event { return VariableArchived{v.Snapshot()} }},
	})

	t.Run("an archived variable does not change", func(t *testing.T) {
		v := build(t)
		v.Archive()
		_, err := v.AddValue(VariableValueProps{Value: "debug", Scope: scoped([]string{domEnvStaging}, nil, nil)})
		require.ErrorIs(t, err, ErrArchived)
	})
}
