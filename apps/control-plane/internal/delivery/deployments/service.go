// Package deployments implements delivery.DeploymentService: requesting,
// tracking, cancelling, and reporting Deployments. The domain types stay in
// delivery; this package orchestrates them.
package deployments

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
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
	"github.com/ivaldibuild/ivaldi/pkg/vcs"
)

const maxRequestedTargets = 20

type ServiceOption func(*options)

type options struct {
	deployments  delivery.DeploymentRepository
	releases     Releases
	builds       Builds
	applications Applications
	environments Environments
	targets      Targets
	clusters     Clusters
	lifecycles   Lifecycles
	history      vcs.History
	changes      provenance.Lookup
	authz        authorization.Authorizer
	writer       change.Writer
}

func WithDeploymentRepo(r delivery.DeploymentRepository) ServiceOption {
	return func(o *options) { o.deployments = r }
}
func WithReleaseRepo(r Releases) ServiceOption {
	return func(o *options) { o.releases = r }
}
func WithBuildRepo(r Builds) ServiceOption {
	return func(o *options) { o.builds = r }
}
func WithApplications(r Applications) ServiceOption {
	return func(o *options) { o.applications = r }
}
func WithEnvironments(r Environments) ServiceOption {
	return func(o *options) { o.environments = r }
}
func WithTargets(r Targets) ServiceOption {
	return func(o *options) { o.targets = r }
}
func WithLifecycles(r Lifecycles) ServiceOption {
	return func(o *options) { o.lifecycles = r }
}
func WithClusters(r Clusters) ServiceOption {
	return func(o *options) { o.clusters = r }
}

// WithCommitHistory reads the commits between two Builds. The
// version-control capability provides it through vcs.History.
func WithCommitHistory(h vcs.History) ServiceOption {
	return func(o *options) { o.history = h }
}

// WithChanges reads the Changes Ivaldi recorded for commits, so release
// notes carry their Session and Work Item. It is optional: without it,
// commits are still listed with no attribution.
func WithChanges(l provenance.Lookup) ServiceOption {
	return func(o *options) { o.changes = l }
}
func WithAuthorizer(a authorization.Authorizer) ServiceOption {
	return func(o *options) { o.authz = a }
}
func WithTransactor(tx transaction.Transactor) ServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewService returns the DeploymentService, authorizing every operation, or
// an error naming a missing dependency.
func NewService(opts ...ServiceOption) (delivery.DeploymentService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"DeploymentRepo", o.deployments != nil},
		{"ReleaseRepo", o.releases != nil},
		{"BuildRepo", o.builds != nil},
		{"Applications", o.applications != nil},
		{"Environments", o.environments != nil},
		{"Targets", o.targets != nil},
		{"Clusters", o.clusters != nil},
		{"Lifecycles", o.lifecycles != nil},
		{"CommitHistory", o.history != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("deployment service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("deployment service: %w", err)
	}
	return &authorized{next: &service{options: o}, authz: o.authz, releases: o.releases,
		targets: o.targets, applications: o.applications, deployments: o.deployments}, nil
}

type service struct {
	options
}

