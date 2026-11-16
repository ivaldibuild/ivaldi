// Package deployments implements delivery.DeploymentService: requesting,
// tracking, cancelling, and reporting Deployments. The domain types stay in
// delivery; this package orchestrates them.
package deployments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/provenance"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/binding"
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
	variables    Variables
	snapshots    VariableSnapshots
	stores       delivery.SecretStores
	history      vcs.History
	changes      provenance.Lookup
	authz        authorization.Authorizer
	writer       change.Writer
	// heartbeat is how often a Cluster's agent must report; capabilities go
	// stale after three intervals, the same rule as a runner going offline.
	heartbeat time.Duration
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

// WithVariables reads the revisions of Releases' variables Deployments use.
func WithVariables(r Variables) ServiceOption {
	return func(o *options) { o.variables = r }
}

// WithSecretStores looks up the Secret Stores a Deployment's secrets are in.
func WithSecretStores(s delivery.SecretStores) ServiceOption {
	return func(o *options) { o.stores = s }
}

// WithVariableSnapshots snapshots the variables of a Release Ivaldi observed.
func WithVariableSnapshots(v VariableSnapshots) ServiceOption {
	return func(o *options) { o.snapshots = v }
}
func WithClusters(r Clusters) ServiceOption {
	return func(o *options) { o.clusters = r }
}

// WithHeartbeatInterval sets how long a Cluster's reported capabilities stay
// current: three intervals. It is runners.heartbeat_interval, which startup
// validation requires to be positive.
func WithHeartbeatInterval(d time.Duration) ServiceOption {
	return func(o *options) { o.heartbeat = d }
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
		{"Variables", o.variables != nil},
		{"VariableSnapshots", o.snapshots != nil},
		{"SecretStores", o.stores != nil},
		{"HeartbeatInterval", o.heartbeat > 0},
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
				Source: delivery.DeploymentExecuted, Reason: plan.reason, Status: status,
				PreviousDeploymentID:   plan.placement.CurrentDeploymentID(),
				InitiatedByPrincipalID: optional.Of(actor.PrincipalID), SessionID: in.SessionID,
				WorkItemID: in.WorkItemID, PolicyDecisionID: optional.Of(decision.ID),
				RequestedAt: &now,
			}
			if plan.variables != nil {
				props.VariableSnapshotID = new(plan.variables.ID())
			}
			if plan.restores != nil {
				props.RestoresDeploymentID = new(plan.restores.ID())
				props.SecretsLatest = in.SecretsLatest
			}
			if status == release.DeploymentApplying {
				props.StartedAt = &now
			}
			d, err := delivery.NewDeployment(props, nil)
			if err != nil {
				return err
			}
			if err := s.fixSecretPaths(ctx, d, rel, plan); err != nil {
				return err
			}
			if err := s.deployments.CreateDeployment(ctx, d); err != nil {
				return err
			}
			if err := s.appendStatusEvent(ctx, d, nil, nil, nil); err != nil {
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
		if err := s.appendStatusEvent(ctx, d, nil, nil, nil); err != nil {
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

// ListClusterAssignments returns the active Deployments on any of the
// Cluster's Targets. Only the Cluster's assigned agent may call it.
func (s *service) ListClusterAssignments(ctx context.Context, actor identity.Actor, organizationID, clusterID string) ([]delivery.DeploymentAssignment, error) {
	cluster, err := s.clusters.GetCluster(ctx, organizationID, clusterID)
	if err != nil {
		return nil, err
	}
	if err := requireClusterAgent(cluster, actor.PrincipalID); err != nil {
		return nil, err
	}
	// The statuses are filtered in the query, not after it, so finished
	// history never pushes in-flight work off the page, and the Cluster's
	// Targets are matched in the same query: an agent polls this every few
	// seconds, so it costs one query however many Targets the Cluster has.
	query := delivery.ListDeploymentsQuery{
		OrganizationID: organizationID, ClusterID: clusterID,
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
			var unassignable *delivery.ErrInvalidInput
			if errors.As(err, &unassignable) {
				// One Deployment that can no longer be applied, such as one
				// whose placement lost its apply config mid-flight, must not
				// cut the agent off from every other Target: it fails, and
				// the rest are returned.
				if err := s.failUnassignable(ctx, d.OrganizationID(), d.ID(), unassignable.Message); err != nil {
					return nil, err
				}
				continue
			}
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

// failUnassignable fails an in-flight Deployment whose assignment cannot be
// built, recording why on its timeline. Ivaldi itself is the actor: the agent
// only asked for its work. A Deployment that finished meanwhile is left alone.
func (s *service) failUnassignable(ctx context.Context, organizationID, deploymentID, reason string) error {
	decision := authorization.Decision{
		ID: authorization.NewDecisionID(), Outcome: authorization.OutcomeAllow,
		Reason: "the deployment can no longer be applied",
	}
	ctx = authorization.WithDecision(ctx, "", decision)
	return s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		d, err := s.deployments.GetDeploymentForUpdate(ctx, organizationID, deploymentID)
		if err != nil {
			return err
		}
		if d.Status() != release.DeploymentApplying && d.Status() != release.DeploymentProgressing {
			return nil
		}
		before := d.Snapshot()
		if err := d.MoveTo(release.DeploymentFailed); err != nil {
			return err
		}
		if err := s.deployments.UpdateDeployment(ctx, d); err != nil {
			return err
		}
		detail, err := json.Marshal(struct {
			Reason string `json:"reason"`
		}{reason})
		if err != nil {
			return fmt.Errorf("encode failure reason: %w", err)
		}
		if err := s.appendStatusEvent(ctx, d, nil, nil, detail); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, delivery.AggregateDeployment, d.ID(), d); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: delivery.EventDeploymentFailed, OrganizationID: organizationID, ResourceID: d.ID(),
			Actor: identity.Actor{Kind: identity.ActorSystem}, Before: before, After: d.Snapshot(),
		})
	})
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
		if err := s.recordSecrets(ctx, d, in.Detail); err != nil {
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

// checkTargetAgent refuses anyone but the agent of the Target's Cluster.
// recordSecrets stores which version of each secret the agent says it
// resolved, with the path it read it from and a fingerprint, beside where the
// Deployment's revision says the secret is kept. A report without secrets
// changes nothing.
func (s *service) recordSecrets(ctx context.Context, d *delivery.Deployment, detail json.RawMessage) error {
	var reported struct {
		Secrets []struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			Version     string `json:"version"`
			Fingerprint string `json:"fingerprint"`
			DeliveredAs string `json:"delivered_as"`
		} `json:"secrets"`
		// The secrets an overlay bound, whose values went into the chart's
		// values however the variable asks to be delivered.
		SecretsInValues []string `json:"secrets_in_values"`
	}
	// The detail is the agent's to shape; one that is not an object simply
	// names no secrets.
	if len(detail) == 0 || json.Unmarshal(detail, &reported) != nil || len(reported.Secrets) == 0 {
		return nil
	}
	references := map[string]delivery.SecretReference{}
	paths := map[string]string{}
	if snapshotID := d.VariableSnapshotID(); snapshotID != nil {
		revision, err := s.variables.GetReleaseVariables(ctx, d.OrganizationID(), *snapshotID)
		if err != nil {
			return err
		}
		rel, err := s.releases.GetRelease(ctx, d.OrganizationID(), d.ReleaseID())
		if err != nil {
			return err
		}
		target, err := s.targets.GetTarget(ctx, d.OrganizationID(), d.TargetID())
		if err != nil {
			return err
		}
		env, err := s.environments.GetEnvironment(ctx, d.OrganizationID(), d.EnvironmentID())
		if err != nil {
			return err
		}
		resolved := s.resolve(revision, rel, target)
		for _, r := range resolved {
			if r.Winner != nil && r.Winner.Secret != nil {
				references[r.Name] = *r.Winner.Secret
			}
		}
		if paths, err = s.pathsOf(ctx, d, rel, target, env, resolved); err != nil {
			return err
		}
	}
	versions := make([]delivery.SecretVersion, 0, len(reported.Secrets))
	for _, secret := range reported.Secrets {
		ref, sent := references[secret.Name]
		if !sent {
			return delivery.NewErrInvalidInput(fmt.Sprintf("the report names secret %q, which the deployment was not sent", secret.Name))
		}
		// The agent reads the path its assignment gives, which the control
		// plane filled in; one that read another is refused rather than
		// recorded. An agent of an earlier version names none.
		path := paths[secret.Name]
		if secret.Path != "" && secret.Path != path {
			return delivery.NewErrInvalidInput(fmt.Sprintf(
				"the agent read secret %s from %q, not from %q, the path the deployment was sent", secret.Name, secret.Path, path))
		}
		deliveredAs := delivery.VariableDeliverAsSecret
		if secret.DeliveredAs == string(delivery.VariableDeliverAsValue) || slices.Contains(reported.SecretsInValues, secret.Name) {
			deliveredAs = delivery.VariableDeliverAsValue
		}
		versions = append(versions, delivery.SecretVersion{
			Name: secret.Name, StoreID: ref.StoreID, Path: path, Field: ref.Field,
			Version: secret.Version, Fingerprint: secret.Fingerprint, DeliveredAs: deliveredAs,
		})
	}
	return d.RecordSecretVersions(versions)
}

