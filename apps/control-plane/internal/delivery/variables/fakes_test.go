package variables

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const org = "org_acme"

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// store fakes the Variable repository, the lookups, the transactor, and the
// locker. A transaction restores the variables when its function fails, as
// a rolled-back database transaction would. Its auditLog and outboxLog fake
// the recorders.
type store struct {
	variables    map[string]delivery.VariableSnapshot
	applications map[string]*delivery.Application
	channels     map[string]*delivery.Channel
	environments map[string]*delivery.Environment
	targets      map[string]*delivery.Target
	projects     map[string]*workspace.Project
	audit        []audit.Event
	events       []events.Envelope
	locked       []string
}

func (s *store) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	saved := maps.Clone(s.variables)
	if err := fn(ctx); err != nil {
		s.variables = saved
		return err
	}
	return nil
}

func (s *store) Lock(_ context.Context, key string) error {
	s.locked = append(s.locked, key)
	return nil
}

func rebuild(snap delivery.VariableSnapshot) *delivery.Variable {
	values := make([]delivery.NewVariableValueProps, len(snap.Values))
	for i, value := range snap.Values {
		values[i] = delivery.NewVariableValueProps{ID: &value.ID, Value: value.Value, Scope: value.Scope, CreatedAt: &value.CreatedAt, UpdatedAt: &value.UpdatedAt}
	}
	return must(delivery.NewVariable(delivery.NewVariableProps{
		OrganizationID: snap.OrganizationID, ProjectID: snap.ProjectID, ApplicationID: snap.ApplicationID,
		Name: snap.Name, Description: snap.Description, Kind: snap.Kind, ValuesPath: snap.ValuesPath, DeliverAs: snap.DeliverAs,
		Values: values, CreatedAt: &snap.CreatedAt, UpdatedAt: &snap.UpdatedAt, ArchivedAt: snap.ArchivedAt,
	}, &snap.ID))
}

func (s *store) CreateVariable(_ context.Context, v *delivery.Variable) error {
	s.variables[v.ID()] = v.Snapshot()
	return nil
}

func (s *store) UpdateVariable(ctx context.Context, v *delivery.Variable) error {
	return s.CreateVariable(ctx, v)
}

func (s *store) GetVariable(_ context.Context, organizationID, variableID string) (*delivery.Variable, error) {
	snap, ok := s.variables[variableID]
	if !ok || snap.OrganizationID != organizationID {
		return nil, delivery.ErrNotFound
	}
	return rebuild(snap), nil
}

