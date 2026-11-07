// Package releases implements delivery.ReleaseService: recording Builds and
// creating, publishing, and withdrawing Releases, and their HTTP API. The
// domain types stay in delivery; this package orchestrates them.
//
// NewReleaseService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it. The service below assumes
// that check has happened and records the decision that allowed each change
// in its audit record.
package releases

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// Applications reads an Application, its artifacts, and its Channels.
// delivery.ApplicationRepository provides it.
type Applications interface {
	GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error)
	GetApplicationByIdentifier(ctx context.Context, organizationID, identifier string) (*delivery.Application, error)
	ListArtifacts(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Artifact, error)
	ListChannels(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Channel, error)
}

// publishSubject is the subject the authorizing layer files the publish
// decision under when a Release is created and published in one call, apart
// from the decision to create it.
const publishSubject = "release.publish"

type ReleaseServiceOption func(*options)

type options struct {
	builds       delivery.BuildRepository
	releases     delivery.ReleaseRepository
	applications Applications
	tags         delivery.TagResolver
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithBuildRepo(r delivery.BuildRepository) ReleaseServiceOption {
	return func(o *options) { o.builds = r }
}
func WithReleaseRepo(r delivery.ReleaseRepository) ReleaseServiceOption {
	return func(o *options) { o.releases = r }
}
func WithApplications(a Applications) ReleaseServiceOption {
	return func(o *options) { o.applications = a }
}

// WithTagResolver resolves Builds named by tag. delivery.NoTagResolver
// refuses every tag until a registry client exists.
func WithTagResolver(t delivery.TagResolver) ReleaseServiceOption {
	return func(o *options) { o.tags = t }
}
func WithAuthorizer(a authorization.Authorizer) ReleaseServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repositories, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) ReleaseServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ReleaseServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ReleaseServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewReleaseService returns the ReleaseService, authorizing every
// operation, or an error naming a missing dependency.
func NewReleaseService(opts ...ReleaseServiceOption) (delivery.ReleaseService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"BuildRepo", o.builds != nil},
		{"ReleaseRepo", o.releases != nil},
		{"Applications", o.applications != nil},
		{"TagResolver", o.tags != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("release service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("release service: %w", err)
	}
	s := &service{options: o}
	return &authorized{next: s, authz: o.authz, applications: o.applications, releases: o.releases}, nil
}

// service carries out the operations once they are authorized.
type service struct {
	options
}

// conflict is a Build report the service refused because it named another
// commit; it is audited outside the failed transaction.
type conflict struct {
	build    delivery.BuildSnapshot
	reported string
}

