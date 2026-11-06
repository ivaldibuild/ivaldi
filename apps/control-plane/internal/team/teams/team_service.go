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

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type TeamServiceOption func(*options)

type options struct {
	teams  team.TeamRepository
	users  team.Users
	authz  authorization.Authorizer
	writer change.Writer
}

func WithTeamRepo(r team.TeamRepository) TeamServiceOption {
	return func(o *options) { o.teams = r }
}

// WithUsers checks that a new member is an active user in the Organization.
// iam.UserRepository provides it.
func WithUsers(u team.Users) TeamServiceOption {
	return func(o *options) { o.users = u }
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
		{"Users", o.users != nil},
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
	s := &service{teams: o.teams, users: o.users, w: o.writer}
	return &authorized{next: s, authz: o.authz}, nil
}

// service carries out Team operations once they are authorized. It checks
// rules that span records, changes Teams only through their methods, and
// commits each change with its events and audit record.
type service struct {
	teams team.TeamRepository
	users team.Users
	w     change.Writer
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

// changeTeam applies a change to a Team. The Team is locked, because the
// repository writes its whole member list: two unlocked changes could each
// drop the other's member.
func (s *service) changeTeam(ctx context.Context, actor identity.Actor, organizationID, teamID, event string, apply func(context.Context, *team.Team) error) (*team.Team, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*team.Team]{
		AggregateType: team.AggregateTeam,
		AggregateID:   func(t *team.Team) string { return t.ID() },
		Load: func(ctx context.Context) (*team.Team, error) {
			return s.teams.GetTeamForUpdate(ctx, organizationID, teamID)
		},
		Apply:    apply,
		Save:     s.teams.UpdateTeam,
		Snapshot: func(t *team.Team) any { return t.Snapshot() },
		Event: func(t *team.Team) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: t.ID()}
		},
	})
}

// UpdateTeam changes an active Team.
func (s *service) UpdateTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string, props team.UpdateTeamProps) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamUpdated,
		func(_ context.Context, t *team.Team) error { return t.Update(props) })
}

// ArchiveTeam archives a Team. Archiving an archived one does nothing.
func (s *service) ArchiveTeam(ctx context.Context, actor identity.Actor, organizationID, teamID string) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamArchived,
		func(_ context.Context, t *team.Team) error {
			if t.Archived() {
				return change.ErrUnchanged
			}
			t.Archive()
			return nil
		})
}

// RestoreTeam restores an archived Team. Its slug must still be free.
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

// SetTeamMember adds an active user to an active Team, or changes a
// member's role. Setting a member's current role records nothing.
func (s *service) SetTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string, role team.Role) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamMemberSet,
		func(ctx context.Context, t *team.Team) error {
			if err := s.checkUser(ctx, organizationID, principalID); err != nil {
				return err
			}
			if m, ok := t.Member(principalID); ok && m.Role == role && !t.Archived() {
				return change.ErrUnchanged
			}
			return t.SetMember(principalID, role)
		})
}

// checkUser checks that principalID is an active user in the Organization.
// Agents are not modeled in IAM yet, so only users can be members.
func (s *service) checkUser(ctx context.Context, organizationID, principalID string) error {
	u, err := s.users.Get(ctx, organizationID, principalID)
	if errors.Is(err, iam.ErrNotFound) {
		return team.NewErrInvalidInput(fmt.Sprintf("principal %s is not a user in the organization", principalID))
	}
	if err != nil {
		return fmt.Errorf("get user %s: %w", principalID, err)
	}
	if !u.Active() {
		return team.NewErrInvalidInput(fmt.Sprintf("user %s is %s, not active", principalID, u.Status()))
	}
	return nil
}

// RemoveTeamMember removes a member from an active Team. The member need
// not be an active user, so suspended and deactivated users can be removed.
func (s *service) RemoveTeamMember(ctx context.Context, actor identity.Actor, organizationID, teamID, principalID string) (*team.Team, error) {
	return s.changeTeam(ctx, actor, organizationID, teamID, team.EventTeamMemberRemoved,
		func(_ context.Context, t *team.Team) error { return t.RemoveMember(principalID) })
}
