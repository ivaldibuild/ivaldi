// Package teams implements team.TeamService: Teams, their members, and
// their HTTP API. The domain types stay in team; this package orchestrates
// them.
//
// NewTeamService returns the service wrapped in an authorizing layer
// (authorization.go), the only way to reach it, so every caller (the HTTP
// API, MCP, workers) is checked the same way. The service below assumes that
// check has happened and records the decision that allowed each change in
// its audit record.
package teams

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type TeamServiceOption func(*options)

type options struct {
	teams      team.TeamRepository
	principals team.Principals
	agents     team.Agents
	groups     iam.TeamGroups
	owners     []team.Owner
	authz      authorization.Authorizer
	writer     change.Writer
}

func WithTeamRepo(r team.TeamRepository) TeamServiceOption {
	return func(o *options) { o.teams = r }
}

// WithPrincipals checks that a new member is an active user or Agent in the
// Organization. iam.PrincipalRepository provides it.
func WithPrincipals(p team.Principals) TeamServiceOption {
	return func(o *options) { o.principals = p }
}

// WithAgents stores the Agents Teams create. iam.AgentRepository provides
// it; it must write through the Transactor's context.
func WithAgents(a team.Agents) TeamServiceOption {
	return func(o *options) { o.agents = a }
}

// WithTeamGroups keeps each Team's IAM group in sync with its members. It
// must write through the Transactor's context.
func WithTeamGroups(g iam.TeamGroups) TeamServiceOption {
	return func(o *options) { o.groups = g }
}

// WithOwners adds capabilities that record owning Teams. A Team that still
// owns active records in any of them cannot be archived.
func WithOwners(owners ...team.Owner) TeamServiceOption {
	return func(o *options) { o.owners = append(o.owners, owners...) }
}

func WithAuthorizer(a authorization.Authorizer) TeamServiceOption {
	return func(o *options) { o.authz = a }
}

// WithTransactor makes a change, its events, and its audit record commit
// together, so the repository, the outbox, and the audit recorder must
// write through its context.
func WithTransactor(tx transaction.Transactor) TeamServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) TeamServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) TeamServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewTeamService returns the TeamService, authorizing every operation, or
// an error naming a missing dependency.
func NewTeamService(opts ...TeamServiceOption) (team.TeamService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	required := []struct {
		name string
		ok   bool
	}{
		{"TeamRepo", o.teams != nil},
		{"Principals", o.principals != nil},
		{"Agents", o.agents != nil},
		{"TeamGroups", o.groups != nil},
		{"Authorizer", o.authz != nil},
	}
	for _, r := range required {
		if !r.ok {
			return nil, fmt.Errorf("team service: %s is required", r.name)
		}
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("team service: %w", err)
	}
	s := &service{teams: o.teams, principals: o.principals, agents: o.agents, groups: o.groups, owners: o.owners, w: o.writer}
	return &authorized{next: s, authz: o.authz}, nil
}

// service carries out Team operations once they are authorized. It checks
// rules that span records, changes Teams only through their methods, keeps
// each Team's IAM group in sync, and commits each change with its events
// and audit record.
type service struct {
	teams      team.TeamRepository
	principals team.Principals
	agents     team.Agents
	groups     iam.TeamGroups
	owners     []team.Owner
	w          change.Writer
}

var _ team.TeamService = (*service)(nil)