// active returns the active variables matching keep, by name.
func (s *store) active(keep func(delivery.VariableSnapshot) bool) []*delivery.Variable {
	out := []*delivery.Variable{}
	for _, snap := range s.variables {
		if snap.ArchivedAt == nil && keep(snap) {
			out = append(out, rebuild(snap))
		}
	}
	slices.SortFunc(out, func(a, b *delivery.Variable) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}

func (s *store) ListVariables(_ context.Context, _ string, owner delivery.VariableOwner) ([]*delivery.Variable, error) {
	return s.active(func(snap delivery.VariableSnapshot) bool {
		if owner.ApplicationID != "" {
			return snap.ApplicationID != nil && *snap.ApplicationID == owner.ApplicationID
		}
		return snap.ProjectID != nil && *snap.ProjectID == owner.ProjectID
	}), nil
}

func (s *store) ListVariablesInProject(ctx context.Context, organizationID, projectID string) ([]*delivery.Variable, map[string][]*delivery.Variable, error) {
	project, _ := s.ListVariables(ctx, organizationID, delivery.VariableOwner{ProjectID: projectID})
	applications := map[string][]*delivery.Variable{}
	for _, app := range s.applications {
		if app.ProjectID() != projectID || app.Archived() {
			continue
		}
		if own, _ := s.ListVariables(ctx, organizationID, delivery.VariableOwner{ApplicationID: app.ID()}); len(own) > 0 {
			applications[app.ID()] = own
		}
	}
	return project, applications, nil
}

func (s *store) CountActiveVariablesWithEnvironment(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *store) CountActiveVariablesWithTarget(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *store) CountActiveVariablesWithChannel(context.Context, string, string) (int, error) {
	return 0, nil
}

func find[T any](m map[string]T, id string) (T, error) {
	v, ok := m[id]
	if !ok {
		var zero T
		return zero, delivery.ErrNotFound
	}
	return v, nil
}

func (s *store) GetApplication(_ context.Context, _, id string) (*delivery.Application, error) {
	return find(s.applications, id)
}
func (s *store) GetApplicationByIdentifier(_ context.Context, _, identifier string) (*delivery.Application, error) {
	for _, app := range s.applications {
		if app.ID() == identifier || app.Slug() == identifier {
			return app, nil
		}
	}
	return nil, delivery.ErrNotFound
}
func (s *store) GetChannel(_ context.Context, _, id string) (*delivery.Channel, error) {
	return find(s.channels, id)
}
func (s *store) GetTarget(_ context.Context, _, id string) (*delivery.Target, error) {
	return find(s.targets, id)
}
func (s *store) GetEnvironments(_ context.Context, _ string, ids []string) ([]*delivery.Environment, error) {
	var out []*delivery.Environment
	for _, id := range ids {
		if e, ok := s.environments[id]; ok {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *store) LookupWorkspace(context.Context, string, string) (*workspace.Workspace, error) {
	return nil, workspace.ErrNotFound
}
func (s *store) LookupProject(_ context.Context, _, id string) (*workspace.Project, error) {
	p, ok := s.projects[id]
	if !ok {
		return nil, workspace.ErrNotFound
	}
	return p, nil
}

type auditLog struct{ s *store }

func (a auditLog) Record(_ context.Context, e audit.Event) error {
	a.s.audit = append(a.s.audit, e)
	return nil
}

type outboxLog struct{ s *store }

func (o outboxLog) Record(_ context.Context, envs ...events.Envelope) error {
	o.s.events = append(o.s.events, envs...)
	return nil
}

// authorizer allows everything except what deny refuses, and remembers
// what it was asked. Each decision's ID is "dec-" and the request's
// resource ID.
type authorizer struct {
	asked []authorization.Request
	deny  func(authorization.Request) bool
}

func (a *authorizer) Authorize(_ context.Context, req authorization.Request) (authorization.Decision, error) {
	a.asked = append(a.asked, req)
	d := authorization.Decision{ID: "dec-" + req.ResourceID, Outcome: authorization.OutcomeAllow}
	if a.deny != nil && a.deny(req) {
		d.Outcome = authorization.OutcomeDeny
	}
	return d, nil
}

// askedOn returns the resources the authorizer was asked about for a permission.
func (a *authorizer) askedOn(permission string) []string {
	var out []string
	for _, req := range a.asked {
		if req.Permission == permission {
			out = append(out, req.ResourceID)
		}
	}
	return out
}

// world is a Project with two Applications, each with a Channel, in a
// Workspace with a staging and a production Environment, a Target in
// production, and an Environment in another Workspace.
type world struct {
	*store
	authz *authorizer
	svc   delivery.VariableService

	project            string
	billing, checkout  *delivery.Application
	hotfix, otherApps  *delivery.Channel
	staging, prod, far *delivery.Environment
	prodEU             *delivery.Target
}

func newWorld(t *testing.T) *world {
	t.Helper()
	s := &store{
		variables:    map[string]delivery.VariableSnapshot{},
		applications: map[string]*delivery.Application{},
		channels:     map[string]*delivery.Channel{},
		environments: map[string]*delivery.Environment{},
		targets:      map[string]*delivery.Target{},
		projects:     map[string]*workspace.Project{},
	}
	w := &world{store: s, authz: &authorizer{}}
	ws, elsewhere, team := id.New("wsp"), id.New("wsp"), "tea_01BX5ZZKBKACTAV9WEVGEMMVRZ"

	project := must(workspace.NewProject(workspace.NewProjectProps{
		OrganizationID: org, WorkspaceID: ws, OwnerTeamID: team, Slug: "payments", DisplayName: "Payments", CreatedByPrincipalID: "prn_a",
	}, nil))
	s.projects[project.ID()] = project
	w.project = project.ID()

	app := func(slug string) *delivery.Application {
		a := must(delivery.NewApplication(delivery.NewApplicationProps{
			OrganizationID: org, WorkspaceID: ws, ProjectID: project.ID(), OwnerTeamID: team, Slug: slug, DisplayName: slug,
		}, nil))
		s.applications[a.ID()] = a
		return a
	}
	channel := func(a *delivery.Application) *delivery.Channel {
		c := must(delivery.NewChannel(delivery.NewChannelProps{
			OrganizationID: org, ApplicationID: a.ID(), LifecycleID: id.New("lfc"), Slug: "hotfix", DisplayName: "Hotfix",
		}, nil))
		s.channels[c.ID()] = c
		return c
	}
	env := func(workspaceID, slug string, tier delivery.Tier) *delivery.Environment {
		e := must(delivery.NewEnvironment(delivery.NewEnvironmentProps{
			OrganizationID: org, WorkspaceID: workspaceID, Slug: slug, DisplayName: slug, Tier: tier,
		}, nil))
		s.environments[e.ID()] = e
		return e
	}
	w.billing, w.checkout = app("billing"), app("checkout")
	w.hotfix, w.otherApps = channel(w.billing), channel(w.checkout)
	w.staging, w.prod = env(ws, "staging", delivery.TierStaging), env(ws, "production", delivery.TierProduction)
	w.far = env(elsewhere, "far", delivery.TierStaging)
	w.prodEU = must(delivery.NewTarget(delivery.NewTargetProps{
		OrganizationID: org, EnvironmentID: w.prod.ID(), ClusterID: id.New("cls"), Slug: "eu", DisplayName: "EU",
		Runtime: delivery.RuntimeKubernetes, Locator: map[string]string{delivery.LocatorCluster: "prod-eu", delivery.LocatorNamespace: "payments"},
	}, nil))
	s.targets[w.prodEU.ID()] = w.prodEU

	svc, err := NewVariableService(
		WithVariableRepo(s), WithApplicationLookup(s), WithEnvironmentLookup(s), WithTargetLookup(s),
		WithWorkspaces(s), WithLocker(s), WithAuthorizer(w.authz), WithTransactor(s),
		WithOutboxRecorder(outboxLog{s}), WithAuditRecorder(auditLog{s}),
	)
	require.NoError(t, err)
	w.svc = svc
	return w
}
