package delivery

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// ReleaseSource says how a Release came to exist.
type ReleaseSource string

const (
	// ReleaseCreated is a Release a person, an agent Session, or CI created through the API.
	ReleaseCreated ReleaseSource = "created"
	// ReleaseObserved is a Release the control plane made from digests the agent saw running.
	ReleaseObserved ReleaseSource = "observed"
)

// maxReleaseBuilds bounds the Builds one Release may carry, one per artifact.
const maxReleaseBuilds = 50

// Release is one version of an Application in one of its Channels, with one
// Build per artifact. A draft can be published, which fixes its Channel and
// Builds, and a published Release can be withdrawn so it is not deployed
// again. Its version is unique within the Application.
type Release struct {
	aggregate.Root
	organizationID       string
	applicationID        string
	channelID            string
	version              string
	status               release.Status
	source               ReleaseSource
	createdByPrincipalID *string
	publishDecisionID    *string
	publishedAt          *time.Time
	withdrawnAt          *time.Time
	builds               []ReleaseBuildSnapshot
	createdAt            time.Time
	updatedAt            time.Time
}

// ReleaseSnapshot is a Release's state, for repositories, audit records, and
// events. Its JSON form is part of the Release events' contract.
type ReleaseSnapshot struct {
	ID                   string                 `json:"id"`
	OrganizationID       string                 `json:"organization_id"`
	ApplicationID        string                 `json:"application_id"`
	ChannelID            string                 `json:"channel_id"`
	Version              string                 `json:"version"`
	Status               release.Status         `json:"status" enum:"draft,published,withdrawn"`
	Source               ReleaseSource          `json:"source" enum:"created,observed"`
	CreatedByPrincipalID *string                `json:"created_by_principal_id"`
	PublishDecisionID    *string                `json:"publish_decision_id"`
	PublishedAt          *time.Time             `json:"published_at"`
	WithdrawnAt          *time.Time             `json:"withdrawn_at"`
	Builds               []ReleaseBuildSnapshot `json:"builds"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

// ReleaseBuildSnapshot is the Build a Release carries for one artifact.
type ReleaseBuildSnapshot struct {
	ArtifactID string    `json:"artifact_id"`
	BuildID    string    `json:"build_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewReleaseProps describes a Release. A new one leaves Status, Source, and
// the timestamps empty; a repository loading a stored one sets them.
type NewReleaseProps struct {
	OrganizationID string
	ApplicationID  string
	ChannelID      string
	Version        string
	// Status defaults to draft.
	Status release.Status
	// Source defaults to created.
	Source               ReleaseSource
	CreatedByPrincipalID *string
	PublishDecisionID    *string
	PublishedAt          *time.Time
	WithdrawnAt          *time.Time
	// Builds are the Release's Builds, at most one per artifact, and at least one.
	Builds    []NewReleaseBuildProps
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// NewReleaseBuildProps names the Build a Release carries for one artifact.
// A new one leaves CreatedAt nil.
type NewReleaseBuildProps struct {
	ArtifactID string
	BuildID    string
	CreatedAt  *time.Time
}

// NewRelease returns a Release, with a new ID unless releaseID gives a stored one.
func NewRelease(props NewReleaseProps, releaseID *string) (*Release, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Application{}.Prefix(), props.ApplicationID) {
		return nil, NewErrInvalidInput("a release needs an application ID")
	}
	if !id.Valid(Channel{}.Prefix(), props.ChannelID) {
		return nil, NewErrInvalidInput("a release needs a channel ID")
	}
	root, err := newRoot(Release{}.Prefix(), releaseID)
	if err != nil {
		return nil, err
	}
	if err := validateVersionString("release version", props.Version); err != nil {
		return nil, err
	}
	if props.Status == "" {
		props.Status = release.StatusDraft
	}
	if !props.Status.Valid() {
		return nil, NewErrInvalidInput(fmt.Sprintf("release status %q must be draft, published, or withdrawn", props.Status))
	}
	if props.Source == "" {
		props.Source = ReleaseCreated
	}
	switch props.Source {
	case ReleaseCreated, ReleaseObserved:
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("release source %q must be created or observed", props.Source))
	}
	if len(props.Builds) == 0 {
		return nil, NewErrInvalidInput("a release carries at least one build")
	}
	if len(props.Builds) > maxReleaseBuilds {
		return nil, NewErrInvalidInput(fmt.Sprintf("a release can carry at most %d builds", maxReleaseBuilds))
	}

	now := clock.Now()
	r := &Release{
		Root:                 root,
		organizationID:       props.OrganizationID,
		applicationID:        props.ApplicationID,
		channelID:            props.ChannelID,
		version:              props.Version,
		status:               props.Status,
		source:               props.Source,
		createdByPrincipalID: optional.OfPtr(props.CreatedByPrincipalID),
		publishDecisionID:    optional.OfPtr(props.PublishDecisionID),
		publishedAt:          clock.CopyTime(props.PublishedAt),
		withdrawnAt:          clock.CopyTime(props.WithdrawnAt),
		createdAt:            timeOr(props.CreatedAt, now),
		updatedAt:            timeOr(props.UpdatedAt, now),
	}
	for _, b := range props.Builds {
		if !id.Valid(Artifact{}.Prefix(), b.ArtifactID) || !id.Valid(Build{}.Prefix(), b.BuildID) {
			return nil, NewErrInvalidInput("each release build needs an artifact ID and a build ID")
		}
		if slices.ContainsFunc(r.builds, func(o ReleaseBuildSnapshot) bool { return o.ArtifactID == b.ArtifactID }) {
			return nil, NewErrInvalidInput(fmt.Sprintf("a release carries one build of artifact %s, not two", b.ArtifactID))
		}
		r.builds = append(r.builds, ReleaseBuildSnapshot{ArtifactID: b.ArtifactID, BuildID: b.BuildID, CreatedAt: timeOr(b.CreatedAt, now)})
	}
	slices.SortFunc(r.builds, func(a, b ReleaseBuildSnapshot) int { return strings.Compare(a.ArtifactID, b.ArtifactID) })
	if r.IsNew() {
		r.RecordEvent(ReleaseCreatedEvent{r.Snapshot()})
	}
	return r, nil
}

