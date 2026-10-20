package delivery

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func domPlacementProps() NewPlacementProps {
	return NewPlacementProps{
		OrganizationID: domOrg,
		ApplicationID:  id.New("app"),
		TargetID:       id.New("tgt"),
	}
}

func domNewPlacement(t *testing.T, mutate func(*NewPlacementProps)) *Placement {
	t.Helper()
	props := domPlacementProps()
	if mutate != nil {
		mutate(&props)
	}
	p, err := NewPlacement(props)
	require.NoError(t, err)
	return p
}

func TestNewPlacement(t *testing.T) {
	clock.Fix(t, domT0)
	props := domPlacementProps()

	p, err := NewPlacement(props)

	require.NoError(t, err)
	assert.Equal(t, domOrg, p.OrganizationID())
	assert.Equal(t, props.ApplicationID, p.ApplicationID())
	assert.Equal(t, props.TargetID, p.TargetID())
	assert.Equal(t, PlacementConfigured, p.Source(), "the source defaults to configured")
	assert.Nil(t, p.ApplyConfig())
	assert.Nil(t, p.CurrentDeploymentID())
	assert.Nil(t, p.CurrentReleaseID())
	assert.Equal(t, domT0, p.CreatedAt())
	assert.Equal(t, domT0, p.UpdatedAt())
}

func TestNewPlacementWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	props := domPlacementProps()
	props.Source = PlacementObserved
	props.ApplyConfig = &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_values"}}
	props.CurrentDeploymentID = new("dep_1")
	props.CurrentReleaseID = new("rel_1")
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)

	p, err := NewPlacement(props)

	require.NoError(t, err)
	assert.Equal(t, PlacementObserved, p.Source())
	assert.Equal(t, &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_values"}}, p.ApplyConfig())
	assert.Equal(t, "dep_1", *p.CurrentDeploymentID())
	assert.Equal(t, "rel_1", *p.CurrentReleaseID())
	assert.Equal(t, domT0, p.CreatedAt())
	assert.Equal(t, domT1, p.UpdatedAt())

	t.Run("empty current IDs are none", func(t *testing.T) {
		p := domNewPlacement(t, func(p *NewPlacementProps) {
			p.CurrentDeploymentID = new("")
			p.CurrentReleaseID = new("")
		})
		assert.Nil(t, p.CurrentDeploymentID())
		assert.Nil(t, p.CurrentReleaseID())
	})
}

func TestNewPlacementIsRefused(t *testing.T) {
	tooMany := make([]string, maxValuesRefs+1)
	for i := range tooMany {
		tooMany[i] = "ref"
	}
	tests := []struct {
		name   string
		mutate func(*NewPlacementProps)
	}{
		{"no organization", func(p *NewPlacementProps) { p.OrganizationID = "" }},
		{"no application", func(p *NewPlacementProps) { p.ApplicationID = "" }},
		{"application ID with wrong prefix", func(p *NewPlacementProps) { p.ApplicationID = id.New("tgt") }},
		{"no target", func(p *NewPlacementProps) { p.TargetID = "" }},
		{"target ID with wrong prefix", func(p *NewPlacementProps) { p.TargetID = id.New("env") }},
		{"unknown source", func(p *NewPlacementProps) { p.Source = "imported" }},
		{"apply config without a definition artifact", func(p *NewPlacementProps) { p.ApplyConfig = &ApplyConfig{} }},
		{"apply config with a bad definition artifact", func(p *NewPlacementProps) { p.ApplyConfig = &ApplyConfig{DefinitionArtifact: "Helm Chart"} }},
		{"apply config with too many values refs", func(p *NewPlacementProps) {
			p.ApplyConfig = &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: tooMany}
		}},
		{"apply config with an empty values ref", func(p *NewPlacementProps) {
			p.ApplyConfig = &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{""}}
		}},
		{"apply config with a values ref too long", func(p *NewPlacementProps) {
			p.ApplyConfig = &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{strings.Repeat("r", maxValuesRefLength+1)}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domPlacementProps()
			tt.mutate(&props)
			_, err := NewPlacement(props)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}

	t.Run("values refs at their limits", func(t *testing.T) {
		refs := make([]string, maxValuesRefs)
		for i := range refs {
			refs[i] = strings.Repeat("r", maxValuesRefLength)
		}
		_, err := NewPlacement(NewPlacementProps{
			OrganizationID: domOrg, ApplicationID: id.New("app"), TargetID: id.New("tgt"),
			ApplyConfig: &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: refs},
		})
		assert.NoError(t, err)
	})
}

