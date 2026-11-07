package delivery

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// eventCase is a change to a domain object and the event it should record,
// built from the object after the change. A nil want means the change
// should record nothing.
type eventCase[T any] struct {
	name   string
	change func(t *testing.T, x T)
	want   func(x T) aggregate.Event
}

type eventRecorder interface {
	PullEvents() []aggregate.Event
}

// checkEvents runs each case on a fresh object whose creation event has
// already been pulled.
func checkEvents[T eventRecorder](t *testing.T, build func(*testing.T) T, cases []eventCase[T]) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := build(t)
			x.PullEvents()

			c.change(t, x)

			got := x.PullEvents()
			if c.want == nil {
				assert.Empty(t, got, "nothing changed")
				return
			}
			assert.Equal(t, []aggregate.Event{c.want(x)}, got)
		})
	}
}

func TestNewRecordsCreatedOnlyForNewObjects(t *testing.T) {
	t.Run("environment", func(t *testing.T) {
		e := domNewEnvironment(t)
		assert.Equal(t, []aggregate.Event{EnvironmentCreated{e.Snapshot()}}, e.PullEvents())
		stored, err := NewEnvironment(domEnvironmentProps(), new(id.New("env")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("target", func(t *testing.T) {
		tgt := domNewTarget(t)
		assert.Equal(t, []aggregate.Event{TargetCreated{tgt.Snapshot()}}, tgt.PullEvents())
		stored, err := NewTarget(domTargetProps(), new(id.New("tgt")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("lifecycle", func(t *testing.T) {
		l := domNewLifecycle(t, domEnvIDs(1))
		assert.Equal(t, []aggregate.Event{LifecycleCreated{l.Snapshot()}}, l.PullEvents(), "one event, with its phases")
		stored, err := NewLifecycle(domLifecycleProps(), new(id.New("lfc")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("registry", func(t *testing.T) {
		r := domNewRegistry(t)
		assert.Equal(t, []aggregate.Event{RegistryCreated{r.Snapshot()}}, r.PullEvents())
		stored, err := NewRegistry(domRegistryProps(), new(id.New("reg")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("artifact", func(t *testing.T) {
		a := domNewArtifact(t)
		assert.Equal(t, []aggregate.Event{ArtifactCreated{a.Snapshot()}}, a.PullEvents())
		stored, err := NewArtifact(domArtifactProps(), new(id.New("art")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("channel", func(t *testing.T) {
		c := domNewChannel(t, NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"range": "1.0"}`)})
		assert.Equal(t, []aggregate.Event{ChannelCreated{c.Snapshot()}}, c.PullEvents(), "one event, with its rules")
		stored, err := NewChannel(domChannelProps(), new(id.New("chn")))
		require.NoError(t, err)
		assert.Empty(t, stored.PullEvents())
	})
	t.Run("placement", func(t *testing.T) {
		p := domNewPlacement(t, nil)
		assert.Equal(t, []aggregate.Event{PlacementCreated{p.Snapshot()}}, p.PullEvents())
		stored := domNewPlacement(t, func(props *NewPlacementProps) { props.CreatedAt = new(domT0) })
		assert.Empty(t, stored.PullEvents(), "a placement with a creation time is stored")
	})
}

func TestEnvironmentEvents(t *testing.T) {
	checkEvents(t, domNewEnvironment, []eventCase[*Environment]{
		{"update", func(t *testing.T, e *Environment) {
			require.NoError(t, e.Update(UpdateEnvironmentProps{DisplayName: new("Pre-production"), ExecutionEnabled: new(true)}))
		}, func(e *Environment) aggregate.Event { return EnvironmentUpdated{e.Snapshot()} }},
		{"update with the same values", func(t *testing.T, e *Environment) {
			require.NoError(t, e.Update(UpdateEnvironmentProps{Slug: new("staging"), Tier: new(TierStaging), ExecutionEnabled: new(false)}))
		}, nil},
		{"archive", func(t *testing.T, e *Environment) { e.Archive() },
			func(e *Environment) aggregate.Event { return EnvironmentArchived{e.Snapshot()} }},
		{"archive twice", func(t *testing.T, e *Environment) { e.Archive(); e.PullEvents(); e.Archive() }, nil},
		{"restore", func(t *testing.T, e *Environment) { e.Archive(); e.PullEvents(); e.Restore() },
			func(e *Environment) aggregate.Event { return EnvironmentRestored{e.Snapshot()} }},
		{"restore an active one", func(t *testing.T, e *Environment) { e.Restore() }, nil},
	})
}

func TestTargetEvents(t *testing.T) {
	checkEvents(t, domNewTarget, []eventCase[*Target]{
		{"update", func(t *testing.T, tgt *Target) {
			require.NoError(t, tgt.Update(UpdateTargetProps{Locator: map[string]string{LocatorCluster: "prod-eu-2", LocatorNamespace: "payments"}}))
		}, func(tgt *Target) aggregate.Event { return TargetUpdated{tgt.Snapshot()} }},
		{"update with the same values", func(t *testing.T, tgt *Target) {
			require.NoError(t, tgt.Update(UpdateTargetProps{
				DisplayName: new("EU West"),
				Locator:     map[string]string{LocatorCluster: "prod-eu-1", LocatorNamespace: "payments"},
			}))
		}, nil},
		{"assign an agent", func(t *testing.T, tgt *Target) { require.NoError(t, tgt.AssignAgent(domAgent)) },
			func(tgt *Target) aggregate.Event { return TargetAgentAssigned{Target: tgt.Snapshot()} }},
		{"replace the agent", func(t *testing.T, tgt *Target) {
			require.NoError(t, tgt.AssignAgent(domAgent))
			tgt.PullEvents()
			require.NoError(t, tgt.AssignAgent(domOtherAgent))
		}, func(tgt *Target) aggregate.Event {
			return TargetAgentAssigned{Target: tgt.Snapshot(), PreviousAgentPrincipalID: new(domAgent)}
		}},
		{"assign the same agent", func(t *testing.T, tgt *Target) {
			require.NoError(t, tgt.AssignAgent(domAgent))
			tgt.PullEvents()
			require.NoError(t, tgt.AssignAgent(domAgent))
		}, nil},
		{"unassign the agent", func(t *testing.T, tgt *Target) {
			require.NoError(t, tgt.AssignAgent(domAgent))
			tgt.PullEvents()
			require.NoError(t, tgt.UnassignAgent())
		}, func(tgt *Target) aggregate.Event {
			return TargetAgentUnassigned{Target: tgt.Snapshot(), PreviousAgentPrincipalID: domAgent}
		}},
		{"unassign without an agent", func(t *testing.T, tgt *Target) { require.NoError(t, tgt.UnassignAgent()) }, nil},
		{"agent report", func(t *testing.T, tgt *Target) {
			require.NoError(t, tgt.AssignAgent(domAgent))
			tgt.PullEvents()
			require.NoError(t, tgt.RecordReport(domAgent))
		}, nil},
		{"archive", func(t *testing.T, tgt *Target) { tgt.Archive() },
			func(tgt *Target) aggregate.Event { return TargetArchived{tgt.Snapshot()} }},
		{"restore", func(t *testing.T, tgt *Target) { tgt.Archive(); tgt.PullEvents(); tgt.Restore() },
			func(tgt *Target) aggregate.Event { return TargetRestored{tgt.Snapshot()} }},
	})
}

func TestLifecycleEvents(t *testing.T) {
	build := func(t *testing.T) *Lifecycle { return domNewLifecycle(t, domEnvIDs(1), domEnvIDs(1)) }
	phase := func(l *Lifecycle, i int) LifecyclePhaseSnapshot { return l.Phases()[i] }
	var removed string

	checkEvents(t, build, []eventCase[*Lifecycle]{
		{"update", func(t *testing.T, l *Lifecycle) {
			require.NoError(t, l.Update(UpdateLifecycleProps{Description: new("")}))
		}, func(l *Lifecycle) aggregate.Event { return LifecycleUpdated{l.Snapshot()} }},
		{"update with the same values", func(t *testing.T, l *Lifecycle) {
			require.NoError(t, l.Update(UpdateLifecycleProps{Slug: new("mainline"), Description: new("The usual path")}))
		}, nil},
		{"add a phase", func(t *testing.T, l *Lifecycle) {
			_, err := l.AddPhase(LifecyclePhaseProps{Name: "Phase C", EnvironmentIDs: domEnvIDs(1)})
			require.NoError(t, err)
		}, func(l *Lifecycle) aggregate.Event {
			return LifecyclePhaseAdded{Lifecycle: l.Snapshot(), PhaseID: phase(l, 2).ID}
		}},
		{"update a phase", func(t *testing.T, l *Lifecycle) {
			p := phase(l, 0)
			require.NoError(t, l.UpdatePhase(p.ID, LifecyclePhaseProps{Name: "First", EnvironmentIDs: p.EnvironmentIDs}))
		}, func(l *Lifecycle) aggregate.Event {
			return LifecyclePhaseUpdated{Lifecycle: l.Snapshot(), PhaseID: phase(l, 0).ID}
		}},
		{"update a phase with the same values", func(t *testing.T, l *Lifecycle) {
			p := phase(l, 0)
			require.NoError(t, l.UpdatePhase(p.ID, LifecyclePhaseProps{
				Name: p.Name, Optional: p.Optional, MinEnvironments: p.MinEnvironments, EnvironmentIDs: p.EnvironmentIDs,
			}))
		}, nil},
		{"remove a phase", func(t *testing.T, l *Lifecycle) {
			removed = phase(l, 1).ID
			require.NoError(t, l.RemovePhase(removed))
		}, func(l *Lifecycle) aggregate.Event {
			return LifecyclePhaseRemoved{Lifecycle: l.Snapshot(), PhaseID: removed}
		}},
		{"move a phase", func(t *testing.T, l *Lifecycle) {
			require.NoError(t, l.MovePhase(phase(l, 1).ID, 0))
		}, func(l *Lifecycle) aggregate.Event {
			return LifecyclePhaseMoved{Lifecycle: l.Snapshot(), PhaseID: phase(l, 0).ID}
		}},
		{"move a phase to where it is", func(t *testing.T, l *Lifecycle) {
			require.NoError(t, l.MovePhase(phase(l, 1).ID, 1))
		}, nil},
		{"archive", func(t *testing.T, l *Lifecycle) { l.Archive() },
			func(l *Lifecycle) aggregate.Event { return LifecycleArchived{l.Snapshot()} }},
		{"restore", func(t *testing.T, l *Lifecycle) { l.Archive(); l.PullEvents(); l.Restore() },
			func(l *Lifecycle) aggregate.Event { return LifecycleRestored{l.Snapshot()} }},
	})
}

func TestRegistryEvents(t *testing.T) {
	checkEvents(t, domNewRegistry, []eventCase[*Registry]{
		{"update", func(t *testing.T, r *Registry) {
			require.NoError(t, r.Update(UpdateRegistryProps{URL: new("https://ghcr.io/acme-2")}))
		}, func(r *Registry) aggregate.Event { return RegistryUpdated{r.Snapshot()} }},
		{"update with the same values", func(t *testing.T, r *Registry) {
			require.NoError(t, r.Update(UpdateRegistryProps{Slug: new("ghcr"), URL: new("https://ghcr.io/acme")}))
		}, nil},
		{"set a credential", func(t *testing.T, r *Registry) { require.NoError(t, r.SetCredential("cred_1")) },
			func(r *Registry) aggregate.Event { return RegistryCredentialSet{r.Snapshot()} }},
		{"set the same credential", func(t *testing.T, r *Registry) {
			require.NoError(t, r.SetCredential("cred_1"))
			r.PullEvents()
			require.NoError(t, r.SetCredential("cred_1"))
		}, nil},
		{"clear the credential", func(t *testing.T, r *Registry) {
			require.NoError(t, r.SetCredential("cred_1"))
			r.PullEvents()
			require.NoError(t, r.ClearCredential())
		}, func(r *Registry) aggregate.Event { return RegistryCredentialCleared{r.Snapshot()} }},
		{"clear without a credential", func(t *testing.T, r *Registry) { require.NoError(t, r.ClearCredential()) }, nil},
		{"change the lookup", func(t *testing.T, r *Registry) {
			require.NoError(t, r.SetLookup(LookupFromDataPlane, "eu-pool"))
		}, func(r *Registry) aggregate.Event { return RegistryLookupChanged{r.Snapshot()} }},
		{"keep the lookup", func(t *testing.T, r *Registry) {
			require.NoError(t, r.SetLookup(LookupFromControlPlane, ""))
		}, nil},
		{"archive", func(t *testing.T, r *Registry) { r.Archive() },
			func(r *Registry) aggregate.Event { return RegistryArchived{r.Snapshot()} }},
		{"restore", func(t *testing.T, r *Registry) { r.Archive(); r.PullEvents(); r.Restore() },
			func(r *Registry) aggregate.Event { return RegistryRestored{r.Snapshot()} }},
	})
}

func TestArtifactEvents(t *testing.T) {
	registryID := id.New("reg")
	observed := func(t *testing.T) *Artifact {
		props := domArtifactProps()
		props.Source = ArtifactObserved
		a, err := NewArtifact(props, nil)
		require.NoError(t, err)
		return a
	}

	checkEvents(t, domNewArtifact, []eventCase[*Artifact]{
		{"update", func(t *testing.T, a *Artifact) {
			require.NoError(t, a.Update(UpdateArtifactProps{Required: new(false)}))
		}, func(a *Artifact) aggregate.Event { return ArtifactUpdated{a.Snapshot()} }},
		{"update with the same values", func(t *testing.T, a *Artifact) {
			require.NoError(t, a.Update(UpdateArtifactProps{Name: new("api"), Required: new(true)}))
		}, nil},
		{"use a registry", func(t *testing.T, a *Artifact) { require.NoError(t, a.UseRegistry(registryID)) },
			func(a *Artifact) aggregate.Event { return ArtifactRegistrySet{a.Snapshot()} }},
		{"use the same registry", func(t *testing.T, a *Artifact) {
			require.NoError(t, a.UseRegistry(registryID))
			a.PullEvents()
			require.NoError(t, a.UseRegistry(registryID))
		}, nil},
		{"clear the registry", func(t *testing.T, a *Artifact) {
			require.NoError(t, a.UseRegistry(registryID))
			a.PullEvents()
			require.NoError(t, a.ClearRegistry())
		}, func(a *Artifact) aggregate.Event { return ArtifactRegistryCleared{a.Snapshot()} }},
		{"clear without a registry", func(t *testing.T, a *Artifact) { require.NoError(t, a.ClearRegistry()) }, nil},
		{"declare a declared artifact", func(t *testing.T, a *Artifact) { require.NoError(t, a.Declare()) }, nil},
		{"archive", func(t *testing.T, a *Artifact) { a.Archive() },
			func(a *Artifact) aggregate.Event { return ArtifactArchived{a.Snapshot()} }},
		{"restore", func(t *testing.T, a *Artifact) { a.Archive(); a.PullEvents(); a.Restore() },
			func(a *Artifact) aggregate.Event { return ArtifactRestored{a.Snapshot()} }},
	})
	checkEvents(t, observed, []eventCase[*Artifact]{
		{"confirm an observed artifact", func(t *testing.T, a *Artifact) { require.NoError(t, a.Declare()) },
			func(a *Artifact) aggregate.Event { return ArtifactConfirmed{a.Snapshot()} }},
	})
}

func TestChannelEvents(t *testing.T) {
	build := func(t *testing.T) *Channel {
		return domNewChannel(t, NewChannelRuleProps{Kind: release.RuleVersion, Config: json.RawMessage(`{"range": "1.0"}`)})
	}
	rule := func(c *Channel, i int) string { return c.Rules()[i].ID }
	var previousLifecycle, removed string

	checkEvents(t, build, []eventCase[*Channel]{
		{"update", func(t *testing.T, c *Channel) {
			require.NoError(t, c.Update(UpdateChannelProps{DisplayName: new("Main line")}))
		}, func(c *Channel) aggregate.Event { return ChannelUpdated{c.Snapshot()} }},
		{"update with the same values", func(t *testing.T, c *Channel) {
			require.NoError(t, c.Update(UpdateChannelProps{Slug: new("mainline"), Description: new("Regular releases")}))
		}, nil},
		{"use another lifecycle", func(t *testing.T, c *Channel) {
			previousLifecycle = c.LifecycleID()
			require.NoError(t, c.UseLifecycle(id.New("lfc")))
		}, func(c *Channel) aggregate.Event {
			return ChannelLifecycleChanged{Channel: c.Snapshot(), PreviousLifecycleID: previousLifecycle}
		}},
		{"use the same lifecycle", func(t *testing.T, c *Channel) { require.NoError(t, c.UseLifecycle(c.LifecycleID())) }, nil},
		{"add a rule", func(t *testing.T, c *Channel) {
			_, err := c.AddRule(domVersionRule(`{"range": "2.0"}`))
			require.NoError(t, err)
		}, func(c *Channel) aggregate.Event { return ChannelRuleAdded{Channel: c.Snapshot(), RuleID: rule(c, 1)} }},
		{"update a rule", func(t *testing.T, c *Channel) {
			require.NoError(t, c.UpdateRule(rule(c, 0), domVersionRule(`{"range": "2.0"}`)))
		}, func(c *Channel) aggregate.Event { return ChannelRuleUpdated{Channel: c.Snapshot(), RuleID: rule(c, 0)} }},
		{"update a rule with the same values", func(t *testing.T, c *Channel) {
			require.NoError(t, c.UpdateRule(rule(c, 0), domVersionRule(`{ "range": "1.0" }`)))
		}, nil},
		{"remove a rule", func(t *testing.T, c *Channel) {
			removed = rule(c, 0)
			require.NoError(t, c.RemoveRule(removed))
		}, func(c *Channel) aggregate.Event { return ChannelRuleRemoved{Channel: c.Snapshot(), RuleID: removed} }},
		{"make default", func(t *testing.T, c *Channel) { require.NoError(t, c.MakeDefault()) },
			func(c *Channel) aggregate.Event { return ChannelMadeDefault{c.Snapshot()} }},
		{"make default twice", func(t *testing.T, c *Channel) {
			require.NoError(t, c.MakeDefault())
			c.PullEvents()
			require.NoError(t, c.MakeDefault())
		}, nil},
		{"clear default", func(t *testing.T, c *Channel) {
			require.NoError(t, c.MakeDefault())
			c.PullEvents()
			c.ClearDefault()
		}, func(c *Channel) aggregate.Event { return ChannelDefaultCleared{c.Snapshot()} }},
		{"clear default when not the default", func(t *testing.T, c *Channel) { c.ClearDefault() }, nil},
		{"archive", func(t *testing.T, c *Channel) { require.NoError(t, c.Archive()) },
			func(c *Channel) aggregate.Event { return ChannelArchived{c.Snapshot()} }},
		{"restore", func(t *testing.T, c *Channel) {
			require.NoError(t, c.Archive())
			c.PullEvents()
			c.Restore()
		}, func(c *Channel) aggregate.Event { return ChannelRestored{c.Snapshot()} }},
	})
}

func TestPlacementEvents(t *testing.T) {
	build := func(t *testing.T) *Placement { return domNewPlacement(t, nil) }
	config := ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"values/eu"}}

	checkEvents(t, build, []eventCase[*Placement]{
		{"configure", func(t *testing.T, p *Placement) { require.NoError(t, p.Configure(config)) },
			func(p *Placement) aggregate.Event { return PlacementApplyConfigSet{p.Snapshot()} }},
		{"configure the same way", func(t *testing.T, p *Placement) {
			require.NoError(t, p.Configure(config))
			p.PullEvents()
			require.NoError(t, p.Configure(ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"values/eu"}}))
		}, nil},
		{"clear the apply config", func(t *testing.T, p *Placement) {
			require.NoError(t, p.Configure(config))
			p.PullEvents()
			p.ClearApplyConfig()
		}, func(p *Placement) aggregate.Event { return PlacementApplyConfigCleared{p.Snapshot()} }},
		{"clear without an apply config", func(t *testing.T, p *Placement) { p.ClearApplyConfig() }, nil},
		{"remove", func(t *testing.T, p *Placement) { p.Remove() },
			func(p *Placement) aggregate.Event { return PlacementRemoved{p.Snapshot()} }},
	})
	checkEvents(t, func(t *testing.T) *Placement {
		return domNewPlacement(t, func(props *NewPlacementProps) {
			props.Source = PlacementObserved
			props.ApplyConfig = &config
		})
	}, []eventCase[*Placement]{
		{"configure an observed placement as it is", func(t *testing.T, p *Placement) { require.NoError(t, p.Configure(config)) },
			func(p *Placement) aggregate.Event { return PlacementApplyConfigSet{p.Snapshot()} }},
	})
}

// The snapshots' JSON keys are the events' contract: a change here is a
// change for every consumer.
func TestSnapshotJSONKeys(t *testing.T) {
	timestamps := []string{"created_at", "updated_at"}
	tests := []struct {
		snapshot any
		keys     []string
	}{
		{EnvironmentSnapshot{}, append([]string{"id", "organization_id", "workspace_id", "slug", "display_name", "tier", "execution_enabled", "deleted_at"}, timestamps...)},
		{TargetSnapshot{}, append([]string{"id", "organization_id", "environment_id", "slug", "display_name", "runtime", "locator", "agent_principal_id", "last_reported_at", "deleted_at"}, timestamps...)},
		{LifecycleSnapshot{}, append([]string{"id", "organization_id", "workspace_id", "slug", "display_name", "description", "phases", "deleted_at"}, timestamps...)},
		{LifecyclePhaseSnapshot{}, append([]string{"id", "position", "name", "optional", "min_environments", "environment_ids"}, timestamps...)},
		{RegistrySnapshot{}, append([]string{"id", "organization_id", "workspace_id", "slug", "display_name", "kind", "url", "credential_ref", "lookup_from", "lookup_pool", "deleted_at"}, timestamps...)},
		{ArtifactSnapshot{}, append([]string{"id", "organization_id", "application_id", "name", "artifact_kind", "artifact_name", "registry_id", "required", "source", "archived_at"}, timestamps...)},
		{ChannelSnapshot{}, append([]string{"id", "organization_id", "application_id", "lifecycle_id", "slug", "display_name", "description", "is_default", "rules", "archived_at"}, timestamps...)},
		{ChannelRuleSnapshot{}, append([]string{"id", "kind", "artifact_id", "config"}, timestamps...)},
		{PlacementSnapshot{}, append([]string{"organization_id", "application_id", "target_id", "source", "apply_config", "current_deployment_id", "current_release_id"}, timestamps...)},
		{DeploymentSnapshot{}, []string{"id", "organization_id", "application_id", "release_id", "environment_id", "target_id", "source", "reason", "status", "previous_deployment_id", "initiated_by_principal_id", "session_id", "work_item_id", "external_ref", "policy_decision_id", "findings", "requested_at", "started_at", "finished_at", "succeeded_at", "status_changed_at", "created_at", "updated_at"}},
		{DeploymentEventSnapshot{}, []string{"id", "organization_id", "deployment_id", "kind", "status", "detail", "report_id", "reported_by_principal_id", "occurred_at", "recorded_at"}},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.snapshot)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &fields))

		assert.ElementsMatch(t, tt.keys, slices.Collect(maps.Keys(fields)), "%T", tt.snapshot)
	}
}