// storeKinds returns the kinds of the stores, for a check that does not
// concern what the agent can read.
func storeKinds(stores map[string]delivery.SecretStoreRef) []string {
	var out []string
	for _, store := range stores {
		out = append(out, string(store.Settings.Kind))
	}
	return out
}

func (s *service) checkTargetAgent(ctx context.Context, organizationID string, target *delivery.Target, principalID string) error {
	cluster, err := s.clusters.GetCluster(ctx, organizationID, target.ClusterID())
	if err != nil {
		return err
	}
	return requireClusterAgent(cluster, principalID)
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
	return requireClusterAgent(cluster, principalID)
}

func requireClusterAgent(cluster *delivery.Cluster, principalID string) error {
	agent := cluster.AgentPrincipalID()
	if agent == nil || *agent != principalID {
		return fmt.Errorf("%w: %s does not serve cluster %s", delivery.ErrNotDeploymentAgent, principalID, cluster.ID())
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
		ReleaseID: rel.ID(), TargetID: targetID, Reason: plan.reason,
		Artifacts: []delivery.ArtifactChange{}, Changes: []delivery.ReleaseChange{},
		RemovedChanges: []delivery.ReleaseChange{},
	}
	if plan.restores != nil {
		changes.RestoresDeploymentID = new(plan.restores.ID())
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
	added, removed := commitList{}, commitList{}
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
			ArtifactID:        rb.ArtifactID,
			ArtifactName:      artifact.Name(),
			Next:              b.Snapshot(),
			CommitSHAs:        []string{},
			RemovedCommitSHAs: []string{},
			Unresolved:        []string{},
		}
		prev := previous[rb.ArtifactID]
		if prev != nil {
			snap := prev.Snapshot()
			item.Previous = &snap
		}
		if prev == nil {
			item.Unresolved = append(item.Unresolved, "the artifact is new on this Target")
		} else if prev.Digest() != b.Digest() {
			comparison, reason := s.commitRange(ctx, organizationID, prev, b)
			if reason != "" {
				item.Unresolved = append(item.Unresolved, reason)
			}
			sourceID := sourceOf(b)
			item.CommitSHAs = added.add(sourceID, comparison.Added)
			item.RemovedCommitSHAs = removed.add(sourceID, comparison.Removed)
		}
		changes.Artifacts = append(changes.Artifacts, item)
	}
	changes.Changes = added.sorted()
	changes.RemovedChanges = removed.sorted()
	if err := s.attributeChanges(ctx, organizationID, changes.Changes, changes.RemovedChanges); err != nil {
		return nil, err
	}
	changes.Checks = plan.checks
	return changes, nil
}

