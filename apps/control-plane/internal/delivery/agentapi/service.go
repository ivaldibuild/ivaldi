// Package agentapi serves the operations a Cluster's runtime agent calls.
// Every operation resolves the caller's Cluster from its workload principal:
// an agent never names a Cluster in a path, so it cannot reach another one.
package agentapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/release"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

// The narrow contracts this package needs.

type Clusters interface {
	GetClusterByAgentPrincipalID(ctx context.Context, organizationID, principalID string) (*delivery.Cluster, error)
	GetClusterForUpdate(ctx context.Context, organizationID, clusterID string) (*delivery.Cluster, error)
	UpdateCluster(ctx context.Context, c *delivery.Cluster) error
}

type Targets interface {
	ListTargets(ctx context.Context, query delivery.ListTargetsQuery) (*pagination.Result[*delivery.Target], error)
	GetTargetForUpdate(ctx context.Context, organizationID, targetID string) (*delivery.Target, error)
	UpdateTarget(ctx context.Context, t *delivery.Target) error
}

type Applications interface {
	ListPlacementsByTarget(ctx context.Context, organizationID, targetID string) ([]*delivery.Placement, error)
	GetApplication(ctx context.Context, organizationID, applicationID string) (*delivery.Application, error)
	ListArtifacts(ctx context.Context, organizationID, applicationID string, includeArchived bool) ([]*delivery.Artifact, error)
}

// Deployments reads a placement's current Deployment, for its status.
type Deployments interface {
	GetDeployment(ctx context.Context, organizationID, deploymentID string) (*delivery.Deployment, error)
}

type Assignments interface {
	ListClusterAssignments(ctx context.Context, actor identity.Actor, organizationID, clusterID string) ([]delivery.DeploymentAssignment, error)
}

type Observations interface {
	Observe(ctx context.Context, actor identity.Actor, in delivery.ObserveInput) (*delivery.DeploymentDetail, error)
}

// Intervals are the pace the control plane tells an agent to run at.
type Intervals struct {
	Heartbeat time.Duration
	Poll      time.Duration
	Watch     time.Duration
}

type ServiceOption func(*options)

type options struct {
	clusters     Clusters
	targets      Targets
	applications Applications
	deployments  Deployments
	assignments  Assignments
	observations Observations
	tx           transaction.Transactor
	intervals    Intervals
}

func WithClusters(c Clusters) ServiceOption         { return func(o *options) { o.clusters = c } }
func WithTargets(t Targets) ServiceOption           { return func(o *options) { o.targets = t } }
func WithApplications(a Applications) ServiceOption { return func(o *options) { o.applications = a } }
func WithAssignments(a Assignments) ServiceOption   { return func(o *options) { o.assignments = a } }
func WithDeployments(d Deployments) ServiceOption   { return func(o *options) { o.deployments = d } }
func WithObservations(o2 Observations) ServiceOption {
	return func(o *options) { o.observations = o2 }
}
func WithTransactor(tx transaction.Transactor) ServiceOption { return func(o *options) { o.tx = tx } }
func WithIntervals(i Intervals) ServiceOption                { return func(o *options) { o.intervals = i } }

// NewService returns the agent API service, or an error naming a missing
// dependency.
func NewService(opts ...ServiceOption) (Service, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"Clusters", o.clusters != nil},
		{"Targets", o.targets != nil},
		{"Applications", o.applications != nil},
		{"Deployments", o.deployments != nil},
		{"Assignments", o.assignments != nil},
		{"Observations", o.observations != nil},
		{"Transactor", o.tx != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("agent api: %s is required", r.name)
		}
	}
	return &service{options: o}, nil
}

