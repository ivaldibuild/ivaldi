package delivery

import (
	"fmt"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

// ArtifactKind is the kind of thing an artifact's Builds are.
type ArtifactKind string

const (
	ArtifactContainerImage ArtifactKind = "container_image"
	ArtifactHelmChart      ArtifactKind = "helm_chart"
	ArtifactArchive        ArtifactKind = "archive"
)

// ArtifactSource says whether someone declared an artifact or the runtime
// agent found it in the Application's workloads.
type ArtifactSource string

const (
	ArtifactDeclared ArtifactSource = "declared"
	ArtifactObserved ArtifactSource = "observed"
)

// Artifact is one of the things an Application is made of, such as its
// api image, its migrate image, or its Helm chart. A Release has one Build
// for each artifact.
type Artifact struct {
	id             string
	organizationID string
	applicationID  string
	name           string
	artifactKind   ArtifactKind
	artifactName   string
	registryID     *string
	required       bool
	source         ArtifactSource
	createdAt      time.Time
	updatedAt      time.Time
	archivedAt     *time.Time
}

// ArtifactSnapshot is an Artifact's state, for repositories and audit records.
type ArtifactSnapshot struct {
	ID             string
	OrganizationID string
	ApplicationID  string
	Name           string
	ArtifactKind   ArtifactKind
	ArtifactName   string
	RegistryID     *string
	Required       bool
	Source         ArtifactSource
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
}

// NewArtifactProps describes an Artifact. A new one leaves the timestamps
// nil; a repository loading a stored one sets them.
type NewArtifactProps struct {
	OrganizationID string
	ApplicationID  string
	// Name is unique within the Application, such as "api" or "chart".
	Name         string
	ArtifactKind ArtifactKind
	// ArtifactName is what its Builds are published under, such as
	// ghcr.io/acme/api, without a tag or digest.
	ArtifactName string
	RegistryID   *string
	// Required says whether every Release must include a Build of it.
	Required bool
	// Source defaults to ArtifactDeclared.
	Source     ArtifactSource
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
	ArchivedAt *time.Time
}

// UpdateArtifactProps changes the fields that are set.
type UpdateArtifactProps struct {
	Name         *string
	ArtifactName *string
	Required     *bool
}

// NewArtifact returns an Artifact, with a new ID unless artifactID gives a stored one.
func NewArtifact(props NewArtifactProps, artifactID *string) (*Artifact, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Application{}.Prefix(), props.ApplicationID) {
		return nil, NewErrInvalidInput("an artifact needs an application ID")
	}
	if err := validateOwnID(Artifact{}.Prefix(), artifactID); err != nil {
		return nil, err
	}
	if err := validateArtifactName(props.Name); err != nil {
		return nil, err
	}
	switch props.ArtifactKind {
	case ArtifactContainerImage, ArtifactHelmChart, ArtifactArchive:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("artifact kind %q must be container_image, helm_chart, or archive", props.ArtifactKind))
	}
	if err := validatePublishedName(props.ArtifactName); err != nil {
		return nil, err
	}
	registry := optionalString(props.RegistryID)
	if registry != nil && !id.Valid(Registry{}.Prefix(), *registry) {
		return nil, NewErrInvalidInput(fmt.Sprintf("%q is not a registry ID", *registry))
	}
	if props.Source == "" {
		props.Source = ArtifactDeclared
	}
	if props.Source != ArtifactDeclared && props.Source != ArtifactObserved {
		return nil, NewErrInvalidInput(fmt.Sprintf("artifact source %q must be declared or observed", props.Source))
	}

	now := clock.Now()
	a := &Artifact{
		organizationID: props.OrganizationID,
		applicationID:  props.ApplicationID,
		name:           props.Name,
		artifactKind:   props.ArtifactKind,
		artifactName:   props.ArtifactName,
		registryID:     registry,
		required:       props.Required,
		source:         props.Source,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		archivedAt:     clock.CopyTime(props.ArchivedAt),
	}
	if artifactID != nil {
		a.id = *artifactID
	}
	id.Assign(&a.id, a)
	return a, nil
}

// validateArtifactName checks an artifact's name within its Application,
// which uses the slug format.
func validateArtifactName(name string) error {
	if err := slug.Validate(name); err != nil {
		return NewErrInvalidInput(fmt.Sprintf("artifact name %q must be lowercase letters, digits, and single hyphens", name))
	}
	return nil
}

