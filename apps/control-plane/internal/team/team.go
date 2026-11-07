// Package team is the control plane's team capability (ADR 0007): Teams of
// humans and Agents that own Projects and Applications. Ownership means
// responsibility, not access: work is routed to the owning Team, and access
// still comes only from IAM grants.
//
// This package holds the domain types, the interfaces the capability needs
// (interfaces.go), and the Directory other capabilities read Teams through.
// The service and its HTTP API live in teams; the PostgreSQL repository in
// postgres. Domain objects follow delivery's conventions: unexported fields,
// New… constructors that also rebuild stored objects, Snapshot for reading
// state, and events recorded only when something changes.
//
// Members are users or Agents (ADR 0007), and every Agent stays in at least
// one active Team; Agents are created through a Team. Each Team has an IAM
// group that mirrors its members, so Teams can receive grants: the service
// keeps it in sync through iam.TeamGroups in the transaction of every change.
package team

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// Domain types whose IDs carry a prefix register it, so a prefix used twice
// fails when the control plane starts.
func init() {
	id.MustRegister(Team{}.Prefix(), "team.Team")
}

// Role is a member's role on a Team.
type Role string

const (
	// RoleLead leads the Team: questions about its work go to its leads first.
	RoleLead Role = "lead"
	// RoleMember is any other member.
	RoleMember Role = "member"
)

// MemberKind is the kind of principal a member is. Its values are IAM's
// principal kinds.
type MemberKind string

const (
	MemberUser  MemberKind = MemberKind(iam.PrincipalUser)
	MemberAgent MemberKind = MemberKind(iam.PrincipalAgent)
)

func validateMemberKind(k MemberKind) error {
	switch k {
	case MemberUser, MemberAgent:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("member kind %q must be user or agent", k))
}

func validateRole(r Role) error {
	switch r {
	case RoleLead, RoleMember:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("role %q must be lead or member", r))
}

// Team is a group of principals in an Organization that owns work. It owns
// its members: they change only through its methods.
type Team struct {
	aggregate.Root
	organizationID string
	slug           string
	displayName    string
	description    *string
	// members are sorted by principal ID.
	members    []*member
	createdAt  time.Time
	updatedAt  time.Time
	archivedAt *time.Time
}

// member is one principal on a Team.
type member struct {
	principalID string
	kind        MemberKind
	role        Role
	createdAt   time.Time
	updatedAt   time.Time
}

// TeamSnapshot is a Team's state, for repositories, audit records, and
// events. Its JSON form is part of the Team events' contract.
type TeamSnapshot struct {
	ID             string  `json:"id"`
	OrganizationID string  `json:"organization_id"`
	Slug           string  `json:"slug"`
	DisplayName    string  `json:"display_name"`
	Description    *string `json:"description"`
	// Members are sorted by principal ID.
	Members    []TeamMemberSnapshot `json:"members"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
	ArchivedAt *time.Time           `json:"archived_at"`
}

// TeamMemberSnapshot is a member's state. CreatedAt is when the principal
// joined the Team, UpdatedAt when its role last changed.
type TeamMemberSnapshot struct {
	PrincipalID   string     `json:"principal_id"`
	PrincipalKind MemberKind `json:"principal_kind" enum:"user,agent"`
	Role          Role       `json:"role" enum:"lead,member"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// NewTeamProps describes a Team. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewTeamProps struct {
	OrganizationID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description *string
	Members     []NewTeamMemberProps
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
	ArchivedAt  *time.Time
}

// NewTeamMemberProps describes a member given to NewTeam. A new one leaves
// the timestamps nil; a stored one sets them.
type NewTeamMemberProps struct {
	PrincipalID   string
	PrincipalKind MemberKind
	Role          Role
	CreatedAt     *time.Time
	UpdatedAt     *time.Time
}

// UpdateTeamProps changes the fields that are set. An empty Description
// removes it.
type UpdateTeamProps struct {
	Slug        *string
	DisplayName *string
	Description *string
}

