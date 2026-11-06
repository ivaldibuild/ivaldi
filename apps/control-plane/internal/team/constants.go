package team

// Limits on what one record may hold. Text lengths match the columns.
const (
	maxDisplayNameLength = 255
)

// AggregateTeam names the domain object that raises Team events. An
// envelope carries the type and ID of the object whose change raised it.
const AggregateTeam = "team.Team"

// Permissions this capability asks the authorizer about. Teams are not IAM
// resources, so both are checked on the Organization. The full catalog, and
// which roles include each one, belongs to IAM: every built-in role reads
// Teams, and admins and owners manage them.
const (
	PermissionTeamRead   = "team.read"
	PermissionTeamManage = "team.manage"
)

// Actions name what an actor asks the authorizer to allow. Policy rules
// and approval requests match on them, and they appear in its decisions.
const (
	ActionTeamArchive      = "team.archive"
	ActionTeamCreate       = "team.create"
	ActionTeamList         = "team.list"
	ActionTeamListMembers  = "team.list_members"
	ActionTeamRead         = "team.read"
	ActionTeamRemoveMember = "team.remove_member"
	ActionTeamRestore      = "team.restore"
	ActionTeamSetMember    = "team.set_member"
	ActionTeamUpdate       = "team.update"
)

// Audit event types this capability emits.
const (
	EventTeamCreated       = "team.created"
	EventTeamUpdated       = "team.updated"
	EventTeamArchived      = "team.archived"
	EventTeamRestored      = "team.restored"
	EventTeamMemberSet     = "team.member_set"
	EventTeamMemberRemoved = "team.member_removed"
)