func (Release) Prefix() string                    { return "rel" }
func (r *Release) OrganizationID() string         { return r.organizationID }
func (r *Release) ApplicationID() string          { return r.applicationID }
func (r *Release) ChannelID() string              { return r.channelID }
func (r *Release) Version() string                { return r.version }
func (r *Release) Status() release.Status         { return r.status }
func (r *Release) Source() ReleaseSource          { return r.source }
func (r *Release) PublishDecisionID() *string     { return copyString(r.publishDecisionID) }
func (r *Release) PublishedAt() *time.Time        { return clock.CopyTime(r.publishedAt) }
func (r *Release) WithdrawnAt() *time.Time        { return clock.CopyTime(r.withdrawnAt) }
func (r *Release) CreatedAt() time.Time           { return r.createdAt }
func (r *Release) UpdatedAt() time.Time           { return r.updatedAt }
func (r *Release) Builds() []ReleaseBuildSnapshot { return slices.Clone(r.builds) }

// BuildIDs returns the IDs of the Release's Builds, by artifact ID.
func (r *Release) BuildIDs() []string {
	ids := make([]string, len(r.builds))
	for i, b := range r.builds {
		ids[i] = b.BuildID
	}
	return ids
}

func (r *Release) Snapshot() ReleaseSnapshot {
	return ReleaseSnapshot{
		ID:                   r.ID(),
		OrganizationID:       r.organizationID,
		ApplicationID:        r.applicationID,
		ChannelID:            r.channelID,
		Version:              r.version,
		Status:               r.status,
		Source:               r.source,
		CreatedByPrincipalID: copyString(r.createdByPrincipalID),
		PublishDecisionID:    copyString(r.publishDecisionID),
		PublishedAt:          clock.CopyTime(r.publishedAt),
		WithdrawnAt:          clock.CopyTime(r.withdrawnAt),
		Builds:               slices.Clone(r.builds),
		CreatedAt:            r.createdAt,
		UpdatedAt:            r.updatedAt,
	}
}

func (r *Release) move(to release.Status) error {
	if err := release.CheckMove(r.status, to); err != nil {
		return fmt.Errorf("%w: %w", err, ErrReleaseStatus)
	}
	r.status = to
	r.updatedAt = clock.Now()
	return nil
}

// Publish publishes a draft, recording the authorizer's decision that
// allowed it. Whether the Release still meets its Channel's rules and has a
// Build of every required artifact is checked before, by the caller, which
// can read the Channel and the artifacts.
func (r *Release) Publish(decisionID string) error {
	if decisionID == "" {
		return NewErrInvalidInput("publishing records the decision that allowed it")
	}
	if err := r.move(release.StatusPublished); err != nil {
		return err
	}
	now := r.updatedAt
	r.publishedAt = &now
	r.publishDecisionID = &decisionID
	r.RecordEvent(ReleasePublished{r.Snapshot()})
	return nil
}

// Withdraw stops a published Release from being deployed again. What already
// runs keeps running.
func (r *Release) Withdraw() error {
	if err := r.move(release.StatusWithdrawn); err != nil {
		return err
	}
	now := r.updatedAt
	r.withdrawnAt = &now
	r.RecordEvent(ReleaseWithdrawn{r.Snapshot()})
	return nil
}