// NewTeam returns a Team, with a new ID unless teamID gives a stored one.
func NewTeam(props NewTeamProps, teamID *string) (*Team, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	root, err := newRoot(Team{}.Prefix(), teamID)
	if err != nil {
		return nil, err
	}
	if err := validateDisplayName(props.DisplayName); err != nil {
		return nil, err
	}
	s, err := slugOrDerived(props.Slug, props.DisplayName)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	t := &Team{
		Root:           root,
		organizationID: props.OrganizationID,
		slug:           s,
		displayName:    props.DisplayName,
		description:    optional.OfPtr(props.Description),
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		archivedAt:     clock.CopyTime(props.ArchivedAt),
	}
	for _, p := range props.Members {
		if err := validateMember(p.PrincipalID, p.PrincipalKind, p.Role); err != nil {
			return nil, err
		}
		if _, ok := t.memberIndex(p.PrincipalID); ok {
			return nil, NewErrInvalidInput(fmt.Sprintf("principal %s appears twice", p.PrincipalID))
		}
		t.insertMember(&member{
			principalID: p.PrincipalID,
			kind:        p.PrincipalKind,
			role:        p.Role,
			createdAt:   timeOr(p.CreatedAt, now),
			updatedAt:   timeOr(p.UpdatedAt, now),
		})
	}
	if t.IsNew() {
		t.RecordEvent(TeamCreated{t.Snapshot()})
	}
	return t, nil
}

// validateMember checks a member's principal ID, kind, and role. Users and
// Agents share IAM's principal prefix.
func validateMember(principalID string, kind MemberKind, role Role) error {
	if !id.Valid(iam.User{}.Prefix(), principalID) {
		return NewErrInvalidInput(fmt.Sprintf("%q is not a principal ID", principalID))
	}
	if err := validateMemberKind(kind); err != nil {
		return err
	}
	return validateRole(role)
}

func (Team) Prefix() string { return "tem" }

func (t *Team) OrganizationID() string { return t.organizationID }
func (t *Team) Slug() string           { return t.slug }
func (t *Team) DisplayName() string    { return t.displayName }
func (t *Team) Description() *string   { return copyString(t.description) }
func (t *Team) CreatedAt() time.Time   { return t.createdAt }
func (t *Team) UpdatedAt() time.Time   { return t.updatedAt }
func (t *Team) ArchivedAt() *time.Time { return clock.CopyTime(t.archivedAt) }

func (t *Team) Archived() bool { return t.archivedAt != nil }

// Members returns the members, sorted by principal ID.
func (t *Team) Members() []TeamMemberSnapshot {
	out := make([]TeamMemberSnapshot, len(t.members))
	for i, m := range t.members {
		out[i] = m.snapshot()
	}
	return out
}

// Member returns the member with principalID, and whether there is one.
func (t *Team) Member(principalID string) (TeamMemberSnapshot, bool) {
	i, ok := t.memberIndex(principalID)
	if !ok {
		return TeamMemberSnapshot{}, false
	}
	return t.members[i].snapshot(), true
}

func (m *member) snapshot() TeamMemberSnapshot {
	return TeamMemberSnapshot{PrincipalID: m.principalID, PrincipalKind: m.kind, Role: m.role, CreatedAt: m.createdAt, UpdatedAt: m.updatedAt}
}

func (t *Team) Snapshot() TeamSnapshot {
	return TeamSnapshot{
		ID:             t.ID(),
		OrganizationID: t.organizationID,
		Slug:           t.slug,
		DisplayName:    t.displayName,
		Description:    copyString(t.description),
		Members:        t.Members(),
		CreatedAt:      t.createdAt,
		UpdatedAt:      t.updatedAt,
		ArchivedAt:     clock.CopyTime(t.archivedAt),
	}
}