// Service serves the operations a Cluster's runtime agent calls. Each one
// resolves the caller's Cluster from its workload principal.
type Service interface {
	// Targets returns what the caller's Cluster's agent should watch.
	Targets(ctx context.Context, caller identity.Caller) (*TargetsResponse, error)
	// Assignments returns the in-flight Deployments on the Cluster's Targets.
	Assignments(ctx context.Context, caller identity.Caller) ([]delivery.DeploymentAssignment, error)
	// Heartbeat records the agent's contact, its cluster-wide facts, and
	// what it can do in each Target.
	Heartbeat(ctx context.Context, caller identity.Caller, in HeartbeatInput) error
	// Observe records what the agent sees running on a Target.
	Observe(ctx context.Context, caller identity.Caller, in delivery.ObserveInput) (*delivery.DeploymentDetail, error)
}

type service struct {
	options
}

var _ Service = (*service)(nil)

// TargetsResponse is what an agent watches: its Cluster, its Targets, the
// Applications on them, and the intervals to run at.
type TargetsResponse struct {
	Cluster                  ClusterView  `json:"cluster"`
	HeartbeatIntervalSeconds int          `json:"heartbeat_interval_seconds"`
	PollIntervalSeconds      int          `json:"poll_interval_seconds"`
	WatchIntervalSeconds     int          `json:"watch_interval_seconds"`
	Targets                  []TargetView `json:"targets"`
}

type ClusterView struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

type TargetView struct {
	ID           string            `json:"id"`
	Slug         string            `json:"slug"`
	DisplayName  string            `json:"display_name"`
	Namespace    string            `json:"namespace"`
	Capabilities CapabilityView    `json:"capabilities"`
	Applications []ApplicationView `json:"applications"`
}

type CapabilityView struct {
	Observe bool   `json:"observe"`
	Execute bool   `json:"execute"`
	Reason  string `json:"reason,omitempty"`
}

type ApplicationView struct {
	ID                  string            `json:"id"`
	Slug                string            `json:"slug"`
	WorkloadSelector    map[string]string `json:"workload_selector"`
	Artifacts           []ArtifactView    `json:"artifacts"`
	CurrentDeploymentID *string           `json:"current_deployment_id"`
	// CurrentDeploymentStatus is the current Deployment's status, so an
	// agent that restarts resumes tracking its health from where it was.
	CurrentDeploymentStatus *release.DeploymentStatus `json:"current_deployment_status" enum:"pending,awaiting_approval,rejected,cancelled,applying,progressing,healthy,degraded,failed,superseded"`
}

type ArtifactView struct {
	Name         string `json:"name"`
	ArtifactName string `json:"artifact_name"`
}