func (s *service) ReportBuild(ctx context.Context, actor identity.Actor, in delivery.ReportBuildInput) (*delivery.Build, error) {
	var out *delivery.Build
	var refused *conflict
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		artifacts, err := s.activeArtifacts(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}
		out, refused, err = s.recordBuild(ctx, actor, in.OrganizationID, artifacts, in.Build)
		return err
	})
	if refused != nil {
		s.auditConflict(ctx, actor, in.OrganizationID, *refused)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// activeArtifacts returns an active Application's active artifacts.
func (s *service) activeArtifacts(ctx context.Context, organizationID, applicationID string) ([]*delivery.Artifact, error) {
	app, err := s.applications.GetApplication(ctx, organizationID, applicationID)
	if err != nil {
		return nil, err
	}
	if app.Archived() {
		return nil, fmt.Errorf("application %s is %w", applicationID, delivery.ErrArchived)
	}
	return s.applications.ListArtifacts(ctx, organizationID, applicationID, false)
}

// recordBuild finds or records the Build an input names, applying its CI
// details, in the caller's transaction. A report naming another commit for
// a recorded digest is returned as a conflict too, so it can be audited.
func (s *service) recordBuild(ctx context.Context, actor identity.Actor, organizationID string, artifacts []*delivery.Artifact, in delivery.BuildInput) (*delivery.Build, *conflict, error) {
	artifact := findArtifact(artifacts, in.Artifact)
	if artifact == nil {
		return nil, nil, delivery.NewErrInvalidInput(fmt.Sprintf("the application has no active artifact named %q", in.Artifact))
	}
	digest, err := s.digestOf(ctx, organizationID, artifact, in)
	if err != nil {
		return nil, nil, err
	}

	existing, err := s.builds.GetBuildByDigestForUpdate(ctx, organizationID, artifact.ArtifactName(), digest)
	if errors.Is(err, delivery.ErrNotFound) {
		b, err := delivery.NewBuild(delivery.NewBuildProps{
			OrganizationID: organizationID, ArtifactKind: artifact.ArtifactKind(), ArtifactName: artifact.ArtifactName(),
			Digest: digest, Provenance: delivery.BuildCIReported, Details: in.Details,
		}, nil)
		if err != nil {
			return nil, nil, err
		}
		created, err := s.builds.CreateBuild(ctx, b)
		if err != nil {
			return nil, nil, err
		}
		if created {
			if err := s.writer.Events(ctx, delivery.AggregateBuild, b.ID(), b); err != nil {
				return nil, nil, err
			}
			return b, nil, s.writer.AuditRecord(ctx, audit.Event{
				Type: delivery.EventBuildRecorded, OrganizationID: organizationID, ResourceID: b.ID(), Actor: actor, After: b.Snapshot(),
			})
		}
		// Someone recorded it meanwhile; apply this report to theirs.
		existing, err = s.builds.GetBuildByDigestForUpdate(ctx, organizationID, artifact.ArtifactName(), digest)
	}
	if err != nil {
		return nil, nil, err
	}
	if existing.ArtifactKind() != artifact.ArtifactKind() {
		return nil, nil, delivery.NewErrInvalidInput(fmt.Sprintf("%s@%s is recorded as a %s, not a %s", artifact.ArtifactName(), digest, existing.ArtifactKind(), artifact.ArtifactKind()))
	}
	before := existing.Snapshot()
	changed, err := existing.Report(in.Details)
	if errors.Is(err, delivery.ErrBuildConflict) {
		return nil, &conflict{build: before, reported: *in.Details.CommitSHA}, err
	}
	if err != nil || !changed {
		return existing, nil, err
	}
	if err := s.builds.UpdateBuild(ctx, existing); err != nil {
		return nil, nil, err
	}
	if err := s.writer.Events(ctx, delivery.AggregateBuild, existing.ID(), existing); err != nil {
		return nil, nil, err
	}
	return existing, nil, s.writer.AuditRecord(ctx, audit.Event{
		Type: delivery.EventBuildReported, OrganizationID: organizationID, ResourceID: existing.ID(), Actor: actor,
		Before: before, After: existing.Snapshot(),
	})
}

// digestOf returns the digest an input names, resolving a tag through the
// artifact's Registry.
func (s *service) digestOf(ctx context.Context, organizationID string, artifact *delivery.Artifact, in delivery.BuildInput) (string, error) {
	switch {
	case in.Digest != "" && in.Tag != "":
		return "", delivery.NewErrInvalidInput(fmt.Sprintf("name the %s build by digest or by tag, not both", artifact.Name()))
	case in.Digest != "":
		return in.Digest, delivery.ValidateDigest(in.Digest)
	case in.Tag != "":
		if err := delivery.ValidateTag(in.Tag); err != nil {
			return "", err
		}
		digest, err := s.tags.ResolveTag(ctx, organizationID, artifact, in.Tag)
		if err != nil {
			return "", fmt.Errorf("resolve %s:%s: %w", artifact.ArtifactName(), in.Tag, err)
		}
		return digest, delivery.ValidateDigest(digest)
	}
	return "", delivery.NewErrInvalidInput(fmt.Sprintf("name the %s build by its digest", artifact.Name()))
}

// auditConflict records a refused report in a transaction of its own, so
// the record survives the request failing. Failing to record it is not
// reported over the conflict itself.
func (s *service) auditConflict(ctx context.Context, actor identity.Actor, organizationID string, c conflict) {
	_ = s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: delivery.EventBuildReportRefused, OrganizationID: organizationID, ResourceID: c.build.ID, Actor: actor,
			Before: c.build, After: map[string]string{"reported_commit_sha": c.reported},
		})
	})
}

