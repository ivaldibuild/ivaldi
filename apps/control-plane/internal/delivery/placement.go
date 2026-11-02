package delivery

import (
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// PlacementSource says whether someone placed an Application on a Target or
// the runtime agent found it there first.
type PlacementSource string

const (
	PlacementConfigured PlacementSource = "configured"
	PlacementObserved   PlacementSource = "observed"
)

// Placement records that an Application runs on a Target. It has no ID of
// its own: the Application and Target identify it. Which Deployment and
// Release are current there is set by Deployments, not by this capability's
// configuration methods.
type Placement struct {
	aggregate.Events
	organizationID      string
	applicationID       string
	targetID            string
	source              PlacementSource
	applyConfig         *ApplyConfig
	currentDeploymentID *string
	currentReleaseID    *string
	createdAt           time.Time
	updatedAt           time.Time
}

// ApplyConfig says how Ivaldi applies a Release to the Target when it
// executes a Deployment. It is not used for observed Deployments.
type ApplyConfig struct {
	// DefinitionArtifact is the name of the Release's artifact that holds
	// the deployment definition, such as "chart".
	DefinitionArtifact string `json:"definition_artifact"`
	// ValuesRefs refer to Target-specific values, such as Helm values held
	// by the credentials capability. They are references, never values.
	ValuesRefs []string `json:"values_refs,omitempty"`
}

func (c ApplyConfig) clone() *ApplyConfig {
	c.ValuesRefs = copyStrings(c.ValuesRefs)
	return &c
}

func (c *ApplyConfig) equal(o *ApplyConfig) bool {
	if c == nil || o == nil {
		return c == o
	}
	return c.DefinitionArtifact == o.DefinitionArtifact && slices.Equal(c.ValuesRefs, o.ValuesRefs)
}

func (c ApplyConfig) validate() error {
	if err := validateArtifactName(c.DefinitionArtifact); err != nil {
		return NewErrInvalidInput("apply config needs the name of the artifact that holds the deployment definition")
	}
	if len(c.ValuesRefs) > maxValuesRefs {
		return NewErrInvalidInput(fmt.Sprintf("apply config can have at most %d values references", maxValuesRefs))
	}
	for _, ref := range c.ValuesRefs {
		if ref == "" || len(ref) > maxValuesRefLength {
			return NewErrInvalidInput(fmt.Sprintf("values references must be 1-%d characters", maxValuesRefLength))
		}
	}
	return nil
}

// PlacementSnapshot is a Placement's state, for repositories, audit records, and
// events. Its JSON form is part of the Placement events' contract.
type PlacementSnapshot struct {
	OrganizationID      string          `json:"organization_id"`
	ApplicationID       string          `json:"application_id"`
	TargetID            string          `json:"target_id"`
	Source              PlacementSource `json:"source"`
	ApplyConfig         *ApplyConfig    `json:"apply_config"`
	CurrentDeploymentID *string         `json:"current_deployment_id"`
	CurrentReleaseID    *string         `json:"current_release_id"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// NewPlacementProps describes a Placement. A new one leaves the current
// Deployment, Release, and timestamps nil; a repository loading a stored one sets them.
type NewPlacementProps struct {
	OrganizationID string
	ApplicationID  string
	TargetID       string
	// Source defaults to PlacementConfigured.
	Source              PlacementSource
	ApplyConfig         *ApplyConfig
	CurrentDeploymentID *string
	CurrentReleaseID    *string
	CreatedAt           *time.Time
	UpdatedAt           *time.Time
}

// NewPlacement returns a Placement.
func NewPlacement(props NewPlacementProps) (*Placement, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Application{}.Prefix(), props.ApplicationID) {
		return nil, NewErrInvalidInput("a placement needs an application ID")
	}
	if !id.Valid(Target{}.Prefix(), props.TargetID) {
		return nil, NewErrInvalidInput("a placement needs a target ID")
	}
	if props.Source == "" {
		props.Source = PlacementConfigured
	}
	if props.Source != PlacementConfigured && props.Source != PlacementObserved {
		return nil, NewErrInvalidInput(fmt.Sprintf("placement source %q must be configured or observed", props.Source))
	}
	p := &Placement{
		organizationID:      props.OrganizationID,
		applicationID:       props.ApplicationID,
		targetID:            props.TargetID,
		source:              props.Source,
		currentDeploymentID: optionalString(props.CurrentDeploymentID),
		currentReleaseID:    optionalString(props.CurrentReleaseID),
	}
	if props.ApplyConfig != nil {
		if err := props.ApplyConfig.validate(); err != nil {
			return nil, err
		}
		p.applyConfig = props.ApplyConfig.clone()
	}
	now := clock.Now()
	p.createdAt = timeOr(props.CreatedAt, now)
	p.updatedAt = timeOr(props.UpdatedAt, now)
	// A Placement has no ID to tell a new one from a stored one; a stored one
	// has its creation time.
	if props.CreatedAt == nil {
		p.RecordEvent(PlacementCreated{p.Snapshot()})
	}
	return p, nil
}

func (p *Placement) OrganizationID() string       { return p.organizationID }
func (p *Placement) ApplicationID() string        { return p.applicationID }
func (p *Placement) TargetID() string             { return p.targetID }
func (p *Placement) Source() PlacementSource      { return p.source }
func (p *Placement) CurrentDeploymentID() *string { return copyString(p.currentDeploymentID) }
func (p *Placement) CurrentReleaseID() *string    { return copyString(p.currentReleaseID) }
func (p *Placement) CreatedAt() time.Time         { return p.createdAt }
func (p *Placement) UpdatedAt() time.Time         { return p.updatedAt }

// ApplyConfig returns a copy of how Releases are applied, or nil if Ivaldi
// cannot execute Deployments to this placement yet.
func (p *Placement) ApplyConfig() *ApplyConfig {
	if p.applyConfig == nil {
		return nil
	}
	return p.applyConfig.clone()
}

// Key identifies the Placement where one ID is expected, such as an event
// envelope's aggregate ID: its Application and Target IDs joined by a colon.
func (p *Placement) Key() string { return p.applicationID + ":" + p.targetID }

func (p *Placement) Snapshot() PlacementSnapshot {
	return PlacementSnapshot{
		OrganizationID:      p.organizationID,
		ApplicationID:       p.applicationID,
		TargetID:            p.targetID,
		Source:              p.source,
		ApplyConfig:         p.ApplyConfig(),
		CurrentDeploymentID: copyString(p.currentDeploymentID),
		CurrentReleaseID:    copyString(p.currentReleaseID),
		CreatedAt:           p.createdAt,
		UpdatedAt:           p.updatedAt,
	}
}

// Configure sets how Releases are applied. The caller checks that the
// definition artifact is one of the Application's. Configuring an observed
// placement makes it configured: someone has now set it up.
func (p *Placement) Configure(c ApplyConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	changed := p.source != PlacementConfigured || !p.applyConfig.equal(&c)
	p.applyConfig = c.clone()
	p.source = PlacementConfigured
	p.updatedAt = clock.Now()
	if changed {
		p.RecordEvent(PlacementApplyConfigSet{p.Snapshot()})
	}
	return nil
}

// ClearApplyConfig removes how Releases are applied, so Ivaldi only tracks
// Deployments here.
func (p *Placement) ClearApplyConfig() {
	if p.applyConfig != nil {
		p.applyConfig = nil
		p.updatedAt = clock.Now()
		p.RecordEvent(PlacementApplyConfigCleared{p.Snapshot()})
	}
}

// Remove records that the Placement is removed. The caller deletes it.
func (p *Placement) Remove() {
	p.RecordEvent(PlacementRemoved{p.Snapshot()})
}