// Targets returns what the caller's Cluster's agent should watch.
func (s *service) Targets(ctx context.Context, caller identity.Caller) (*TargetsResponse, error) {
	cluster, err := s.clusterFor(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := &TargetsResponse{
		Cluster:                  ClusterView{ID: cluster.ID(), Slug: cluster.Slug(), DisplayName: cluster.DisplayName()},
		HeartbeatIntervalSeconds: seconds(s.intervals.Heartbeat),
		PollIntervalSeconds:      seconds(s.intervals.Poll),
		WatchIntervalSeconds:     seconds(s.intervals.Watch),
	}
	query := delivery.ListTargetsQuery{
		OrganizationID: caller.OrganizationID, ClusterID: cluster.ID(),
		Page: pagination.Query{Limit: 100},
	}
	for {
		page, err := s.targets.ListTargets(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, target := range page.Items {
			view, err := s.targetView(ctx, caller.OrganizationID, target)
			if err != nil {
				return nil, err
			}
			out.Targets = append(out.Targets, *view)
		}
		if !page.HasNext || page.NextCursor == nil {
			return out, nil
		}
		query.Page.NextCursor = page.NextCursor
	}
}

func (s *service) targetView(ctx context.Context, organizationID string, target *delivery.Target) (*TargetView, error) {
	placements, err := s.applications.ListPlacementsByTarget(ctx, organizationID, target.ID())
	if err != nil {
		return nil, err
	}
	view := &TargetView{
		ID: target.ID(), Slug: target.Slug(), DisplayName: target.DisplayName(),
		Namespace:    target.Locator()[delivery.LocatorNamespace],
		Capabilities: CapabilityView{Observe: target.CanObserve(), Execute: target.CanExecute(), Reason: target.CapabilityReason()},
	}
	for _, placement := range placements {
		app, err := s.applications.GetApplication(ctx, organizationID, placement.ApplicationID())
		if err != nil {
			return nil, err
		}
		if app.Archived() {
			continue
		}
		artifacts, err := s.applications.ListArtifacts(ctx, organizationID, app.ID(), false)
		if err != nil {
			return nil, err
		}
		appView := ApplicationView{
			ID: app.ID(), Slug: app.Slug(), WorkloadSelector: app.WorkloadSelector(),
			CurrentDeploymentID: placement.CurrentDeploymentID(),
		}
		if id := placement.CurrentDeploymentID(); id != nil {
			current, err := s.deployments.GetDeployment(ctx, organizationID, *id)
			if err != nil {
				return nil, err
			}
			status := current.Status()
			appView.CurrentDeploymentStatus = &status
		}
		for _, artifact := range artifacts {
			appView.Artifacts = append(appView.Artifacts, ArtifactView{Name: artifact.Name(), ArtifactName: artifact.ArtifactName()})
		}
		view.Applications = append(view.Applications, appView)
	}
	return view, nil
}

// Assignments returns the in-flight Deployments on any of the Cluster's
// Targets.
func (s *service) Assignments(ctx context.Context, caller identity.Caller) ([]delivery.DeploymentAssignment, error) {
	cluster, err := s.clusterFor(ctx, caller)
	if err != nil {
		return nil, err
	}
	return s.assignments.ListClusterAssignments(ctx, caller.Actor, caller.OrganizationID, cluster.ID())
}

// HeartbeatInput is one heartbeat from an agent.
type HeartbeatInput struct {
	AgentVersion      string
	KubernetesVersion string
	WatchMode         string
	Deployers         []string
	SecretStoreKinds  []string
	// SecretStoreModes are the access modes the agent can open stores in.
	SecretStoreModes []string
	// SecretCredentials name the credentials the agent's operator
	// configured for secret stores.
	SecretCredentials []string
	Targets           []TargetHeartbeat
}

// TargetHeartbeat is what the agent can do in one Target's namespace.
type TargetHeartbeat struct {
	TargetID string
	Observe  bool
	Execute  bool
	Reason   string
}

// Heartbeat records the agent's contact, its cluster-wide facts, and what it
// can do in each Target. A heartbeat that names a Target outside the Cluster,
// or an archived one, changes nothing.
//
// It locks Targets before the Cluster, the order every operation that locks
// both uses (reports, observations, Target restores), so a heartbeat never
// deadlocks with them. Only Targets whose capabilities change are locked and
// written, so a steady Cluster costs one list query and one row update.
func (s *service) Heartbeat(ctx context.Context, caller identity.Caller, in HeartbeatInput) error {
	if in.WatchMode != "" && in.WatchMode != "cluster" && in.WatchMode != "namespaces" {
		return delivery.NewErrInvalidInput("watch mode must be cluster or namespaces")
	}
	cluster, err := s.clusterFor(ctx, caller)
	if err != nil {
		return err
	}
	active, err := s.activeTargets(ctx, caller.OrganizationID, cluster.ID())
	if err != nil {
		return err
	}
	// Checked before the transaction so a bad report writes nothing.
	var changed []TargetHeartbeat
	for _, reported := range in.Targets {
		target, ok := active[reported.TargetID]
		if !ok {
			return delivery.NewErrInvalidInput(fmt.Sprintf("target %s is not an active target in this cluster", reported.TargetID))
		}
		if !target.HasCapabilities(reported.Observe, reported.Execute, reported.Reason) {
			changed = append(changed, reported)
		}
	}
	// A fixed order, so two writers locking several Targets cannot deadlock.
	slices.SortFunc(changed, func(a, b TargetHeartbeat) int { return strings.Compare(a.TargetID, b.TargetID) })

	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, reported := range changed {
			target, err := s.targets.GetTargetForUpdate(ctx, caller.OrganizationID, reported.TargetID)
			if err != nil {
				return err
			}
			// Archived or moved since the list was read: nothing to record.
			if target.ClusterID() != cluster.ID() || target.Archived() {
				continue
			}
			ok, err := target.SetCapabilities(reported.Observe, reported.Execute, reported.Reason)
			if err != nil {
				return err
			}
			if ok {
				if err := s.targets.UpdateTarget(ctx, target); err != nil {
					return err
				}
			}
		}
		// Locked so a re-enrolment cannot swap the agent between the check
		// in RecordHeartbeat and the write.
		locked, err := s.clusters.GetClusterForUpdate(ctx, caller.OrganizationID, cluster.ID())
		if err != nil {
			return err
		}
		capabilities := map[string]any{
			"agent_version":            in.AgentVersion,
			"kubernetes_version":       in.KubernetesVersion,
			"watch_mode":               in.WatchMode,
			"deployers":                strings0(in.Deployers),
			"secret_store_kinds":       strings0(in.SecretStoreKinds),
			"secret_store_modes":       strings0(in.SecretStoreModes),
			"secret_store_credentials": strings0(in.SecretCredentials),
		}
		if err := locked.RecordHeartbeat(caller.Actor.PrincipalID, capabilities); err != nil {
			return err
		}
		return s.clusters.UpdateCluster(ctx, locked)
	})
}