// validatePublishedName checks the name Builds are published under. Tags
// and digests belong to Builds, so a digest is refused.
func validatePublishedName(name string) error {
	if name == "" || len(name) > maxPublishedNameLength || strings.ContainsAny(name, " \t\n@") || strings.Contains(name, "://") {
		return NewErrInvalidInput(fmt.Sprintf("artifact name %q must be 1-%d characters, without a scheme, spaces, or a digest", name, maxPublishedNameLength))
	}
	return nil
}

func (Artifact) Prefix() string                { return "art" }
func (a *Artifact) ID() string                 { return a.id }
func (a *Artifact) OrganizationID() string     { return a.organizationID }
func (a *Artifact) ApplicationID() string      { return a.applicationID }
func (a *Artifact) Name() string               { return a.name }
func (a *Artifact) ArtifactKind() ArtifactKind { return a.artifactKind }
func (a *Artifact) ArtifactName() string       { return a.artifactName }
func (a *Artifact) RegistryID() *string        { return copyString(a.registryID) }
func (a *Artifact) Required() bool             { return a.required }
func (a *Artifact) Source() ArtifactSource     { return a.source }
func (a *Artifact) CreatedAt() time.Time       { return a.createdAt }
func (a *Artifact) UpdatedAt() time.Time       { return a.updatedAt }
func (a *Artifact) ArchivedAt() *time.Time     { return clock.CopyTime(a.archivedAt) }

func (a *Artifact) Archived() bool { return a.archivedAt != nil }

func (a *Artifact) Snapshot() ArtifactSnapshot {
	return ArtifactSnapshot{
		ID:             a.id,
		OrganizationID: a.organizationID,
		ApplicationID:  a.applicationID,
		Name:           a.name,
		ArtifactKind:   a.artifactKind,
		ArtifactName:   a.artifactName,
		RegistryID:     copyString(a.registryID),
		Required:       a.required,
		Source:         a.source,
		CreatedAt:      a.createdAt,
		UpdatedAt:      a.updatedAt,
		ArchivedAt:     clock.CopyTime(a.archivedAt),
	}
}

func (a *Artifact) checkActive() error {
	if a.Archived() {
		return fmt.Errorf("%w: artifact %s", ErrArchived, a.id)
	}
	return nil
}

// Update changes an active Artifact. Its kind never changes. Nothing
// changes if any field is invalid.
func (a *Artifact) Update(props UpdateArtifactProps) error {
	if err := a.checkActive(); err != nil {
		return err
	}
	if props.Name != nil {
		if err := validateArtifactName(*props.Name); err != nil {
			return err
		}
	}
	if props.ArtifactName != nil {
		if err := validatePublishedName(*props.ArtifactName); err != nil {
			return err
		}
	}

	if props.Name != nil {
		a.name = *props.Name
	}
	if props.ArtifactName != nil {
		a.artifactName = *props.ArtifactName
	}
	if props.Required != nil {
		a.required = *props.Required
	}
	a.updatedAt = clock.Now()
	return nil
}

// UseRegistry records the Registry the artifact's Builds are looked up in.
// The caller checks that it is active and in the Application's Workspace.
func (a *Artifact) UseRegistry(registryID string) error {
	if err := a.checkActive(); err != nil {
		return err
	}
	if !id.Valid(Registry{}.Prefix(), registryID) {
		return NewErrInvalidInput(fmt.Sprintf("%q is not a registry ID", registryID))
	}
	a.registryID = &registryID
	a.updatedAt = clock.Now()
	return nil
}

// ClearRegistry removes the artifact's Registry.
func (a *Artifact) ClearRegistry() error {
	if err := a.checkActive(); err != nil {
		return err
	}
	a.registryID = nil
	a.updatedAt = clock.Now()
	return nil
}

// Declare marks an observed artifact as declared, once someone has
// confirmed it. Declaring a declared artifact does nothing.
func (a *Artifact) Declare() error {
	if err := a.checkActive(); err != nil {
		return err
	}
	if a.source != ArtifactDeclared {
		a.source = ArtifactDeclared
		a.updatedAt = clock.Now()
	}
	return nil
}

// Archive archives the Artifact. Archiving an archived one does nothing.
func (a *Artifact) Archive() {
	if !a.Archived() {
		now := clock.Now()
		a.archivedAt = &now
		a.updatedAt = now
	}
}

// Restore undoes Archive. Restoring an active Artifact does nothing.
func (a *Artifact) Restore() {
	if a.Archived() {
		a.archivedAt = nil
		a.updatedAt = clock.Now()
	}
}