func (t *Team) checkActive() error {
	if t.Archived() {
		return fmt.Errorf("%w: team %s", ErrArchived, t.ID())
	}
	return nil
}

// Update changes an active Team. Nothing changes if any field is invalid.
func (t *Team) Update(props UpdateTeamProps) error {
	if err := t.checkActive(); err != nil {
		return err
	}
	if props.Slug != nil {
		if err := validateSlug(*props.Slug); err != nil {
			return err
		}
	}
	if props.DisplayName != nil {
		if err := validateDisplayName(*props.DisplayName); err != nil {
			return err
		}
	}

	changed := anyTrue(
		set(&t.slug, props.Slug),
		set(&t.displayName, props.DisplayName),
		setOptional(&t.description, props.Description),
	)
	t.updatedAt = clock.Now()
	if changed {
		t.RecordEvent(TeamUpdated{t.Snapshot()})
	}
	return nil
}

// SetMember adds principalID, a principal of kind, to an active Team with
// role, or changes the role of a member. Setting a member's current role
// changes nothing. A principal's kind never changes, so a member given
// another kind is refused. The caller checks that the principal exists, is
// of kind, and is active.
func (t *Team) SetMember(principalID string, kind MemberKind, role Role) error {
	if err := t.checkActive(); err != nil {
		return err
	}
	if err := validateMember(principalID, kind, role); err != nil {
		return err
	}
	now := clock.Now()
	if i, ok := t.memberIndex(principalID); ok {
		m := t.members[i]
		if m.kind != kind {
			return NewErrInvalidInput(fmt.Sprintf("member %s is a %s, not a %s", principalID, m.kind, kind))
		}
		if m.role == role {
			return nil
		}
		previous := m.role
		updated := *m
		updated.role = role
		updated.updatedAt = now
		t.members = slices.Clone(t.members)
		t.members[i] = &updated
		t.updatedAt = now
		t.RecordEvent(TeamMemberRoleChanged{Team: t.Snapshot(), PrincipalID: principalID, PreviousRole: previous})
		return nil
	}
	t.members = slices.Clone(t.members)
	t.insertMember(&member{principalID: principalID, kind: kind, role: role, createdAt: now, updatedAt: now})
	t.updatedAt = now
	t.RecordEvent(TeamMemberAdded{Team: t.Snapshot(), PrincipalID: principalID})
	return nil
}

// RemoveMember removes a member from an active Team.
func (t *Team) RemoveMember(principalID string) error {
	if err := t.checkActive(); err != nil {
		return err
	}
	i, ok := t.memberIndex(principalID)
	if !ok {
		return fmt.Errorf("%w: principal %s is not a member of team %s", ErrNotFound, principalID, t.ID())
	}
	previous := t.members[i].role
	t.members = slices.Delete(slices.Clone(t.members), i, i+1)
	t.updatedAt = clock.Now()
	t.RecordEvent(TeamMemberRemoved{Team: t.Snapshot(), PrincipalID: principalID, PreviousRole: previous})
	return nil
}

func (t *Team) memberIndex(principalID string) (int, bool) {
	return slices.BinarySearchFunc(t.members, principalID, func(m *member, id string) int {
		return strings.Compare(m.principalID, id)
	})
}

// insertMember adds m where it sorts. m must not be a member already.
func (t *Team) insertMember(m *member) {
	i, _ := t.memberIndex(m.principalID)
	t.members = slices.Insert(t.members, i, m)
}

// Archive archives the Team. Archiving an archived one does nothing.
func (t *Team) Archive() {
	if !t.Archived() {
		now := clock.Now()
		t.archivedAt = &now
		t.updatedAt = now
		t.RecordEvent(TeamArchived{t.Snapshot()})
	}
}

// Restore undoes Archive. Restoring an active Team does nothing.
func (t *Team) Restore() {
	if t.Archived() {
		t.archivedAt = nil
		t.updatedAt = clock.Now()
		t.RecordEvent(TeamRestored{t.Snapshot()})
	}
}