func (s *service) CreateTeam(ctx context.Context, actor identity.Actor, in team.CreateTeamInput) (*team.Team, error) {
	t, err := team.NewTeam(team.NewTeamProps{
		OrganizationID: in.OrganizationID,
		Slug:           in.Slug,
		DisplayName:    in.DisplayName,
		Description:    in.Description,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.teams.CreateTeam(ctx, t); err != nil {
			return fmt.Errorf("create team: %w", err)
		}
		if err := s.syncGroup(ctx, t); err != nil {
			return err
		}
		if err := s.w.Events(ctx, team.AggregateTeam, t.ID(), t); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: team.EventTeamCreated, OrganizationID: t.OrganizationID(), ResourceID: t.ID(), Actor: actor, After: t.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// groupName names the IAM group that mirrors t. An archived Team's slug may
// be taken by an active one, and group names are unique in the
// Organization, so an archived Team's group is named by its ID instead.
func groupName(t *team.Team) string {
	if t.Archived() {
		return "team:" + t.ID()
	}
	return "team:" + t.Slug()
}

// syncGroup makes t's IAM group match it: named for it, with its members,
// or with none while it is archived, so its grants stop applying.
func (s *service) syncGroup(ctx context.Context, t *team.Team) error {
	var members []string
	if !t.Archived() {
		for _, m := range t.Members() {
			members = append(members, m.PrincipalID)
		}
	}
	if err := s.groups.SyncTeamGroup(ctx, t.OrganizationID(), t.ID(), groupName(t), members); err != nil {
		return fmt.Errorf("sync the group of team %s: %w", t.ID(), err)
	}
	return nil
}

func (s *service) GetTeam(ctx context.Context, _ identity.Actor, organizationID, teamID string) (*team.Team, error) {
	return s.teams.GetTeam(ctx, organizationID, teamID)
}

func (s *service) GetTeamBySlug(ctx context.Context, _ identity.Actor, organizationID, slug string) (*team.Team, error) {
	return s.teams.GetTeamBySlug(ctx, organizationID, slug)
}

func (s *service) ListTeams(ctx context.Context, _ identity.Actor, query team.ListTeamsQuery) (*pagination.Result[*team.Team], error) {
	return s.teams.ListTeams(ctx, query)
}

func (s *service) ListTeamMembers(ctx context.Context, _ identity.Actor, organizationID, teamID string) ([]team.TeamMemberSnapshot, error) {
	t, err := s.teams.GetTeam(ctx, organizationID, teamID)
	if err != nil {
		return nil, err
	}
	return t.Members(), nil
}

// changeTeam applies a change to a Team, saves it, and syncs its group. The
// Team is locked, because the repository writes its whole member list: two
// unlocked changes could each drop the other's member.
func (s *service) changeTeam(ctx context.Context, actor identity.Actor, organizationID, teamID, event string, apply func(context.Context, *team.Team) error) (*team.Team, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*team.Team]{
		AggregateType: team.AggregateTeam,
		AggregateID:   func(t *team.Team) string { return t.ID() },
		Load: func(ctx context.Context) (*team.Team, error) {
			return s.teams.GetTeamForUpdate(ctx, organizationID, teamID)
		},
		Apply:    apply,
		Save:     s.saveTeam,
		Snapshot: func(t *team.Team) any { return t.Snapshot() },
		Event: func(t *team.Team) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: t.ID()}
		},
	})
}

// saveTeam writes the Team and syncs its group.
func (s *service) saveTeam(ctx context.Context, t *team.Team) error {
	if err := s.teams.UpdateTeam(ctx, t); err != nil {
		return err
	}
	return s.syncGroup(ctx, t)
}

// UpdateTeam changes an active Team.
func (s *service) UpdateTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string, props team.UpdateTeamProps) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamUpdated,
		func(_ context.Context, t *team.Team) error { return t.Update(props) })
}

// ArchiveTeam archives a Team that owns no active records in other
// capabilities and is no Agent's only active Team. Its group loses its
// members until it is restored. Archiving an archived one does nothing.
func (s *service) ArchiveTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamArchived,
		func(ctx context.Context, t *team.Team) error {
			if t.Archived() {
				return change.ErrUnchanged
			}
			if err := team.CheckOwned(ctx, s.owners, organizationID, teamID); err != nil {
				return err
			}
			var alone []string
			for _, m := range t.Members() {
				if m.PrincipalKind != team.MemberAgent {
					continue
				}
				last, err := s.lastTeam(ctx, organizationID, m.PrincipalID)
				if err != nil {
					return err
				}
				if last {
					alone = append(alone, m.PrincipalID)
				}
			}
			if len(alone) > 0 {
				return fmt.Errorf("%w: agents %s belong to no other active team; add them to one first", team.ErrLastTeam, strings.Join(alone, ", "))
			}
			t.Archive()
			return nil
		})
}

// lastTeam reports whether an Agent is a member of one active Team at most,
// the one being changed.
func (s *service) lastTeam(ctx context.Context, organizationID, principalID string) (bool, error) {
	n, err := s.teams.CountActiveTeamsWithMember(ctx, organizationID, principalID)
	if err != nil {
		return false, fmt.Errorf("count the teams of agent %s: %w", principalID, err)
	}
	return n <= 1, nil
}

// RestoreTeam restores an archived Team, and its group's members. Its slug
// must still be free.
func (s *service) RestoreTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamRestored,
		func(_ context.Context, t *team.Team) error {
			if !t.Archived() {
				return change.ErrUnchanged
			}
			t.Restore()
			return nil
		})
}

