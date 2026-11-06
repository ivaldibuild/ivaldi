package team

// Events a Team records. Each carries the Team, with its members, as it is
// after the change; the audit record keeps the state before it. Member
// events also name the member, and carry its previous role when the change
// loses it.

// TeamCreated is recorded when a Team is created.
type TeamCreated struct {
	Team TeamSnapshot `json:"team"`
}

// TeamUpdated is recorded when a Team's slug, display name, or description
// changes.
type TeamUpdated struct {
	Team TeamSnapshot `json:"team"`
}

// TeamMemberAdded is recorded when a principal joins a Team.
type TeamMemberAdded struct {
	Team        TeamSnapshot `json:"team"`
	PrincipalID string       `json:"principal_id"`
}

// TeamMemberRoleChanged is recorded when a member's role changes.
type TeamMemberRoleChanged struct {
	Team         TeamSnapshot `json:"team"`
	PrincipalID  string       `json:"principal_id"`
	PreviousRole Role         `json:"previous_role"`
}

// TeamMemberRemoved is recorded when a member leaves a Team. The Team no
// longer holds it.
type TeamMemberRemoved struct {
	Team         TeamSnapshot `json:"team"`
	PrincipalID  string       `json:"principal_id"`
	PreviousRole Role         `json:"previous_role"`
}

// TeamArchived is recorded when a Team is archived.
type TeamArchived struct {
	Team TeamSnapshot `json:"team"`
}

// TeamRestored is recorded when an archived Team is restored.
type TeamRestored struct {
	Team TeamSnapshot `json:"team"`
}

func (TeamCreated) EventName() string           { return "team.v1.team.created" }
func (TeamUpdated) EventName() string           { return "team.v1.team.updated" }
func (TeamMemberAdded) EventName() string       { return "team.v1.team.member_added" }
func (TeamMemberRoleChanged) EventName() string { return "team.v1.team.member_role_changed" }
func (TeamMemberRemoved) EventName() string     { return "team.v1.team.member_removed" }
func (TeamArchived) EventName() string          { return "team.v1.team.archived" }
func (TeamRestored) EventName() string          { return "team.v1.team.restored" }