func (s *service) RequestDeployments(ctx context.Context, actor identity.Actor, in delivery.RequestDeploymentsInput) ([]*delivery.Deployment, error) {
	if len(in.TargetIDs) == 0 {
		return nil, delivery.NewErrInvalidInput("at least one target is required")
	}
	if len(in.TargetIDs) > maxRequestedTargets {
		return nil, delivery.NewErrInvalidInput(fmt.Sprintf("one request can deploy to at most %d targets", maxRequestedTargets))
	}
	seen := map[string]bool{}
	for _, id := range in.TargetIDs {
		if seen[id] {
			return nil, delivery.NewErrInvalidInput(fmt.Sprintf("target %s is given twice", id))
		}
		seen[id] = true
	}

	var out []*delivery.Deployment
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		rel, err := s.releases.GetRelease(ctx, in.OrganizationID, in.ReleaseID)
		if err != nil {
			return err
		}
		if rel.Status() != release.StatusPublished {
			return fmt.Errorf("%w: release %s is %s", delivery.ErrReleaseStatus, rel.ID(), rel.Status())
		}
		for _, targetID := range in.TargetIDs {
			plan, err := s.plan(ctx, in.OrganizationID, rel, targetID)
			if err != nil {
				return err
			}
			if failed := firstFailure(plan.checks); failed != nil {
				return failed
			}
			decision, ok := authorization.DecisionFor(ctx, targetID)
			if !ok {
				return change.ErrUnauthorized
			}
			status := release.DeploymentApplying
			if decision.Outcome == authorization.OutcomeRequiresApproval {
				status = release.DeploymentAwaitingApproval
			}
			now := clock.Now()
			props := delivery.NewDeploymentProps{
				OrganizationID: in.OrganizationID, ApplicationID: rel.ApplicationID(),
				ReleaseID: rel.ID(), EnvironmentID: plan.target.EnvironmentID(), TargetID: targetID,
				Source: delivery.DeploymentExecuted, Reason: delivery.DeploymentRollout, Status: status,
				PreviousDeploymentID:   plan.placement.CurrentDeploymentID(),
				InitiatedByPrincipalID: optional.Of(actor.PrincipalID), SessionID: in.SessionID,
				WorkItemID: in.WorkItemID, PolicyDecisionID: optional.Of(decision.ID),
				RequestedAt: &now,
			}
			if status == release.DeploymentApplying {
				props.StartedAt = &now
			}
			d, err := delivery.NewDeployment(props, nil)
			if err != nil {
				return err
			}
			if err := s.deployments.CreateDeployment(ctx, d); err != nil {
				return err
			}
			if err := s.appendStatusEvent(ctx, d, nil); err != nil {
				return err
			}
			dctx := authorization.WithDecision(ctx, d.ID(), decision)
			if err := s.writer.Events(dctx, delivery.AggregateDeployment, d.ID(), d); err != nil {
				return err
			}
			if err := s.writer.AuditRecord(dctx, audit.Event{
				Type: delivery.EventDeploymentRequested, OrganizationID: in.OrganizationID,
				ResourceID: d.ID(), Actor: actor, After: d.Snapshot(),
			}); err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) GetDeployment(ctx context.Context, _ identity.Actor, organizationID, deploymentID string) (*delivery.DeploymentDetail, error) {
	d, err := s.deployments.GetDeployment(ctx, organizationID, deploymentID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, d)
}

func (s *service) ListDeployments(ctx context.Context, _ identity.Actor, query delivery.ListDeploymentsQuery) (*pagination.Result[delivery.DeploymentSnapshot], error) {
	if query.Status != "" && !query.Status.Valid() {
		return nil, delivery.NewErrInvalidInput(fmt.Sprintf("deployment status %q is not known", query.Status))
	}
	page, err := s.deployments.ListDeployments(ctx, query)
	if err != nil {
		return nil, err
	}
	items := make([]delivery.DeploymentSnapshot, len(page.Items))
	for i, d := range page.Items {
		items[i] = d.Snapshot()
	}
	return pagination.NewResult(items, page.TotalCount, page.HasNext, page.HasPrevious, page.NextCursor, page.PreviousCursor), nil
}

func (s *service) CancelDeployment(ctx context.Context, actor identity.Actor, organizationID, deploymentID string) (*delivery.DeploymentDetail, error) {
	var d *delivery.Deployment
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		d, err = s.deployments.GetDeploymentForUpdate(ctx, organizationID, deploymentID)
		if err != nil {
			return err
		}
		before := d.Snapshot()
		if err := d.MoveTo(release.DeploymentCancelled); err != nil {
			return err
		}
		if err := s.deployments.UpdateDeployment(ctx, d); err != nil {
			return err
		}
		if err := s.appendStatusEvent(ctx, d, nil); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, delivery.AggregateDeployment, d.ID(), d); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: delivery.EventDeploymentCancelled, OrganizationID: organizationID, ResourceID: d.ID(),
			Actor: actor, Before: before, After: d.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, d)
}