// attributeChanges fills, in place, each listed commit's recorded Change,
// Session, and Work Item. Without a provenance lookup, commits stay
// unattributed.
func (s *service) attributeChanges(ctx context.Context, organizationID string, lists ...[]delivery.ReleaseChange) error {
	if s.changes == nil {
		return nil
	}
	bySource := map[string][]string{}
	for _, changes := range lists {
		for _, c := range changes {
			bySource[c.SourceID] = append(bySource[c.SourceID], c.CommitSHA)
		}
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
	for _, changes := range lists {
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
	}
	return nil
}

// commitList gathers the commits several artifacts add or remove, once
// each, in the order release notes list them.
type commitList struct {
	seen    map[string]bool
	changes []delivery.ReleaseChange
}

// add records commits from a Source and returns their SHAs, oldest first.
func (l *commitList) add(sourceID string, commits []vcs.Commit) []string {
	if l.seen == nil {
		l.seen = map[string]bool{}
	}
	shas := make([]string, 0, len(commits))
	for _, commit := range commits {
		shas = append(shas, commit.SHA)
		key := sourceID + "\x00" + commit.SHA
		if l.seen[key] {
			continue
		}
		l.seen[key] = true
		l.changes = append(l.changes, changeOf(sourceID, commit))
	}
	return shas
}

// sorted returns the commits by commit time, the SHA breaking ties.
func (l *commitList) sorted() []delivery.ReleaseChange {
	out := append([]delivery.ReleaseChange{}, l.changes...)
	slices.SortFunc(out, func(a, b delivery.ReleaseChange) int {
		if !a.CommittedAt.Equal(b.CommittedAt) {
			return a.CommittedAt.Compare(b.CommittedAt)
		}
		return strings.Compare(a.CommitSHA, b.CommitSHA)
	})
	return out
}

// commitRange compares two Builds of one Source. It returns why they could
// not be compared, rather than an error, so one artifact never fails the
// whole read.
func (s *service) commitRange(ctx context.Context, organizationID string, previous, next *delivery.Build) (vcs.Comparison, string) {
	p, n := previous.Snapshot(), next.Snapshot()
	switch {
	case n.SourceID == nil:
		return vcs.Comparison{}, "the Build has no Source"
	case p.SourceID == nil:
		return vcs.Comparison{}, "the previous Build has no Source"
	case *n.SourceID != *p.SourceID:
		return vcs.Comparison{}, "the Builds come from different Sources"
	case n.CommitSHA == nil:
		return vcs.Comparison{}, "the Build has no commit"
	case p.CommitSHA == nil:
		return vcs.Comparison{}, "the previous Build has no commit"
	}
	comparison, err := s.history.Compare(ctx, organizationID, *n.SourceID, *p.CommitSHA, *n.CommitSHA)
	if err != nil {
		return vcs.Comparison{}, rangeUnavailable(err)
	}
	return comparison, ""
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
	case errors.Is(err, vcs.ErrRangeTooLarge):
		return "the Builds are too many commits apart to list"
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
	// variables is the revision of the Release's variables a Deployment
	// would use; nil for a Release that has none.
	variables *delivery.ReleaseVariables
	// reason is what a Deployment requested now would be, and restores the
	// executed Deployment a rollback replays, if one was recorded.
	reason   delivery.DeploymentReason
	restores *delivery.Deployment
	checks   []delivery.DeploymentCheck
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
	// Without an agent, agent_assigned already says why; agent_capable would
	// only fail again for the same cause.
	if cluster.AgentPrincipalID() != nil {
		// Only a heartbeat refreshes capabilities, so a Cluster whose agent
		// renews or reports but stops heartbeating is in contact yet stale.
		capable, capableReason := false, "the cluster's agent has not reported recently"
		if reported := cluster.LastReportedAt(); reported != nil && !clock.Now().After(reported.Add(3*s.heartbeat)) {
			capable = target.CanExecute()
			if capable {
				capableReason = ""
			} else {
				capableReason = "the cluster's agent cannot execute in this namespace"
			}
		}
		p.checks = append(p.checks, check("agent_capable", capable, capableReason))
	}
	inFlight, err := s.deployments.GetInFlightDeployment(ctx, organizationID, rel.ApplicationID(), targetID)
	if errors.Is(err, delivery.ErrNotFound) {
		p.checks = append(p.checks, check("no_in_flight", true, ""))
	} else if err != nil {
		return nil, err
	} else {
		p.checks = append(p.checks, check("no_in_flight", false, "deployment "+inFlight.ID()+" is still in flight"))
	}
	position, err := s.lifecyclePosition(ctx, rel, target.EnvironmentID())
	if err != nil {
		return nil, err
	}
	p.checks = append(p.checks,
		check("environment_in_lifecycle", position.inLifecycle, "the environment is not in the release channel's lifecycle"),
		check("phase_open", position.open, position.reason))

	p.reason, p.restores, err = s.reasonFor(ctx, rel, p.placement, targetID)
	if err != nil {
		return nil, err
	}
	p.variables, err = s.variablesFor(ctx, rel, p.restores)
	if err != nil {
		return nil, err
	}
	resolved := s.resolve(p.variables, rel, target)
	if err := refusal(&p.checks, "variables_defined", delivery.CheckBindings(resolved)); err != nil {
		return nil, err
	}
	stores, err := s.stores.LookupSecretStores(ctx, organizationID, delivery.SecretStoreIDs(resolved))
	if err != nil {
		return nil, fmt.Errorf("look up secret stores: %w", err)
	}
	allowed := delivery.CheckSecretStores(resolved, stores, target.EnvironmentID(), agentStoreKinds(cluster))
	if err := refusal(&p.checks, "secret_stores_allowed", allowed); err != nil {
		return nil, err
	}
	return p, nil
}

// refusal adds a check that passes unless err says the input is invalid,
// whose message is the reason. Any other error is returned.
func refusal(checks *[]delivery.DeploymentCheck, name string, err error) error {
	var invalid *delivery.ErrInvalidInput
	switch {
	case err == nil:
		*checks = append(*checks, check(name, true, ""))
	case errors.As(err, &invalid):
		*checks = append(*checks, check(name, false, invalid.Message))
	default:
		return err
	}
	return nil
}

// agentStoreKinds returns the secret-store kinds the Cluster's agent last
// said it can read.
func agentStoreKinds(cluster *delivery.Cluster) []string {
	var out []string
	switch kinds := cluster.AgentCapabilities()["secret_store_kinds"].(type) {
	case []string:
		out = kinds
	case []any:
		for _, k := range kinds {
			if s, ok := k.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// reasonFor says what a Deployment of the Release to the Target would be: a
// redeploy when the Release is current there, a rollback when it was
// healthy there before, and a rollout otherwise. For a rollback it returns
// the Deployment to replay: the Release's last healthy Deployment there, if
// Ivaldi executed it. One it only observed recorded nothing to replay.
func (s *service) reasonFor(ctx context.Context, rel *delivery.Release, placement *delivery.Placement, targetID string) (delivery.DeploymentReason, *delivery.Deployment, error) {
	if placement == nil {
		return delivery.DeploymentRollout, nil, nil
	}
	if current := placement.CurrentReleaseID(); current != nil && *current == rel.ID() {
		return delivery.DeploymentRedeploy, nil, nil
	}
	last, err := s.deployments.GetLastSucceededDeployment(ctx, rel.OrganizationID(), rel.ID(), targetID)
	if errors.Is(err, delivery.ErrNotFound) {
		return delivery.DeploymentRollout, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if last.Source() != delivery.DeploymentExecuted {
		return delivery.DeploymentRollback, nil, nil
	}
	return delivery.DeploymentRollback, last, nil
}

// variablesFor returns the revision of variables a Deployment would use:
// the one the Deployment it restores used, so a rollback brings back the
// configuration the Release last ran with there, or else the Release's
// current one. Either may be nil: there were no variables to record.
func (s *service) variablesFor(ctx context.Context, rel *delivery.Release, restores *delivery.Deployment) (*delivery.ReleaseVariables, error) {
	if restores == nil {
		return s.currentVariables(ctx, rel)
	}
	snapshotID := restores.VariableSnapshotID()
	if snapshotID == nil {
		return nil, nil
	}
	return s.variables.GetReleaseVariables(ctx, rel.OrganizationID(), *snapshotID)
}

// currentVariables returns the revision of its variables a Release deploys
// with now, or nil for a Release created before they were snapshotted.
func (s *service) currentVariables(ctx context.Context, rel *delivery.Release) (*delivery.ReleaseVariables, error) {
	current, err := s.variables.GetCurrentReleaseVariables(ctx, rel.OrganizationID(), rel.ID())
	if errors.Is(err, delivery.ErrNotFound) {
		return nil, nil
	}
	return current, err
}

// resolve picks the value of each variable in a revision for a Deployment
// of the Release to the Target.
func (s *service) resolve(revision *delivery.ReleaseVariables, rel *delivery.Release, target *delivery.Target) []delivery.ResolvedVariable {
	if revision == nil {
		return nil
	}
	return delivery.ResolveVariables(revision.Variables(), delivery.DeploymentScope{
		TargetID: target.ID(), EnvironmentID: target.EnvironmentID(), ChannelID: rel.ChannelID(),
	})
}

// lifecycle is where an Environment stands in a Release's Lifecycle.
type lifecycle struct {
	inLifecycle bool
	// open says whether every earlier required phase has succeeded for the
	// Release; reason says why not.
	open   bool
	reason string
}

// lifecyclePosition checks an Environment against the Lifecycle of the
// Release's Channel: the deploy plan refuses what fails, and an observation
// records it as findings.
func (s *service) lifecyclePosition(ctx context.Context, rel *delivery.Release, environmentID string) (lifecycle, error) {
	channel, err := s.applications.GetChannel(ctx, rel.OrganizationID(), rel.ChannelID())
	if err != nil {
		return lifecycle{}, err
	}
	l, err := s.lifecycles.GetLifecycle(ctx, rel.OrganizationID(), channel.LifecycleID())
	if err != nil {
		return lifecycle{}, err
	}
	inLifecycle, phaseIndex := phaseOf(l, environmentID)
	if !inLifecycle {
		return lifecycle{reason: "the environment is not in the release channel's lifecycle"}, nil
	}
	succeeded, err := s.deployments.ListSucceededEnvironmentIDs(ctx, rel.OrganizationID(), rel.ID())
	if err != nil {
		return lifecycle{}, err
	}
	open, reason := phaseOpen(l, phaseIndex, succeeded)
	return lifecycle{inLifecycle: true, open: open, reason: reason}, nil
}

// findings are the Lifecycle rules an observed Deployment broke. Policy's
// findings wait until policy evaluates observed Deployments.
func (l lifecycle) findings() []delivery.Finding {
	switch {
	case !l.inLifecycle:
		return []delivery.Finding{delivery.FindingOutsideLifecycle}
	case !l.open:
		return []delivery.Finding{delivery.FindingPhaseNotOpen}
	}
	return []delivery.Finding{}
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
	config := placement.ApplyConfig()
	if config == nil {
		return nil, delivery.NewErrInvalidInput("the target has no apply config; deployments there are tracked only")
	}
	restored, err := s.restoredPins(ctx, d)
	if err != nil {
		return nil, err
	}
	variables, secrets := []delivery.AssignmentVariable{}, []delivery.AssignmentSecret{}
	if snapshotID := d.VariableSnapshotID(); snapshotID != nil {
		revision, err := s.variables.GetReleaseVariables(ctx, d.OrganizationID(), *snapshotID)
		if err != nil {
			return nil, err
		}
		resolved := s.resolve(revision, rel, target)
		variables = delivery.AssignmentVariables(resolved)
		stores, err := s.stores.LookupSecretStores(ctx, d.OrganizationID(), delivery.SecretStoreIDs(resolved))
		if err != nil {
			return nil, fmt.Errorf("look up secret stores: %w", err)
		}
		// A store archived or narrowed since the request would leave the
		// agent without a way to read the secret. Once applied, the agent
		// only watches the Deployment's health and reads no secret.
		if d.Status() == release.DeploymentApplying {
			if err := delivery.CheckSecretStores(resolved, stores, d.EnvironmentID(), storeKinds(stores)); err != nil {
				return nil, err
			}
		}
		secrets = delivery.AssignmentSecrets(resolved, stores)
		// The agent reads the path given, filled in here so the control
		// plane knows exactly what it reads, and escaped so the agent's own
		// filling in leaves it as it is.
		paths, err := s.pathsOf(ctx, d, rel, target, env, resolved)
		if err != nil {
			return nil, err
		}
		for i := range secrets {
			secrets[i].Ref.Path = binding.Escape(paths[secrets[i].Name])
		}
	}
	// A rollback reads the versions the Deployment it restores read, and
	// the agent refuses a secret whose value is no longer that one.
	pins := map[string]delivery.SecretVersion{}
	for _, pin := range restored {
		pins[pin.Name] = pin
	}
	for i := range secrets {
		if pin, ok := pins[secrets[i].Name]; ok {
			if pin.Version != "" {
				secrets[i].Ref.Version = pin.Version
			}
			secrets[i].Fingerprint = pin.Fingerprint
		}
	}
	// A rollback asked to use the latest secrets reads the latest of each,
	// including one whose value names a version: that version is what the
	// rollback could not read, or read as something else.
	if d.RestoresDeploymentID() != nil && d.SecretsLatest() {
		for i := range secrets {
			secrets[i].Ref.Version = ""
		}
	}
	apply := delivery.ApplyAssignment{
		Deployer:  delivery.DeployerHelm,
		Namespace: target.Locator()[delivery.LocatorNamespace],
		Overlays:  config.Values,
		Images:    map[string]delivery.AssignmentImage{},
		Variables: variables,
		Secrets:   secrets,
		Helm:      &delivery.HelmApply{ReleaseName: helmReleaseName(d.ApplicationID())},
		Reason:    d.Reason(),
		Restores:  d.RestoresDeploymentID(),
	}
	if apply.Overlays == nil {
		apply.Overlays = []string{}
	}
	found := false
	for _, rb := range rel.Builds() {
		a, b := byArtifact[rb.ArtifactID], builds[rb.ArtifactID]
		if a == nil || b == nil {
			continue
		}
		// Helm applies a chart and the images it runs; any other artifact
		// kind, such as an archive, has no part in a Helm apply.
		switch a.ArtifactKind() {
		case delivery.ArtifactHelmChart:
			if a.Name() == config.DefinitionArtifact {
				apply.Definition = delivery.AssignmentArtifact{
					ArtifactID: a.ID(), ArtifactName: a.Name(), Kind: a.ArtifactKind(),
					BuildID: b.ID(), PublishedAs: b.ArtifactName(), Digest: b.Digest(),
				}
				found = true
			}
		case delivery.ArtifactContainerImage:
			apply.Images[a.Name()] = delivery.AssignmentImage{
				ArtifactID: a.ID(), Repository: b.ArtifactName(), Digest: b.Digest(),
				Image: b.ArtifactName() + "@" + b.Digest(),
			}
		}
	}
	if !found {
		return nil, delivery.NewErrInvalidInput(fmt.Sprintf("the release has no %q artifact to apply", config.DefinitionArtifact))
	}
	return &delivery.DeploymentAssignment{
		Deployment: d.Snapshot(), Release: rel.Snapshot(), Target: target.Snapshot(),
		Environment: env.Snapshot(), Apply: apply,
	}, nil
}

// restoredPins returns what the Deployment a rollback restores read, which the
// rollback reads again: nil for any other Deployment, and for a rollback asked
// to use the latest secrets.
func (s *service) restoredPins(ctx context.Context, d *delivery.Deployment) ([]delivery.SecretVersion, error) {
	restoresID := d.RestoresDeploymentID()
	if restoresID == nil || d.SecretsLatest() {
		return nil, nil
	}
	restored, err := s.deployments.GetDeployment(ctx, d.OrganizationID(), *restoresID)
	if err != nil {
		return nil, err
	}
	return restored.SecretVersions(), nil
}

// fixSecretPaths fixes where a Deployment being requested reads its secrets
// from: each path filled in with its own values, or for a rollback the path
// the Deployment it restores read. Fixed now, the agent's report is checked
// against what was requested, whatever is renamed before it arrives.
func (s *service) fixSecretPaths(ctx context.Context, d *delivery.Deployment, rel *delivery.Release, p *plan) error {
	if p.variables == nil {
		return nil
	}
	var restored []delivery.SecretVersion
	if p.restores != nil && !d.SecretsLatest() {
		restored = p.restores.SecretVersions()
	}
	paths, err := s.secretPaths(ctx, d, rel, p.target, p.environment, s.resolve(p.variables, rel, p.target), restored)
	if err != nil {
		return err
	}
	return d.ReadSecretsFrom(paths)
}

// pathsOf returns where d reads its secrets from: the paths fixed when it was
// requested, or, for a Deployment requested before they were fixed, the
// paths filled in now.
func (s *service) pathsOf(ctx context.Context, d *delivery.Deployment, rel *delivery.Release, target *delivery.Target, env *delivery.Environment, resolved []delivery.ResolvedVariable) (map[string]string, error) {
	if paths := d.SecretPaths(); paths != nil {
		return paths, nil
	}
	restored, err := s.restoredPins(ctx, d)
	if err != nil {
		return nil, err
	}
	return s.secretPaths(ctx, d, rel, target, env, resolved, restored)
}

// secretPaths returns the paths d's secrets are read from, filled in with
// d's system variables and text variables, or replayed from restored.
func (s *service) secretPaths(ctx context.Context, d *delivery.Deployment, rel *delivery.Release, target *delivery.Target, env *delivery.Environment, resolved []delivery.ResolvedVariable, restored []delivery.SecretVersion) (map[string]string, error) {
	cluster, err := s.clusters.GetCluster(ctx, d.OrganizationID(), target.ClusterID())
	if err != nil {
		return nil, err
	}
	paths, err := delivery.SecretPaths(resolved, delivery.DeploymentSystem(d, rel, target, env, cluster.Slug()), restored)
	if err != nil {
		return nil, fmt.Errorf("fill in the secrets' paths: %w", err)
	}
	return paths, nil
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
	return s.setCurrent(ctx, placement, d)
}

// setCurrent supersedes the placement's previous Deployment and makes d the
// one running there. It is shared by executed and observed Deployments.
func (s *service) setCurrent(ctx context.Context, placement *delivery.Placement, d *delivery.Deployment) error {
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
			if err := s.appendStatusEvent(ctx, previous, nil, nil, nil); err != nil {
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

// Observe records what a Cluster's agent sees running on a Target. When the
// images match the placement's current Release, it changes nothing and
// returns the current Deployment; otherwise it records the missing artifacts
// and Builds, an observed Release, and an observed Deployment, then makes it
// current.
func (s *service) Observe(ctx context.Context, actor identity.Actor, in delivery.ObserveInput) (*delivery.DeploymentDetail, error) {
	if len(in.Artifacts) == 0 {
		return nil, delivery.NewErrInvalidInput("an observation needs at least one image")
	}
	seen := map[string]string{}
	for _, a := range in.Artifacts {
		if a.ArtifactName == "" {
			return nil, delivery.NewErrInvalidInput("every observed image needs an artifact name")
		}
		if err := delivery.ValidateDigest(a.Digest); err != nil {
			return nil, err
		}
		if _, ok := seen[a.ArtifactName]; ok {
			return nil, delivery.NewErrInvalidInput(fmt.Sprintf("artifact %s is reported twice", a.ArtifactName))
		}
		seen[a.ArtifactName] = a.Digest
	}

	var out *delivery.Deployment
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		// The Target is locked before the Cluster, the order reports, Target
		// restores, and heartbeats use, so they cannot deadlock. The Target
		// cannot be archived mid-observation, and the Cluster's shared lock
		// keeps a re-enrolment from swapping the agent.
		target, err := s.targets.GetTargetForUpdate(ctx, in.OrganizationID, in.TargetID)
		if err != nil {
			return err
		}
		if target.ClusterID() != in.ClusterID {
			return delivery.NewErrInvalidInput("the target is not in this cluster")
		}
		cluster, err := s.clusters.GetClusterForShare(ctx, in.OrganizationID, in.ClusterID)
		if err != nil {
			return err
		}
		if err := requireClusterAgent(cluster, actor.PrincipalID); err != nil {
			return err
		}
		if target.Archived() {
			return fmt.Errorf("%w: target %s", delivery.ErrArchived, target.ID())
		}
		env, err := s.environments.GetEnvironment(ctx, in.OrganizationID, target.EnvironmentID())
		if err != nil {
			return err
		}
		if env.Archived() {
			return fmt.Errorf("%w: environment %s", delivery.ErrArchived, env.ID())
		}
		app, err := s.applications.GetApplication(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}
		if app.Archived() {
			return fmt.Errorf("%w: application %s", delivery.ErrArchived, app.ID())
		}
		if app.WorkspaceID() != env.WorkspaceID() {
			return delivery.NewErrInvalidInput("the application runs in another workspace")
		}
		// While Ivaldi is applying a Deployment, the agent sees its images
		// mid-rollout. That rollout owns the placement until it finishes, so
		// the observation records nothing and returns it. A Deployment that is
		// only pending or awaiting approval has not touched the cluster, so it
		// does not hold observations back.
		inFlight, err := s.deployments.GetInFlightDeployment(ctx, in.OrganizationID, app.ID(), target.ID())
		if err != nil && !errors.Is(err, delivery.ErrNotFound) {
			return err
		}
		if err == nil && (inFlight.Status() == release.DeploymentApplying || inFlight.Status() == release.DeploymentProgressing) {
			out = inFlight
			return nil
		}

		placement, created, err := s.placementForObserve(ctx, in, target)
		if err != nil {
			return err
		}
		if placement.CurrentDeploymentID() != nil {
			current, err := s.deployments.GetDeployment(ctx, in.OrganizationID, *placement.CurrentDeploymentID())
			if err != nil && !errors.Is(err, delivery.ErrNotFound) {
				return err
			}
			if err == nil {
				same, err := s.sameDigests(ctx, current, in.Artifacts)
				if err != nil {
					return err
				}
				if same {
					out = current
					if created {
						return s.writer.Events(ctx, delivery.AggregatePlacement, placement.Key(), placement)
					}
					return nil
				}
			}
		}

		now := clock.Now()
		// The agent's clock may be skewed: an observation is never later
		// than the server's now.
		observedAt := now
		if in.ObservedAt != nil && in.ObservedAt.Before(now) {
			observedAt = in.ObservedAt.UTC().Truncate(time.Microsecond)
		}
		releaseBuilds, err := s.observedBuilds(ctx, in.OrganizationID, app.ID(), in.Artifacts)
		if err != nil {
			return err
		}
		// A Release that already holds exactly these images, such as one CI
		// created and a GitOps tool deployed, is the one running.
		rel, err := s.releases.GetReleaseWithImages(ctx, in.OrganizationID, app.ID(), buildIDsOf(releaseBuilds))
		if errors.Is(err, delivery.ErrNotFound) {
			version := observedVersion(in.Artifacts)
			channel, err := s.observedChannel(ctx, in.OrganizationID, app.ID(), placement)
			if err != nil {
				return err
			}
			rel, err = delivery.NewRelease(delivery.NewReleaseProps{
				OrganizationID: in.OrganizationID, ApplicationID: app.ID(), ChannelID: channel.ID(),
				Version: version, Status: release.StatusPublished, Source: delivery.ReleaseObserved,
				PublishedAt: &now, Builds: releaseBuilds,
			}, nil)
			if err != nil {
				return err
			}
			if err := s.releases.CreateRelease(ctx, rel); err != nil {
				if !errors.Is(err, delivery.ErrAlreadyExists) {
					return err
				}
				rel, err = s.releases.GetReleaseByVersion(ctx, in.OrganizationID, app.ID(), version)
				if err != nil {
					return err
				}
			} else {
				if err := s.writer.Events(ctx, delivery.AggregateRelease, rel.ID(), rel); err != nil {
					return err
				}
				// An observed Release deploys, if Ivaldi later executes it,
				// with the variables its Application had when it was first seen.
				revisions, err := s.snapshots.Take(ctx, rel, app.ProjectID(), nil)
				if err != nil {
					return err
				}
				for _, rv := range revisions {
					if err := s.writer.Events(ctx, delivery.AggregateReleaseVariables, rv.ID(), rv); err != nil {
						return err
					}
				}
			}
		} else if err != nil {
			return err
		}
		position, err := s.lifecyclePosition(ctx, rel, env.ID())
		if err != nil {
			return err
		}

		d, err := delivery.NewDeployment(delivery.NewDeploymentProps{
			OrganizationID: in.OrganizationID, ApplicationID: app.ID(), ReleaseID: rel.ID(),
			EnvironmentID: env.ID(), TargetID: target.ID(),
			Source: delivery.DeploymentObserved, Reason: delivery.DeploymentRollout,
			Status: release.DeploymentHealthy, PreviousDeploymentID: placement.CurrentDeploymentID(),
			ExternalRef: in.ExternalRef, Findings: position.findings(),
			RequestedAt: &observedAt, StartedAt: &observedAt, FinishedAt: &observedAt, SucceededAt: &observedAt,
		}, nil)
		if err != nil {
			return err
		}
		if err := s.deployments.CreateDeployment(ctx, d); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, delivery.AggregateDeployment, d.ID(), d); err != nil {
			return err
		}
		detail, err := observedDetail(in.Workloads)
		if err != nil {
			return err
		}
		if err := s.appendStatusEvent(ctx, d, &actor.PrincipalID, &in.ReportID, detail); err != nil {
			return err
		}
		if err := s.setCurrent(ctx, placement, d); err != nil {
			return err
		}
		if created {
			if err := s.writer.Events(ctx, delivery.AggregatePlacement, placement.Key(), placement); err != nil {
				return err
			}
		}
		out = d
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: delivery.EventDeploymentObserved, OrganizationID: in.OrganizationID,
			ResourceID: d.ID(), Actor: actor, After: d.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, out)
}

// placementForObserve returns the Application's placement on the Target,
// recording an observed one when nobody had placed it.
func (s *service) placementForObserve(ctx context.Context, in delivery.ObserveInput, target *delivery.Target) (*delivery.Placement, bool, error) {
	placement, err := s.applications.GetPlacement(ctx, in.OrganizationID, in.ApplicationID, target.ID())
	if err == nil {
		return placement, false, nil
	}
	if !errors.Is(err, delivery.ErrNotFound) {
		return nil, false, err
	}
	placement, err = delivery.NewPlacement(delivery.NewPlacementProps{
		OrganizationID: in.OrganizationID, ApplicationID: in.ApplicationID, TargetID: target.ID(),
		Source: delivery.PlacementObserved,
	})
	if err != nil {
		return nil, false, err
	}
	// Inserted only if absent: a plain insert that lost a race would abort
	// the transaction, and the read below could not run.
	inserted, err := s.applications.CreatePlacementIfAbsent(ctx, placement)
	if err != nil {
		return nil, false, err
	}
	if !inserted {
		placement, err = s.applications.GetPlacement(ctx, in.OrganizationID, in.ApplicationID, target.ID())
		return placement, false, err
	}
	return placement, true, nil
}

// observedDetail is the first status event's detail for an observed
// Deployment: the workloads the images came from.
func observedDetail(workloads []delivery.ObservedWorkload) (json.RawMessage, error) {
	if len(workloads) == 0 {
		return nil, nil
	}
	detail, err := json.Marshal(struct {
		Workloads []delivery.ObservedWorkload `json:"workloads"`
	}{workloads})
	if err != nil {
		return nil, fmt.Errorf("encode observed workloads: %w", err)
	}
	return detail, nil
}

// sameDigests reports whether the Deployment's Release runs exactly the
// images the agent reported.
func (s *service) sameDigests(ctx context.Context, current *delivery.Deployment, reported []delivery.ObservedArtifact) (bool, error) {
	rel, err := s.releases.GetRelease(ctx, current.OrganizationID(), current.ReleaseID())
	if err != nil {
		return false, err
	}
	builds, err := s.buildsOf(ctx, rel)
	if err != nil {
		return false, err
	}
	artifacts, err := s.applications.ListArtifacts(ctx, current.OrganizationID(), current.ApplicationID(), true)
	if err != nil {
		return false, err
	}
	byID := map[string]*delivery.Artifact{}
	for _, a := range artifacts {
		byID[a.ID()] = a
	}
	currentSet := map[string]string{}
	for _, rb := range rel.Builds() {
		a, b := byID[rb.ArtifactID], builds[rb.ArtifactID]
		// Only images are observed; a chart in the Release does not run.
		if a == nil || b == nil || a.ArtifactKind() != delivery.ArtifactContainerImage {
			continue
		}
		currentSet[a.ArtifactName()] = b.Digest()
	}
	if len(currentSet) != len(reported) {
		return false, nil
	}
	for _, a := range reported {
		if currentSet[a.ArtifactName] != a.Digest {
			return false, nil
		}
	}
	return true, nil
}

// observedBuilds records the Artifacts and Builds the reported images name,
// creating the ones that are missing. It returns the Release's Builds.
func (s *service) observedBuilds(ctx context.Context, organizationID, applicationID string, reported []delivery.ObservedArtifact) ([]delivery.NewReleaseBuildProps, error) {
	existing, err := s.applications.ListArtifacts(ctx, organizationID, applicationID, false)
	if err != nil {
		return nil, err
	}
	byPublished := map[string]*delivery.Artifact{}
	names := map[string]bool{}
	for _, a := range existing {
		byPublished[a.ArtifactName()] = a
		names[a.Name()] = true
	}
	var builds []delivery.NewReleaseBuildProps
	for _, reportedArtifact := range reported {
		artifact := byPublished[reportedArtifact.ArtifactName]
		if artifact == nil {
			name := uniqueArtifactName(artifactNameFromRepository(reportedArtifact.ArtifactName), names)
			artifact, err = delivery.NewArtifact(delivery.NewArtifactProps{
				OrganizationID: organizationID, ApplicationID: applicationID, Name: name,
				ArtifactKind: delivery.ArtifactContainerImage, ArtifactName: reportedArtifact.ArtifactName,
				Source: delivery.ArtifactObserved,
			}, nil)
			if err != nil {
				return nil, err
			}
			if err := s.applications.CreateArtifact(ctx, artifact); err != nil {
				return nil, err
			}
			if err := s.writer.Events(ctx, delivery.AggregateArtifact, artifact.ID(), artifact); err != nil {
				return nil, err
			}
			byPublished[reportedArtifact.ArtifactName] = artifact
			names[name] = true
		}
		build, err := s.builds.GetBuildByDigestForUpdate(ctx, organizationID, reportedArtifact.ArtifactName, reportedArtifact.Digest)
		if errors.Is(err, delivery.ErrNotFound) {
			build, err = delivery.NewBuild(delivery.NewBuildProps{
				OrganizationID: organizationID, ArtifactKind: delivery.ArtifactContainerImage,
				ArtifactName: reportedArtifact.ArtifactName, Digest: reportedArtifact.Digest,
				Provenance: delivery.BuildObserved,
			}, nil)
			if err != nil {
				return nil, err
			}
			inserted, err := s.builds.CreateBuild(ctx, build)
			if err != nil {
				return nil, err
			}
			if !inserted {
				build, err = s.builds.GetBuildByDigestForUpdate(ctx, organizationID, reportedArtifact.ArtifactName, reportedArtifact.Digest)
				if err != nil {
					return nil, err
				}
			} else if err := s.writer.Events(ctx, delivery.AggregateBuild, build.ID(), build); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		builds = append(builds, delivery.NewReleaseBuildProps{ArtifactID: artifact.ID(), BuildID: build.ID()})
	}
	return builds, nil
}

// observedChannel reuses the channel of the current Release, so an observed
// Deployment does not move the Application to another channel.
func (s *service) observedChannel(ctx context.Context, organizationID, applicationID string, placement *delivery.Placement) (*delivery.Channel, error) {
	if placement.CurrentDeploymentID() != nil {
		if d, err := s.deployments.GetDeployment(ctx, organizationID, *placement.CurrentDeploymentID()); err == nil {
			if rel, err := s.releases.GetRelease(ctx, organizationID, d.ReleaseID()); err == nil {
				return s.applications.GetChannel(ctx, organizationID, rel.ChannelID())
			}
		}
	}
	return s.applications.GetDefaultChannel(ctx, organizationID, applicationID)
}

func buildIDsOf(builds []delivery.NewReleaseBuildProps) []string {
	ids := make([]string, len(builds))
	for i, b := range builds {
		ids[i] = b.BuildID
	}
	return ids
}

// observedVersion names an observed Release after the images it runs, so a
// retried observation finds the same Release instead of making another.
func observedVersion(artifacts []delivery.ObservedArtifact) string {
	sorted := append([]delivery.ObservedArtifact(nil), artifacts...)
	slices.SortFunc(sorted, func(a, b delivery.ObservedArtifact) int {
		return strings.Compare(a.ArtifactName, b.ArtifactName)
	})
	hash := sha256.New()
	for _, a := range sorted {
		fmt.Fprintf(hash, "%s@%s\n", a.ArtifactName, a.Digest)
	}
	return "observed-" + hex.EncodeToString(hash.Sum(nil))[:16]
}

// artifactNameFromRepository derives a slug-shaped artifact name from the
// repository an image is published under.
func artifactNameFromRepository(published string) string {
	base := published
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	if len(name) > 40 {
		name = strings.TrimRight(name[:40], "-")
	}
	if name == "" {
		name = "observed"
	}
	return name
}

// uniqueArtifactName adds a short suffix when the derived name is taken.
func uniqueArtifactName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if !taken[candidate] {
			return candidate
		}
	}
}

func (s *service) appendStatusEvent(ctx context.Context, d *delivery.Deployment, reportedBy, reportID *string, detail json.RawMessage) error {
	status := d.Status()
	event, err := delivery.NewDeploymentEvent(delivery.NewDeploymentEventProps{
		OrganizationID: d.OrganizationID(), DeploymentID: d.ID(), Kind: delivery.DeploymentEventStatusChanged,
		Status: &status, ReportedByPrincipalID: reportedBy, ReportID: reportID, Detail: detail,
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