// SetTeamMember adds an active user or Agent to an active Team, or changes
// a member's role. Setting a member's current role records nothing.
func (s *service) SetTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string, role team.Role) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamMemberSet,
		func(ctx context.Context, t *team.Team) error {
			kind, err := s.memberKind(ctx, organizationID, principalID)
			if err != nil {
				return err
			}
			if m, ok := t.Member(principalID); ok && m.Role == role && !t.Archived() {
				return change.ErrUnchanged
			}
			return t.SetMember(principalID, kind, role)
		})
}

// memberKind checks that principalID is an active user or Agent in the
// Organization, and returns its kind.
func (s *service) memberKind(ctx context.Context, organizationID, principalID string) (team.MemberKind, error) {
	p, err := s.principals.Get(ctx, organizationID, principalID)
	if errors.Is(err, iam.ErrNotFound) {
		return "", team.NewErrInvalidInput(fmt.Sprintf("principal %s is not in the organization", principalID))
	}
	if err != nil {
		return "", fmt.Errorf("get principal %s: %w", principalID, err)
	}
	kind := team.MemberKind(p.Kind)
	if kind != team.MemberUser && kind != team.MemberAgent {
		return "", team.NewErrInvalidInput(fmt.Sprintf("principal %s is a %s; members are users or agents", principalID, p.Kind))
	}
	if p.Status != iam.PrincipalActive {
		return "", team.NewErrInvalidInput(fmt.Sprintf("%s %s is %s, not active", p.Kind, principalID, p.Status))
	}
	return kind, nil
}

// RemoveTeamMember removes a member from an active Team. The member need
// not be active, so suspended and deactivated users can be removed. An
// Agent cannot leave its only active Team.
func (s *service) RemoveTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamMemberRemoved,
		func(ctx context.Context, t *team.Team) error {
			if m, ok := t.Member(principalID); ok && m.PrincipalKind == team.MemberAgent && !t.Archived() {
				last, err := s.lastTeam(ctx, organizationID, principalID)
				if err != nil {
					return err
				}
				if last {
					return fmt.Errorf("%w: agent %s belongs to no other active team; add it to one first", team.ErrLastTeam, principalID)
				}
			}
			return t.RemoveMember(principalID)
		})
}

// CreateAgent creates an Agent in IAM and adds it to an active Team, in one
// transaction: the Agent never exists without its Team. The Agent's events
// go to the outbox as IAM's, and the change is audited twice: the Agent's
// creation, and the Team's new member.
func (s *service) CreateAgent(ctx context.Context, actor identity.Actor, in team.CreateAgentInput) (*team.CreatedAgent, error) {
	if in.Slug == "" {
		in.Slug = slug.Create(in.DisplayName)
	}
	if in.Role == "" {
		in.Role = team.RoleMember
	}
	agent, err := iam.NewAgent(iam.NewAgentProps{
		PrincipalProps: iam.PrincipalProps{OrganizationID: in.OrganizationID, DisplayName: in.DisplayName},
		Slug:           in.Slug,
	}, nil)
	var invalid *iam.ErrInvalidInput
	if errors.As(err, &invalid) {
		return nil, team.NewErrInvalidInput(invalid.Message)
	}
	if err != nil {
		return nil, err
	}

	var t *team.Team
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if t, err = s.teams.GetTeamForUpdate(ctx, in.OrganizationID, in.TeamID); err != nil {
			return err
		}
		before := t.Snapshot()
		if err := t.SetMember(agent.ID(), team.MemberAgent, in.Role); err != nil {
			return err
		}
		if err := s.agents.Write(ctx, agent); err != nil {
			if errors.Is(err, iam.ErrAlreadyExists) {
				return fmt.Errorf("%w: another agent in the organization is %q", team.ErrSlugTaken, in.Slug)
			}
			return fmt.Errorf("create agent: %w", err)
		}
		if err := s.saveTeam(ctx, t); err != nil {
			return err
		}
		if err := s.w.Events(ctx, iam.AggregateAgent, agent.ID(), agent); err != nil {
			return err
		}
		if err := s.w.Events(ctx, team.AggregateTeam, t.ID(), t); err != nil {
			return err
		}
		if err := s.w.AuditRecord(ctx, audit.Event{
			Type: team.EventTeamAgentCreated, OrganizationID: in.OrganizationID, ResourceID: agent.ID(), Actor: actor, After: agent.Snapshot(),
		}); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{
			Type: team.EventTeamMemberSet, OrganizationID: in.OrganizationID, ResourceID: t.ID(), Actor: actor, Before: before, After: t.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return &team.CreatedAgent{Agent: agent, Team: t}, nil
}