func (s *service) ListAssignments(ctx context.Context, actor identity.Actor, organizationID, targetID string) ([]delivery.DeploymentAssignment, error) {
	target, err := s.targets.GetTarget(ctx, organizationID, targetID)
	if err != nil {
		return nil, err
	}
	if err := s.checkTargetAgent(ctx, organizationID, target, actor.PrincipalID); err != nil {
		return nil, err
	}
	// The statuses are filtered in the query, not after it, so a Target's
	// finished history never pushes its in-flight work off the page. At most
	// one Deployment per Application is in flight on a Target, but every page
	// is read regardless.
	query := delivery.ListDeploymentsQuery{
		OrganizationID: organizationID, TargetID: targetID,
		Statuses: []release.DeploymentStatus{release.DeploymentApplying, release.DeploymentProgressing},
		Page:     pagination.Query{Limit: delivery.AssignmentPageSize},
	}
	var out []delivery.DeploymentAssignment
	for {
		page, err := s.deployments.ListDeployments(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, d := range page.Items {
			assignment, err := s.assignment(ctx, d)
			if err != nil {
				return nil, err
			}
			out = append(out, *assignment)
		}
		if !page.HasNext || page.NextCursor == nil {
			return out, nil
		}
		query.Page.NextCursor = page.NextCursor
	}
}

func (s *service) ReportDeployment(ctx context.Context, actor identity.Actor, in delivery.ReportDeploymentInput) (*delivery.DeploymentDetail, error) {
	var out *delivery.Deployment
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		d, err := s.deployments.GetDeploymentForUpdate(ctx, in.OrganizationID, in.DeploymentID)
		if err != nil {
			return err
		}
		// The Target is locked so it cannot be archived mid-report, and the
		// Cluster so re-enrolment cannot swap the agent mid-report.
		target, err := s.targets.GetTargetForUpdate(ctx, in.OrganizationID, d.TargetID())
		if err != nil {
			return err
		}
		if err := s.checkTargetAgentLocked(ctx, in.OrganizationID, target, actor.PrincipalID); err != nil {
			return err
		}
		event, err := delivery.NewDeploymentEvent(delivery.NewDeploymentEventProps{
			OrganizationID: in.OrganizationID, DeploymentID: d.ID(), Kind: in.Kind, Status: in.Status,
			Detail: in.Detail, ReportID: &in.ReportID, ReportedByPrincipalID: optional.Of(actor.PrincipalID),
			OccurredAt: in.OccurredAt,
		}, nil)
		if err != nil {
			return err
		}
		inserted, err := s.deployments.AppendDeploymentEvent(ctx, event)
		if err != nil {
			return err
		}
		if !inserted {
			out = d
			return nil
		}
		if in.Status == nil {
			out = d
			return nil
		}
		if err := d.MoveTo(*in.Status); err != nil {
			return err
		}
		if err := s.deployments.UpdateDeployment(ctx, d); err != nil {
			return err
		}
		if err := s.applyCurrent(ctx, d); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, delivery.AggregateDeployment, d.ID(), d); err != nil {
			return err
		}
		out = d
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, out)
}

// RecordTargetHeartbeat notes that the Target's Cluster agent is in contact.
// Liveness is the Cluster's now, so this only checks the caller; the
// cluster-wide heartbeat replaces it.
func (s *service) RecordTargetHeartbeat(ctx context.Context, actor identity.Actor, organizationID, targetID string) error {
	target, err := s.targets.GetTarget(ctx, organizationID, targetID)
	if err != nil {
		return err
	}
	return s.checkTargetAgent(ctx, organizationID, target, actor.PrincipalID)
}

// checkTargetAgent refuses anyone but the agent of the Target's Cluster.
func (s *service) checkTargetAgent(ctx context.Context, organizationID string, target *delivery.Target, principalID string) error {
	cluster, err := s.clusters.GetCluster(ctx, organizationID, target.ClusterID())
	if err != nil {
		return err
	}
	return requireClusterAgent(cluster, target, principalID)
}

// checkTargetAgentLocked is checkTargetAgent that also takes a shared lock on
// the Cluster, so a re-enrolment cannot change the agent between the check
// and the write. The lock is shared so a Cluster's reports do not queue
// behind each other.
func (s *service) checkTargetAgentLocked(ctx context.Context, organizationID string, target *delivery.Target, principalID string) error {
	cluster, err := s.clusters.GetClusterForShare(ctx, organizationID, target.ClusterID())
	if err != nil {
		return err
	}
	return requireClusterAgent(cluster, target, principalID)
}

func requireClusterAgent(cluster *delivery.Cluster, target *delivery.Target, principalID string) error {
	agent := cluster.AgentPrincipalID()
	if agent == nil || *agent != principalID {
		return fmt.Errorf("%w: %s does not serve target %s", delivery.ErrNotDeploymentAgent, principalID, target.ID())
	}
	return nil
}