func findArtifact(artifacts []*delivery.Artifact, name string) *delivery.Artifact {
	for _, a := range artifacts {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

// candidate is how Channel rules see a Release with these Builds.
func candidate(version string, artifacts []*delivery.Artifact, builds map[string]*delivery.Build) release.Candidate {
	c := release.Candidate{Version: version}
	for _, a := range artifacts {
		b, ok := builds[a.ID()]
		if !ok {
			continue
		}
		cb := release.CandidateBuild{ArtifactID: a.ID(), ArtifactName: a.Name()}
		if v := b.Version(); v != nil {
			cb.Version = *v
		}
		if r := b.SourceRef(); r != nil {
			cb.SourceRef = *r
		}
		c.Builds = append(c.Builds, cb)
	}
	return c
}

func (s *service) CreateRelease(ctx context.Context, actor identity.Actor, in delivery.CreateReleaseInput) (*delivery.ReleaseDetail, error) {
	if len(in.Builds) == 0 {
		return nil, delivery.NewErrInvalidInput("a release carries at least one build")
	}

	var out *delivery.ReleaseDetail
	var refused *conflict

	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		artifacts, err := s.activeArtifacts(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}

		byArtifact := map[string]*delivery.Build{}
		props := delivery.NewReleaseProps{
			OrganizationID:       in.OrganizationID,
			ApplicationID:        in.ApplicationID,
			Version:              in.Version,
			CreatedByPrincipalID: optional.Of(actor.PrincipalID),
			TriggeredBy:          optional.Of(in.TriggeredBy),
			TriggeredByEmail:     optional.Of(in.TriggeredByEmail),
		}

		for _, bi := range in.Builds {
			artifact := findArtifact(artifacts, bi.Artifact)
			if artifact != nil && byArtifact[artifact.ID()] != nil {
				return delivery.NewErrInvalidInput(fmt.Sprintf("the release names two builds of %s", bi.Artifact))
			}
			b, c, err := s.recordBuild(ctx, actor, in.OrganizationID, artifacts, bi)
			if err != nil {
				refused = c
				return err
			}
			byArtifact[artifact.ID()] = b
			props.Builds = append(props.Builds, delivery.NewReleaseBuildProps{ArtifactID: artifact.ID(), BuildID: b.ID()})
		}

		channels, err := s.applications.ListChannels(ctx, in.OrganizationID, in.ApplicationID, true)
		if err != nil {
			return err
		}

		channel, err := chooseChannel(channels, in.ChannelID, candidate(in.Version, artifacts, byArtifact))
		if err != nil {
			return err
		}

		props.ChannelID = channel.ID()
		rel, err := delivery.NewRelease(props, nil)
		if err != nil {
			return err
		}
		if err := s.releases.CreateRelease(ctx, rel); err != nil {
			if errors.Is(err, delivery.ErrAlreadyExists) {
				return fmt.Errorf("the application already has a release %s: %w", in.Version, delivery.ErrAlreadyExists)
			}
			return err
		}

		if err := s.writer.Events(ctx, delivery.AggregateRelease, rel.ID(), rel); err != nil {
			return err
		}

		if err := s.writer.AuditRecord(ctx, audit.Event{
			Type: delivery.EventReleaseCreated, OrganizationID: in.OrganizationID, ResourceID: rel.ID(), Actor: actor, After: rel.Snapshot(),
		}); err != nil {
			return err
		}

		if in.Publish {
			// The publish decision is filed apart from the one to create.
			d, ok := authorization.DecisionFor(ctx, publishSubject)
			if !ok {
				return change.ErrUnauthorized
			}
			if err := s.publish(authorization.WithDecision(ctx, rel.ID(), d), actor, rel, channel, artifacts, byArtifact); err != nil {
				return err
			}
		}

		out = detail(rel, byArtifact)

		return nil
	})
	if refused != nil {
		s.auditConflict(ctx, actor, in.OrganizationID, *refused)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// chooseChannel returns the Channel named, if it admits the Release, or
// else the one active Channel that admits it.
func chooseChannel(channels []*delivery.Channel, channelID string, c release.Candidate) (*delivery.Channel, error) {
	if channelID != "" {
		i := slices.IndexFunc(channels, func(ch *delivery.Channel) bool { return ch.ID() == channelID })
		if i < 0 {
			return nil, delivery.NewErrInvalidInput(fmt.Sprintf("the application has no channel %s", channelID))
		}
		ch := channels[i]
		if ch.Archived() {
			return nil, fmt.Errorf("channel %s is %w", ch.Slug(), delivery.ErrArchived)
		}
		ok, reasons, err := ch.Admits(c)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, &delivery.RulesError{Message: fmt.Sprintf("the release does not meet channel %s's rules", ch.Slug()), Reasons: reasons}
		}
		return ch, nil
	}

	var admitted []*delivery.Channel
	var reasons []string
	for _, ch := range channels {
		if ch.Archived() {
			continue
		}
		ok, why, err := ch.Admits(c)
		if err != nil {
			return nil, err
		}
		if ok {
			admitted = append(admitted, ch)
		} else {
			reasons = append(reasons, ch.Slug()+": "+strings.Join(why, ", "))
		}
	}
	switch len(admitted) {
	case 1:
		return admitted[0], nil
	case 0:
		return nil, &delivery.RulesError{Message: "no channel's rules admit the release", Reasons: reasons}
	}
	names := make([]string, len(admitted))
	for i, ch := range admitted {
		names[i] = ch.Slug()
	}
	return nil, &delivery.RulesError{Message: "several channels admit the release; name one", Candidates: names}
}

// publish checks a draft against its Channel's rules again and that every
// required artifact has a Build, then publishes it under the decision the
// context carries for it.
func (s *service) publish(ctx context.Context, actor identity.Actor, rel *delivery.Release, channel *delivery.Channel, artifacts []*delivery.Artifact, builds map[string]*delivery.Build) error {
	if channel.Archived() {
		return fmt.Errorf("channel %s is %w", channel.Slug(), delivery.ErrArchived)
	}
	ok, reasons, err := channel.Admits(candidate(rel.Version(), artifacts, builds))
	if err != nil {
		return err
	}
	if !ok {
		return &delivery.RulesError{Message: fmt.Sprintf("the release no longer meets channel %s's rules", channel.Slug()), Reasons: reasons}
	}
	var missing []string
	for _, a := range artifacts {
		if a.Required() && builds[a.ID()] == nil {
			missing = append(missing, a.Name())
		}
	}
	if len(missing) > 0 {
		return delivery.NewErrInvalidInput("the release has no build of required artifact " + strings.Join(missing, ", "))
	}
	d, ok := authorization.DecisionFor(ctx, rel.ID())
	if !ok {
		return change.ErrUnauthorized
	}
	before := rel.Snapshot()
	if err := rel.Publish(d.ID); err != nil {
		return err
	}
	return s.save(ctx, actor, rel, delivery.EventReleasePublished, before)
}

// save writes a changed Release with its events and audit record.
func (s *service) save(ctx context.Context, actor identity.Actor, rel *delivery.Release, event string, before delivery.ReleaseSnapshot) error {
	if err := s.releases.UpdateRelease(ctx, rel); err != nil {
		return err
	}
	if err := s.writer.Events(ctx, delivery.AggregateRelease, rel.ID(), rel); err != nil {
		return err
	}
	return s.writer.AuditRecord(ctx, audit.Event{
		Type: event, OrganizationID: rel.OrganizationID(), ResourceID: rel.ID(), Actor: actor, Before: before, After: rel.Snapshot(),
	})
}

func (s *service) PublishRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*delivery.ReleaseDetail, error) {
	var out *delivery.ReleaseDetail
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		rel, err := s.releases.GetReleaseForUpdate(ctx, organizationID, releaseID)
		if err != nil {
			return err
		}
		artifacts, err := s.activeArtifacts(ctx, organizationID, rel.ApplicationID())
		if err != nil {
			return err
		}
		channels, err := s.applications.ListChannels(ctx, organizationID, rel.ApplicationID(), true)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(channels, func(ch *delivery.Channel) bool { return ch.ID() == rel.ChannelID() })
		if i < 0 {
			return fmt.Errorf("release %s: channel %s: %w", releaseID, rel.ChannelID(), delivery.ErrNotFound)
		}
		builds, err := s.buildsOf(ctx, rel)
		if err != nil {
			return err
		}
		if err := s.publish(ctx, actor, rel, channels[i], artifacts, builds); err != nil {
			return err
		}
		out = detail(rel, builds)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) WithdrawRelease(ctx context.Context, actor identity.Actor, organizationID, releaseID string) (*delivery.ReleaseDetail, error) {
	var out *delivery.ReleaseDetail
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		rel, err := s.releases.GetReleaseForUpdate(ctx, organizationID, releaseID)
		if err != nil {
			return err
		}
		before := rel.Snapshot()
		if err := rel.Withdraw(); err != nil {
			return err
		}
		if err := s.save(ctx, actor, rel, delivery.EventReleaseWithdrawn, before); err != nil {
			return err
		}
		builds, err := s.buildsOf(ctx, rel)
		if err != nil {
			return err
		}
		out = detail(rel, builds)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) GetRelease(ctx context.Context, _ identity.Actor, organizationID, releaseID string) (*delivery.ReleaseDetail, error) {
	rel, err := s.releases.GetRelease(ctx, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	builds, err := s.buildsOf(ctx, rel)
	if err != nil {
		return nil, err
	}
	return detail(rel, builds), nil
}

func (s *service) ListReleases(ctx context.Context, _ identity.Actor, q delivery.ListReleasesQuery) (*pagination.Result[*delivery.ReleaseDetail], error) {
	if q.Status != "" && !q.Status.Valid() {
		return nil, delivery.NewErrInvalidInput(fmt.Sprintf("status %q must be draft, published, or withdrawn", q.Status))
	}
	page, err := s.releases.ListReleases(ctx, q)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, rel := range page.Items {
		ids = append(ids, rel.BuildIDs()...)
	}
	builds, err := s.builds.GetBuilds(ctx, q.OrganizationID, slices.Compact(slices.Sorted(slices.Values(ids))))
	if err != nil {
		return nil, err
	}
	byID := map[string]*delivery.Build{}
	for _, b := range builds {
		byID[b.ID()] = b
	}
	items := make([]*delivery.ReleaseDetail, len(page.Items))
	for i, rel := range page.Items {
		items[i] = detail(rel, byArtifactOf(rel, byID))
	}
	return &pagination.Result[*delivery.ReleaseDetail]{
		Items: items, TotalCount: page.TotalCount, HasNext: page.HasNext, HasPrevious: page.HasPrevious,
		NextCursor: page.NextCursor, PreviousCursor: page.PreviousCursor,
	}, nil
}

// buildsOf returns a Release's Builds by artifact ID.
func (s *service) buildsOf(ctx context.Context, rel *delivery.Release) (map[string]*delivery.Build, error) {
	builds, err := s.builds.GetBuilds(ctx, rel.OrganizationID(), rel.BuildIDs())
	if err != nil {
		return nil, err
	}
	byID := map[string]*delivery.Build{}
	for _, b := range builds {
		byID[b.ID()] = b
	}
	return byArtifactOf(rel, byID), nil
}

func byArtifactOf(rel *delivery.Release, byID map[string]*delivery.Build) map[string]*delivery.Build {
	out := map[string]*delivery.Build{}
	for _, rb := range rel.Builds() {
		if b, ok := byID[rb.BuildID]; ok {
			out[rb.ArtifactID] = b
		}
	}
	return out
}

// detail pairs a Release with its Builds, in the order of its Builds.
func detail(rel *delivery.Release, byArtifact map[string]*delivery.Build) *delivery.ReleaseDetail {
	d := &delivery.ReleaseDetail{Release: rel}
	for _, rb := range rel.Builds() {
		d.Builds = append(d.Builds, byArtifact[rb.ArtifactID])
	}
	return d
}
