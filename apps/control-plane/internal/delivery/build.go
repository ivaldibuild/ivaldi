package delivery

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

// BuildProvenance says where a Build's record came from.
type BuildProvenance string

const (
	// BuildCIReported is a Build CI reported, with its commit and ref when CI gave them.
	BuildCIReported BuildProvenance = "ci_reported"
	// BuildObserved is a Build the runtime agent saw running without any CI record.
	BuildObserved BuildProvenance = "observed"
)

// Limits on a Build's fields. Text lengths match the columns where they have one.
const (
	maxDigestLength     = 255
	maxBuildVersion     = 255
	maxCIProviderLength = 50
	maxCIRunRefLength   = 2048
	maxSourceRefLength  = 1024
	maxReportedTags     = 50
	maxTagLength        = 128
)

var (
	// digestPattern is an OCI content digest: an algorithm, a colon, and an
	// encoded hash, such as sha256:<64 hex>.
	digestPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	// tagPattern is an OCI tag.
	tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// Build is one version of one artifact, identified by its artifact name and
// digest, never by a tag. A CI-reported Build never changes. An observed one
// changes once, when CI reports it and fills in its details.
type Build struct {
	aggregate.Root
	organizationID string
	artifactKind   ArtifactKind
	artifactName   string
	digest         string
	provenance     BuildProvenance
	details        BuildDetails
	createdAt      time.Time
	updatedAt      time.Time
}

// BuildDetails are what CI reports about a Build. Every field is optional.
type BuildDetails struct {
	// Version is the version CI gave the Build, such as 1.4.0 or a chart version.
	Version  *string
	SourceID *string
	// CommitSHA is the full or abbreviated commit the Build was built from.
	CommitSHA *string
	// SourceRef is the full Git ref it was built from, such as
	// refs/heads/main, refs/tags/v1.4.0, or refs/pull/42/head.
	SourceRef  *string
	CIProvider *string
	// CIRunRef identifies the CI run, such as its URL.
	CIRunRef *string
	// ReportedTags lists tags seen with the digest, as hints only.
	ReportedTags []string
	BuiltAt      *time.Time
}

// BuildSnapshot is a Build's state, for repositories, audit records, and
// events. Its JSON form is part of the Build events' contract.
type BuildSnapshot struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organization_id"`
	ArtifactKind   ArtifactKind    `json:"artifact_kind"`
	ArtifactName   string          `json:"artifact_name"`
	Digest         string          `json:"digest"`
	Provenance     BuildProvenance `json:"provenance"`
	Version        *string         `json:"version"`
	SourceID       *string         `json:"source_id"`
	CommitSHA      *string         `json:"commit_sha"`
	SourceRef      *string         `json:"source_ref"`
	CIProvider     *string         `json:"ci_provider"`
	CIRunRef       *string         `json:"ci_run_ref"`
	ReportedTags   []string        `json:"reported_tags"`
	BuiltAt        *time.Time      `json:"built_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// NewBuildProps describes a Build. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewBuildProps struct {
	OrganizationID string
	ArtifactKind   ArtifactKind
	ArtifactName   string
	Digest         string
	Provenance     BuildProvenance
	Details        BuildDetails
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
}

// NewBuild returns a Build, with a new ID unless buildID gives a stored one.
func NewBuild(props NewBuildProps, buildID *string) (*Build, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(Build{}.Prefix(), buildID)
	if err != nil {
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
	if err := ValidateDigest(props.Digest); err != nil {
		return nil, err
	}
	switch props.Provenance {
	case BuildCIReported:
	case BuildObserved:
		if props.Details.CommitSHA != nil {
			return nil, NewErrInvalidInput("an observed build has no commit")
		}
	default:
		return nil, NewErrInvalidInput(fmt.Sprintf("build provenance %q must be ci_reported or observed", props.Provenance))
	}
	details, err := props.Details.normalize()
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	b := &Build{
		Root:           root,
		organizationID: props.OrganizationID,
		artifactKind:   props.ArtifactKind,
		artifactName:   props.ArtifactName,
		digest:         props.Digest,
		provenance:     props.Provenance,
		details:        details,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
	}
	if b.IsNew() {
		b.RecordEvent(BuildRecorded{b.Snapshot()})
	}
	return b, nil
}

// ValidateDigest checks an OCI content digest, such as sha256:<64 hex>.
func ValidateDigest(d string) error {
	if len(d) > maxDigestLength || !digestPattern.MatchString(d) {
		return NewErrInvalidInput(fmt.Sprintf("digest %q must be a content digest such as sha256:<64 hex characters>", d))
	}
	algorithm, hash, _ := strings.Cut(d, ":")
	lengths := map[string]int{"sha256": 64, "sha512": 128}
	if n, known := lengths[algorithm]; known && (len(hash) != n || strings.ToLower(hash) != hash) {
		return NewErrInvalidInput(fmt.Sprintf("a %s digest has %d lower-case hex characters", algorithm, n))
	}
	return nil
}

// ValidateTag checks an OCI tag.
func ValidateTag(tag string) error {
	if !tagPattern.MatchString(tag) {
		return NewErrInvalidInput(fmt.Sprintf("tag %q must be 1-128 letters, digits, '_', '.', and '-', not starting with '.' or '-'", tag))
	}
	return nil
}

// normalize checks the details and drops empty values.
func (d BuildDetails) normalize() (BuildDetails, error) {
	out := BuildDetails{
		Version:    optionalString(d.Version),
		SourceID:   optionalString(d.SourceID),
		CommitSHA:  optionalString(d.CommitSHA),
		SourceRef:  optionalString(d.SourceRef),
		CIProvider: optionalString(d.CIProvider),
		CIRunRef:   optionalString(d.CIRunRef),
		BuiltAt:    clock.CopyTime(d.BuiltAt),
	}
	if v := out.Version; v != nil {
		if err := validateVersionString("build version", *v); err != nil {
			return BuildDetails{}, err
		}
	}
	// Sources belong to the version-control capability, which has no IDs to
	// check against yet, so only the column's length is checked.
	if s := out.SourceID; s != nil && (len(*s) > 30 || strings.ContainsAny(*s, " \t\n")) {
		return BuildDetails{}, NewErrInvalidInput(fmt.Sprintf("source ID %q is not a Source ID", *s))
	}
	if c := out.CommitSHA; c != nil && !commitPattern.MatchString(*c) {
		return BuildDetails{}, NewErrInvalidInput(fmt.Sprintf("commit %q must be 7-64 lower-case hex characters", *c))
	}
	if r := out.SourceRef; r != nil && (!strings.HasPrefix(*r, "refs/") || len(*r) > maxSourceRefLength || strings.ContainsAny(*r, " \t\n")) {
		return BuildDetails{}, NewErrInvalidInput(fmt.Sprintf("source ref %q must be a full Git ref such as refs/heads/main or refs/tags/v1.4.0", *r))
	}
	if p := out.CIProvider; p != nil && len(*p) > maxCIProviderLength {
		return BuildDetails{}, NewErrInvalidInput(fmt.Sprintf("CI provider must be at most %d characters", maxCIProviderLength))
	}
	if r := out.CIRunRef; r != nil && len(*r) > maxCIRunRefLength {
		return BuildDetails{}, NewErrInvalidInput(fmt.Sprintf("CI run reference must be at most %d characters", maxCIRunRefLength))
	}
	if len(d.ReportedTags) > maxReportedTags {
		return BuildDetails{}, NewErrInvalidInput(fmt.Sprintf("a build can have at most %d reported tags", maxReportedTags))
	}
	for _, tag := range d.ReportedTags {
		if err := ValidateTag(tag); err != nil {
			return BuildDetails{}, err
		}
	}
	out.ReportedTags = slices.Compact(slices.Sorted(slices.Values(d.ReportedTags)))
	if out.ReportedTags == nil {
		out.ReportedTags = []string{}
	}
	return out, nil
}

// validateVersionString checks a version as given, which need not be SemVer.
func validateVersionString(field, v string) error {
	if v == "" || len(v) > maxBuildVersion || strings.ContainsFunc(v, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return NewErrInvalidInput(fmt.Sprintf("%s %q must be 1-%d characters without spaces", field, v, maxBuildVersion))
	}
	return nil
}

func (Build) Prefix() string                 { return "bld" }
func (b *Build) OrganizationID() string      { return b.organizationID }
func (b *Build) ArtifactKind() ArtifactKind  { return b.artifactKind }
func (b *Build) ArtifactName() string        { return b.artifactName }
func (b *Build) Digest() string              { return b.digest }
func (b *Build) Provenance() BuildProvenance { return b.provenance }
func (b *Build) Version() *string            { return copyString(b.details.Version) }
func (b *Build) CommitSHA() *string          { return copyString(b.details.CommitSHA) }
func (b *Build) SourceRef() *string          { return copyString(b.details.SourceRef) }
func (b *Build) CreatedAt() time.Time        { return b.createdAt }
func (b *Build) UpdatedAt() time.Time        { return b.updatedAt }

func (b *Build) Snapshot() BuildSnapshot {
	d := b.details
	return BuildSnapshot{
		ID:             b.ID(),
		OrganizationID: b.organizationID,
		ArtifactKind:   b.artifactKind,
		ArtifactName:   b.artifactName,
		Digest:         b.digest,
		Provenance:     b.provenance,
		Version:        copyString(d.Version),
		SourceID:       copyString(d.SourceID),
		CommitSHA:      copyString(d.CommitSHA),
		SourceRef:      copyString(d.SourceRef),
		CIProvider:     copyString(d.CIProvider),
		CIRunRef:       copyString(d.CIRunRef),
		ReportedTags:   copyStrings(d.ReportedTags),
		BuiltAt:        clock.CopyTime(d.BuiltAt),
		CreatedAt:      b.createdAt,
		UpdatedAt:      b.updatedAt,
	}
}

// Report applies a CI report of the Build. An observed Build takes the
// report's details and becomes CI-reported; that is the only change a Build
// ever makes. A CI-reported Build does not change, and a report naming
// another commit is refused with ErrBuildConflict, because the same digest
// cannot come from two commits: either CI is misconfigured or the report is
// forged. It returns whether the Build changed.
func (b *Build) Report(report BuildDetails) (bool, error) {
	details, err := report.normalize()
	if err != nil {
		return false, err
	}
	if b.provenance == BuildCIReported {
		if c := details.CommitSHA; c != nil && b.details.CommitSHA != nil && !sameCommit(*c, *b.details.CommitSHA) {
			return false, fmt.Errorf("%s@%s was built from commit %s, not %s: %w", b.artifactName, b.digest, *b.details.CommitSHA, *c, ErrBuildConflict)
		}
		return false, nil
	}
	b.details = details
	b.provenance = BuildCIReported
	b.updatedAt = clock.Now()
	b.RecordEvent(BuildReported{b.Snapshot()})
	return true, nil
}

// sameCommit reports whether two commit hashes name the same commit, one
// possibly abbreviated.
func sameCommit(a, b string) bool {
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