// activeTargets returns the Cluster's active Targets by ID, read page by page
// without locking.
func (s *service) activeTargets(ctx context.Context, organizationID, clusterID string) (map[string]*delivery.Target, error) {
	out := map[string]*delivery.Target{}
	query := delivery.ListTargetsQuery{
		OrganizationID: organizationID, ClusterID: clusterID,
		Page: pagination.Query{Limit: 100},
	}
	for {
		page, err := s.targets.ListTargets(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, target := range page.Items {
			out[target.ID()] = target
		}
		if !page.HasNext || page.NextCursor == nil {
			return out, nil
		}
		query.Page.NextCursor = page.NextCursor
	}
}

// Observe records what the agent sees running on a Target.
func (s *service) Observe(ctx context.Context, caller identity.Caller, in delivery.ObserveInput) (*delivery.DeploymentDetail, error) {
	cluster, err := s.clusterFor(ctx, caller)
	if err != nil {
		return nil, err
	}
	in.OrganizationID, in.ClusterID = caller.OrganizationID, cluster.ID()
	// The agent is authenticated by its workload credential, not a grant, but
	// the change still carries the decision that allowed it, with an ID its
	// audit record can be traced by.
	decision := authorization.Decision{
		ID: authorization.NewDecisionID(), Outcome: authorization.OutcomeAllow,
		Reason: "the cluster's agent reported what runs on its own target",
	}
	ctx = authorization.WithDecision(ctx, "", decision)
	return s.observations.Observe(ctx, caller.Actor, in)
}

// clusterFor resolves the Cluster whose agent the caller is. A caller that is
// not a workload, or whose workload serves no active Cluster, gets
// ErrNotClusterAgent.
func (s *service) clusterFor(ctx context.Context, caller identity.Caller) (*delivery.Cluster, error) {
	if caller.Actor.Kind != identity.ActorWorkload {
		return nil, delivery.ErrNotClusterAgent
	}
	cluster, err := s.clusters.GetClusterByAgentPrincipalID(ctx, caller.OrganizationID, caller.Actor.PrincipalID)
	if errors.Is(err, delivery.ErrNotFound) {
		return nil, delivery.ErrNotClusterAgent
	}
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

func seconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d / time.Second)
}

func strings0(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