func TestPlacementGettersReturnCopies(t *testing.T) {
	config := &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_values"}}
	props := domPlacementProps()
	props.ApplyConfig = config
	props.CurrentDeploymentID = new("dep_1")
	props.CurrentReleaseID = new("rel_1")
	p, err := NewPlacement(props)
	require.NoError(t, err)
	before := p.Snapshot()

	config.DefinitionArtifact = "changed"
	config.ValuesRefs[0] = "changed"
	*props.CurrentDeploymentID = "changed"
	got := p.ApplyConfig()
	got.DefinitionArtifact = "changed"
	got.ValuesRefs[0] = "changed"
	*p.CurrentDeploymentID() = "changed"
	*p.CurrentReleaseID() = "changed"
	snap := p.Snapshot()
	snap.ApplyConfig.ValuesRefs[0] = "changed"
	*snap.CurrentReleaseID = "changed"

	assert.Equal(t, before, p.Snapshot())
	assert.Equal(t, "cred_values", p.ApplyConfig().ValuesRefs[0])
}

func TestPlacementSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	props := domPlacementProps()
	props.ApplyConfig = &ApplyConfig{DefinitionArtifact: "chart"}
	p, err := NewPlacement(props)
	require.NoError(t, err)

	assert.Equal(t, PlacementSnapshot{
		OrganizationID: domOrg,
		ApplicationID:  props.ApplicationID,
		TargetID:       props.TargetID,
		Source:         PlacementConfigured,
		ApplyConfig:    &ApplyConfig{DefinitionArtifact: "chart"},
		CreatedAt:      domT0,
		UpdatedAt:      domT0,
	}, p.Snapshot())
}

func TestPlacementConfigure(t *testing.T) {
	clock.Fix(t, domT0)
	p := domNewPlacement(t, func(p *NewPlacementProps) { p.Source = PlacementObserved })

	clock.Fix(t, domT1)
	config := ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_eu"}}
	require.NoError(t, p.Configure(config))

	assert.Equal(t, &ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"cred_eu"}}, p.ApplyConfig())
	assert.Equal(t, PlacementConfigured, p.Source(), "configuring an observed placement makes it configured")
	assert.Equal(t, domT1, p.UpdatedAt())
	assert.Equal(t, domT0, p.CreatedAt())

	config.ValuesRefs[0] = "changed"
	assert.Equal(t, "cred_eu", p.ApplyConfig().ValuesRefs[0], "the given config is copied")

	t.Run("a new config replaces the old one", func(t *testing.T) {
		require.NoError(t, p.Configure(ApplyConfig{DefinitionArtifact: "manifests"}))
		assert.Equal(t, &ApplyConfig{DefinitionArtifact: "manifests"}, p.ApplyConfig())
	})

	tests := []struct {
		name   string
		config ApplyConfig
	}{
		{"no definition artifact", ApplyConfig{}},
		{"definition artifact not a name", ApplyConfig{DefinitionArtifact: "ghcr.io/acme/chart"}},
		{"empty values ref", ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: []string{"ok", ""}}},
		{"too many values refs", ApplyConfig{DefinitionArtifact: "chart", ValuesRefs: make([]string, maxValuesRefs+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed := domNewPlacement(t, func(p *NewPlacementProps) { p.Source = PlacementObserved })
			before := observed.Snapshot()
			clock.Fix(t, domT2)

			assert.ErrorIs(t, observed.Configure(tt.config), ErrInvalid)
			assert.Equal(t, before, observed.Snapshot(), "a failed configure changes nothing, including the source")
		})
	}
}

func TestPlacementClearApplyConfig(t *testing.T) {
	clock.Fix(t, domT0)
	p := domNewPlacement(t, func(p *NewPlacementProps) { p.ApplyConfig = &ApplyConfig{DefinitionArtifact: "chart"} })

	clock.Fix(t, domT1)
	p.ClearApplyConfig()
	assert.Nil(t, p.ApplyConfig())
	assert.Nil(t, p.Snapshot().ApplyConfig)
	assert.Equal(t, PlacementConfigured, p.Source(), "the source is kept")
	assert.Equal(t, domT1, p.UpdatedAt())

	clock.Fix(t, domT2)
	p.ClearApplyConfig()
	assert.Equal(t, domT1, p.UpdatedAt(), "clearing an absent config does nothing")
}
