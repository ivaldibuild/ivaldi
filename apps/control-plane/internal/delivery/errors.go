package delivery

import (
	"errors"
	"strings"
)

var (
	// ErrInvalid means the input failed validation. ErrInvalidInput matches it.
	ErrInvalid = errors.New("invalid input")
	// ErrNotFound means the record does not exist in the Organization.
	ErrNotFound = errors.New("not found")
	// ErrSlugTaken means another active record in the same scope uses the
	// slug, or, for artifacts, the name.
	ErrSlugTaken = errors.New("slug already in use")
	// ErrAlreadyExists means the record already exists, such as an
	// Application's placement on a Target.
	ErrAlreadyExists = errors.New("already exists")
	// ErrAmbiguous means a slug names more than one record, such as
	// Applications in two Projects, so the caller must use the ID.
	ErrAmbiguous = errors.New("names more than one record; use its ID")
	// ErrConflict means the record changed since the caller read it.
	ErrConflict = errors.New("changed since it was read")
	// ErrArchived means the record, or one it depends on, is archived.
	ErrArchived = errors.New("archived")
	// ErrInUse means active records still depend on this one.
	ErrInUse = errors.New("in use")
	// ErrWorkspaceMismatch means two records that must share a Workspace do not.
	ErrWorkspaceMismatch = errors.New("belongs to another workspace")
	// ErrDefaultChannel means the action is not allowed on an Application's default Channel.
	ErrDefaultChannel = errors.New("not allowed on the default channel")
	// ErrInactiveTeam means the owning Team does not exist, is inactive, or is in another Organization.
	ErrInactiveTeam = errors.New("team is not an active team in the organization")
	// ErrNotTargetAgent means the caller is not the runtime agent that serves the Target.
	ErrNotTargetAgent = errors.New("not the target's agent")
	// ErrReleaseStatus means the Release's status does not allow the change,
	// such as publishing a Release that is already published.
	ErrReleaseStatus = errors.New("not allowed in the release's status")
	// ErrDeploymentStatus means the Deployment's status does not allow the change.
	ErrDeploymentStatus = errors.New("not allowed in the deployment's status")
	// ErrDeploymentInFlight means another Deployment of the Application to
	// the Target is still in flight.
	ErrDeploymentInFlight = errors.New("another deployment is in flight")
	// ErrNotDeploymentAgent means the caller is not the Target's assigned agent.
	ErrNotDeploymentAgent = errors.New("not the deployment target's agent")
	// ErrBuildConflict means a report gives a Build a commit other than the
	// one it was built from.
	ErrBuildConflict = errors.New("conflicts with the recorded build")
	// ErrTagsUnsupported means a Build was named by tag, and tags cannot be
	// resolved to digests yet.
	ErrTagsUnsupported = errors.New("tags cannot be resolved to digests yet; give the digest")
)

// RulesError says why a Release does not meet a Channel's rules, or which
// Channels it could join when none was named. It matches ErrInvalid, as the
// caller can change the Release or name a Channel.
type RulesError struct {
	Message string
	// Reasons are the rules the Release does not meet, in words.
	Reasons []string
	// Candidates names the Channels that admit the Release, when more than
	// one does.
	Candidates []string
}

func (e *RulesError) Error() string {
	msg := e.Message
	if len(e.Reasons) > 0 {
		msg += ": " + strings.Join(e.Reasons, "; ")
	}
	if len(e.Candidates) > 0 {
		msg += ": " + strings.Join(e.Candidates, ", ")
	}
	return msg
}

func (e *RulesError) Is(target error) bool { return target == ErrInvalid }

// ErrInvalidInput describes why input failed validation. It matches ErrInvalid.
type ErrInvalidInput struct {
	Message string
}

func (e *ErrInvalidInput) Error() string {
	return "invalid input: " + e.Message
}

func (e *ErrInvalidInput) Is(target error) bool { return target == ErrInvalid }

func NewErrInvalidInput(message string) *ErrInvalidInput {
	return &ErrInvalidInput{
		Message: message,
	}
}