func (s *service) ReleaseChanges(ctx context.Context, _ identity.Actor, organizationID, releaseID, targetID string) (*delivery.ReleaseChanges, error) {
	rel, err := s.releases.GetRelease(ctx, organizationID, releaseID)
	if err != nil {
		return nil, err
	}
	plan, err := s.plan(ctx, organizationID, rel, targetID)
	if err != nil {
		return nil, err
	}
	artifacts, err := s.applications.ListArtifacts(ctx, organizationID, rel.ApplicationID(), true)
	if err != nil {
		return nil, err
	}
	byArtifact := map[string]*delivery.Artifact{}
	for _, a := range artifacts {
		byArtifact[a.ID()] = a
	}
	next, err := s.buildsOf(ctx, rel)
	if err != nil {
		return nil, err
	}
	var previous map[string]*delivery.Build
	changes := &delivery.ReleaseChanges{
		ReleaseID: rel.ID(), TargetID: targetID,
		Artifacts: []delivery.ArtifactChange{}, Changes: []delivery.ReleaseChange{},
	}
	if plan.placement != nil && plan.placement.CurrentReleaseID() != nil {
		changes.CurrentReleaseID = plan.placement.CurrentReleaseID()
		prevRelease, err := s.releases.GetRelease(ctx, organizationID, *plan.placement.CurrentReleaseID())
		if err != nil {
			return nil, err
		}
		previous, err = s.buildsOf(ctx, prevRelease)
		if err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	for _, rb := range rel.Builds() {
		artifact := byArtifact[rb.ArtifactID]
		if artifact == nil {
			continue
		}
		b := next[rb.ArtifactID]
		if b == nil {
			continue
		}
		item := delivery.ArtifactChange{
			ArtifactID:   rb.ArtifactID,
			ArtifactName: artifact.Name(),
			Next:         b.Snapshot(),
			CommitSHAs:   []string{},
			Unresolved:   []string{},
		}
		prev := previous[rb.ArtifactID]
		if prev != nil {
			snap := prev.Snapshot()
			item.Previous = &snap
		}
		if prev == nil {
			item.Unresolved = append(item.Unresolved, "the artifact is new on this Target")
		} else if prev.Digest() != b.Digest() {
			commits, reason := s.commitRange(ctx, organizationID, prev, b)
			if reason != "" {
				item.Unresolved = append(item.Unresolved, reason)
			}
			sourceID := sourceOf(b)
			for _, commit := range commits {
				item.CommitSHAs = append(item.CommitSHAs, commit.SHA)
				key := sourceID + "\x00" + commit.SHA
				if seen[key] {
					continue
				}
				seen[key] = true
				changes.Changes = append(changes.Changes, changeOf(sourceID, commit))
			}
		}
		changes.Artifacts = append(changes.Artifacts, item)
	}
	slices.SortFunc(changes.Changes, func(a, b delivery.ReleaseChange) int {
		if !a.CommittedAt.Equal(b.CommittedAt) {
			return a.CommittedAt.Compare(b.CommittedAt)
		}
		return strings.Compare(a.CommitSHA, b.CommitSHA)
	})
	if err := s.attributeChanges(ctx, organizationID, changes.Changes); err != nil {
		return nil, err
	}
	changes.Checks = plan.checks
	return changes, nil
}

// attributeChanges fills each commit's recorded Change, Session, and Work
// Item. Without a provenance lookup, commits stay unattributed.
func (s *service) attributeChanges(ctx context.Context, organizationID string, changes []delivery.ReleaseChange) error {
	if s.changes == nil || len(changes) == 0 {
		return nil
	}
	bySource := map[string][]string{}
	for _, c := range changes {
		bySource[c.SourceID] = append(bySource[c.SourceID], c.CommitSHA)
	}
	attributed := map[string]*provenance.Change{}
	for sourceID, commitSHAs := range bySource {
		found, err := s.changes.ChangesForCommits(ctx, organizationID, sourceID, commitSHAs)
		if err != nil {
			return fmt.Errorf("read changes for source %s: %w", sourceID, err)
		}
		for _, change := range found {
			for _, sha := range change.CommitSHAs() {
				attributed[sourceID+"\x00"+sha] = change
			}
		}
	}
	for i := range changes {
		change := attributed[changes[i].SourceID+"\x00"+changes[i].CommitSHA]
		if change == nil {
			continue
		}
		id := change.ID()
		changes[i].ChangeID = &id
		changes[i].SessionID = change.SessionID()
		changes[i].WorkItemID = change.WorkItemID()
	}
	return nil
}

// commitRange reads the commits between two Builds of one Source. It
// returns why the range could not be listed, rather than an error, so one
// artifact never fails the whole read.
func (s *service) commitRange(ctx context.Context, organizationID string, previous, next *delivery.Build) ([]vcs.Commit, string) {
	p, n := previous.Snapshot(), next.Snapshot()
	switch {
	case n.SourceID == nil:
		return nil, "the Build has no Source"
	case p.SourceID == nil:
		return nil, "the previous Build has no Source"
	case *n.SourceID != *p.SourceID:
		return nil, "the Builds come from different Sources"
	case n.CommitSHA == nil:
		return nil, "the Build has no commit"
	case p.CommitSHA == nil:
		return nil, "the previous Build has no commit"
	}
	commits, err := s.history.CommitsBetween(ctx, organizationID, *n.SourceID, *p.CommitSHA, *n.CommitSHA)
	if err != nil {
		return nil, rangeUnavailable(err)
	}
	return commits, ""
}

// rangeUnavailable says why a range could not be read, without exposing
// provider internals.
func rangeUnavailable(err error) string {
	switch {
	case errors.Is(err, vcs.ErrSourceArchived):
		return "the Source is archived"
	case errors.Is(err, vcs.ErrConnectionInactive):
		return "the Source's connection is not active"
	case errors.Is(err, vcs.ErrLookupInDataPlane):
		return "lookups for the Source's connection run in the data plane"
	case errors.Is(err, vcs.ErrUnsupportedProvider):
		return "no version-control provider is configured for the Source"
	case errors.Is(err, vcs.ErrNotFound):
		return "the commit range could not be found"
	case errors.Is(err, vcs.ErrUnauthorized), errors.Is(err, vcs.ErrUnavailable):
		return "the version-control provider could not be reached"
	}
	return "the commit range could not be resolved"
}

func sourceOf(b *delivery.Build) string {
	if id := b.Snapshot().SourceID; id != nil {
		return *id
	}
	return ""
}

func changeOf(sourceID string, commit vcs.Commit) delivery.ReleaseChange {
	title, _, _ := strings.Cut(commit.Message, "\n")
	return delivery.ReleaseChange{
		CommitSHA:   commit.SHA,
		SourceID:    sourceID,
		Title:       strings.TrimSpace(title),
		Message:     commit.Message,
		AuthorName:  commit.AuthorName,
		AuthorEmail: commit.AuthorEmail,
		CommittedAt: commit.CommittedAt,
		URL:         commit.URL,
	}
}

// plan loads a Target and checks whether the Release may be deployed there.
type plan struct {
	target      *delivery.Target
	environment *delivery.Environment
	placement   *delivery.Placement
	checks      []delivery.DeploymentCheck
}

func (s *service) plan(ctx context.Context, organizationID string, rel *delivery.Release, targetID string) (*plan, error) {
	target, err := s.targets.GetTarget(ctx, organizationID, targetID)
	if err != nil {
		return nil, err
	}
	environment, err := s.environments.GetEnvironment(ctx, organizationID, target.EnvironmentID())
	if err != nil {
		return nil, err
	}
	app, err := s.applications.GetApplication(ctx, organizationID, rel.ApplicationID())
	if err != nil {
		return nil, err
	}
	p := &plan{target: target, environment: environment}
	p.checks = append(p.checks,
		check("release_published", rel.Status() == release.StatusPublished, "the release is not published"),
		check("target_active", !target.Archived(), "the target is archived"),
		check("environment_active", !environment.Archived(), "the environment is archived"),
		check("execution_enabled", environment.ExecutionEnabled(), "the environment does not execute deployments"),
		check("same_workspace", app.WorkspaceID() == environment.WorkspaceID(), "the target is in another workspace"),
	)
	placement, err := s.applications.GetPlacement(ctx, organizationID, rel.ApplicationID(), targetID)
	if errors.Is(err, delivery.ErrNotFound) {
		p.checks = append(p.checks,
			check("placement", false, "the application is not placed on the target"),
			check("apply_config", false, "the target has no apply config"),
		)
	} else if err != nil {
		return nil, err
	} else {
		p.placement = placement
		p.checks = append(p.checks,
			check("placement", true, ""),
			check("apply_config", placement.ApplyConfig() != nil, "the target has no apply config"),
		)
	}
	cluster, err := s.clusters.GetCluster(ctx, organizationID, target.ClusterID())
	if err != nil {
		return nil, err
	}
	p.checks = append(p.checks, check("agent_assigned", cluster.AgentPrincipalID() != nil, "the target's cluster has no agent assigned"))
	inFlight, err := s.deployments.GetInFlightDeployment(ctx, organizationID, rel.ApplicationID(), targetID)
	if errors.Is(err, delivery.ErrNotFound) {
		p.checks = append(p.checks, check("no_in_flight", true, ""))
	} else if err != nil {
		return nil, err
	} else {
		p.checks = append(p.checks, check("no_in_flight", false, "deployment "+inFlight.ID()+" is still in flight"))
	}
	channel, err := s.applications.GetChannel(ctx, organizationID, rel.ChannelID())
	if err != nil {
		return nil, err
	}
	lifecycle, err := s.lifecycles.GetLifecycle(ctx, organizationID, channel.LifecycleID())
	if err != nil {
		return nil, err
	}
	inLifecycle, phaseIndex := phaseOf(lifecycle, target.EnvironmentID())
	p.checks = append(p.checks, check("environment_in_lifecycle", inLifecycle, "the environment is not in the release channel's lifecycle"))
	open := false
	reason := "the environment is not in the release channel's lifecycle"
	if inLifecycle {
		succeeded, err := s.deployments.ListSucceededEnvironmentIDs(ctx, organizationID, rel.ID())
		if err != nil {
			return nil, err
		}
		open, reason = phaseOpen(lifecycle, phaseIndex, succeeded)
	}
	p.checks = append(p.checks, check("phase_open", open, reason))
	return p, nil
}

func phaseOf(l *delivery.Lifecycle, environmentID string) (bool, int) {
	for i, phase := range l.Phases() {
		for _, id := range phase.EnvironmentIDs {
			if id == environmentID {
				return true, i
			}
		}
	}
	return false, -1
}

func phaseOpen(l *delivery.Lifecycle, index int, succeeded []string) (bool, string) {
	if index < 0 {
		return false, "the environment is not in the release channel's lifecycle"
	}
	done := map[string]bool{}
	for _, id := range succeeded {
		done[id] = true
	}
	phases := l.Phases()
	for i := 0; i < index; i++ {
		phase := phases[i]
		if phase.Optional {
			continue
		}
		required := len(phase.EnvironmentIDs)
		if phase.MinEnvironments != nil {
			required = *phase.MinEnvironments
		}
		count := 0
		for _, id := range phase.EnvironmentIDs {
			if done[id] {
				count++
			}
		}
		if count < required {
			return false, fmt.Sprintf("phase %q is not open for the release", phase.Name)
		}
	}
	return true, ""
}

func check(name string, ok bool, reason string) delivery.DeploymentCheck {
	if ok {
		reason = ""
	}
	return delivery.DeploymentCheck{Name: name, OK: ok, Reason: reason}
}

func firstFailure(checks []delivery.DeploymentCheck) error {
	for _, c := range checks {
		if c.OK {
			continue
		}
		switch c.Name {
		case "no_in_flight":
			return fmt.Errorf("%w: %s", delivery.ErrDeploymentInFlight, c.Reason)
		case "release_published":
			return fmt.Errorf("%w: %s", delivery.ErrReleaseStatus, c.Reason)
		case "target_active", "environment_active":
			return fmt.Errorf("%w: %s", delivery.ErrArchived, c.Reason)
		default:
			return delivery.NewErrInvalidInput(c.Reason)
		}
	}
	return nil
}

func (s *service) assignment(ctx context.Context, d *delivery.Deployment) (*delivery.DeploymentAssignment, error) {
	rel, err := s.releases.GetRelease(ctx, d.OrganizationID(), d.ReleaseID())
	if err != nil {
		return nil, err
	}
	target, err := s.targets.GetTarget(ctx, d.OrganizationID(), d.TargetID())
	if err != nil {
		return nil, err
	}
	env, err := s.environments.GetEnvironment(ctx, d.OrganizationID(), d.EnvironmentID())
	if err != nil {
		return nil, err
	}
	placement, err := s.applications.GetPlacement(ctx, d.OrganizationID(), d.ApplicationID(), d.TargetID())
	if err != nil {
		return nil, err
	}
	artifacts, err := s.applications.ListArtifacts(ctx, d.OrganizationID(), d.ApplicationID(), true)
	if err != nil {
		return nil, err
	}
	byArtifact := map[string]*delivery.Artifact{}
	for _, a := range artifacts {
		byArtifact[a.ID()] = a
	}
	builds, err := s.buildsOf(ctx, rel)
	if err != nil {
		return nil, err
	}
	out := &delivery.DeploymentAssignment{
		Deployment: d.Snapshot(), Release: rel.Snapshot(), Target: target.Snapshot(),
		Environment: env.Snapshot(), ApplyConfig: placement.ApplyConfig(), HelmReleaseName: helmReleaseName(d.ApplicationID()),
	}
	for _, rb := range rel.Builds() {
		a, b := byArtifact[rb.ArtifactID], builds[rb.ArtifactID]
		if a == nil || b == nil {
			continue
		}
		out.Artifacts = append(out.Artifacts, delivery.AssignmentArtifact{
			ArtifactID: a.ID(), ArtifactName: a.Name(), Kind: a.ArtifactKind(),
			BuildID: b.ID(), PublishedAs: b.ArtifactName(), Digest: b.Digest(),
		})
	}
	return out, nil
}

func helmReleaseName(applicationID string) string {
	return "ivaldi-" + strings.ReplaceAll(strings.ToLower(applicationID), "_", "-")
}

func (s *service) buildsOf(ctx context.Context, rel *delivery.Release) (map[string]*delivery.Build, error) {
	builds, err := s.builds.GetBuilds(ctx, rel.OrganizationID(), rel.BuildIDs())
	if err != nil {
		return nil, err
	}
	byID := map[string]*delivery.Build{}
	for _, b := range builds {
		byID[b.ID()] = b
	}
	out := map[string]*delivery.Build{}
	for _, rb := range rel.Builds() {
		if b, ok := byID[rb.BuildID]; ok {
			out[rb.ArtifactID] = b
		}
	}
	return out, nil
}

func (s *service) applyCurrent(ctx context.Context, d *delivery.Deployment) error {
	status := d.Status()
	if status != release.DeploymentHealthy && status != release.DeploymentDegraded {
		return nil
	}
	placement, err := s.applications.GetPlacement(ctx, d.OrganizationID(), d.ApplicationID(), d.TargetID())
	if err != nil {
		return err
	}
	if placement.CurrentDeploymentID() != nil && *placement.CurrentDeploymentID() != d.ID() {
		previous, err := s.deployments.GetDeploymentForUpdate(ctx, d.OrganizationID(), *placement.CurrentDeploymentID())
		if err != nil {
			return err
		}
		if previous.Status() != release.DeploymentSuperseded {
			if err := previous.MoveTo(release.DeploymentSuperseded); err != nil {
				return err
			}
			if err := s.deployments.UpdateDeployment(ctx, previous); err != nil {
				return err
			}
			if err := s.appendStatusEvent(ctx, previous, nil); err != nil {
				return err
			}
			if err := s.writer.Events(ctx, delivery.AggregateDeployment, previous.ID(), previous); err != nil {
				return err
			}
		}
	}
	if err := placement.SetCurrent(d.ID(), d.ReleaseID()); err != nil {
		return err
	}
	return s.applications.UpdatePlacementCurrent(ctx, placement)
}

func (s *service) appendStatusEvent(ctx context.Context, d *delivery.Deployment, reportedBy *string) error {
	status := d.Status()
	event, err := delivery.NewDeploymentEvent(delivery.NewDeploymentEventProps{
		OrganizationID: d.OrganizationID(), DeploymentID: d.ID(), Kind: delivery.DeploymentEventStatusChanged,
		Status: &status, ReportedByPrincipalID: reportedBy,
	}, nil)
	if err != nil {
		return err
	}
	_, err = s.deployments.AppendDeploymentEvent(ctx, event)
	return err
}

func (s *service) detail(ctx context.Context, d *delivery.Deployment) (*delivery.DeploymentDetail, error) {
	events, err := s.deployments.ListDeploymentEvents(ctx, d.OrganizationID(), d.ID())
	if err != nil {
		return nil, err
	}
	snapshots := make([]delivery.DeploymentEventSnapshot, len(events))
	for i, e := range events {
		snapshots[i] = e.Snapshot()
	}
	return &delivery.DeploymentDetail{Deployment: d.Snapshot(), Events: snapshots}, nil
}
