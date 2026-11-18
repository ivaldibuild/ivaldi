package policy

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// ApprovalState is where an approval request stands. A request is pending
// exactly while its subject waits; the other states are final.
type ApprovalState string

const (
	ApprovalPending   ApprovalState = "pending"
	ApprovalApproved  ApprovalState = "approved"
	ApprovalRejected  ApprovalState = "rejected"
	ApprovalCancelled ApprovalState = "cancelled"
)

func (s ApprovalState) Valid() bool {
	return slices.Contains([]ApprovalState{ApprovalPending, ApprovalApproved, ApprovalRejected, ApprovalCancelled}, s)
}

// MaxTitleLength is the most characters a request's title may have.
const MaxTitleLength = 255

// Limits on what a request stores.
const (
	maxSubjectIDLength = 300
	maxSummaryBytes    = 64 * 1024
	maxNoteLength      = 2000
	maxReasonLength    = 1000
)

var (
	subjectTypeFormat = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	digestFormat      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Subject is what waits for approval, owned by another capability: its
// registered type, such as delivery.deployment, and its ID there.
type Subject struct {
	Type string
	ID   string
}

func (s Subject) validate() error {
	if !subjectTypeFormat.MatchString(s.Type) {
		return errorf("subject type %q must be namespaced, such as capability.name", s.Type)
	}
	if s.ID == "" || len(s.ID) > maxSubjectIDLength {
		return errorf("subject ID must be 1-%d characters", maxSubjectIDLength)
	}
	return nil
}

// Requirement is access a decider must hold: a permission on an IAM
// resource, or on any resource on its path up to the Organization.
type Requirement struct {
	Permission string `json:"permission"`
	ResourceID string `json:"resource_id"`
	// ResourcePath is the resource and its ancestors, from the resource up
	// to the Organization.
	ResourcePath []string `json:"resource_path"`
}

func (r Requirement) validate() error {
	if r.Permission == "" || r.ResourceID == "" {
		return NewErrInvalidInput("a requirement needs a permission and a resource")
	}
	if len(r.ResourcePath) == 0 || r.ResourcePath[0] != r.ResourceID {
		return errorf("requirement %s on %s needs its resource path, starting with the resource", r.Permission, r.ResourceID)
	}
	return nil
}

// ApprovalRequest records that something waits for a human's approval: the
// subject, the authorization request that made it wait and the decision
// and rule behind it, who asked, what approvers are shown, and what access
// deciding it needs. Once approved, rejected, or cancelled, it keeps who
// closed it and why. Requests are never deleted.
type ApprovalRequest struct {
	aggregate.Root
	organizationID         string
	subject                Subject
	title                  string
	state                  ApprovalState
	decisionID             string
	rule                   string
	reason                 string
	action                 string
	permission             string
	resourceID             string
	proposalDigest         string
	summary                json.RawMessage
	readResourceID         string
	ownerTeamID            *string
	requestedByPrincipalID string
	sessionID              *string
	onBehalfOfPrincipalID  *string
	requirements           []Requirement
	closedByPrincipalID    *string
	closedDecisionID       *string
	note                   *string
	closedAt               *time.Time
	createdAt              time.Time
	updatedAt              time.Time
}

// ApprovalRequestSnapshot is an ApprovalRequest's state as plain values,
// for repositories, events, audit records, and the API.
type ApprovalRequestSnapshot struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	SubjectType    string `json:"subject_type"`
	SubjectID      string `json:"subject_id"`
	// Title names what waits in one line, for lists and messages.
	Title string        `json:"title"`
	State ApprovalState `json:"state" enum:"pending,approved,rejected,cancelled"`
	// DecisionID is the authorizer's requires_approval decision, and Rule
	// and Reason are the rule behind it and why.
	DecisionID string `json:"decision_id"`
	Rule       string `json:"rule"`
	Reason     string `json:"reason"`
	// Action, Permission, and ResourceID describe the authorization request
	// that waits, and ProposalDigest its proposal.
	Action         string          `json:"action"`
	Permission     string          `json:"permission"`
	ResourceID     string          `json:"resource_id"`
	ProposalDigest string          `json:"proposal_digest"`
	Summary        json.RawMessage `json:"summary"`
	// ReadResourceID is where approval.read is checked: whoever can read
	// the subject can read its request.
	ReadResourceID string `json:"read_resource_id"`
	// OwnerTeamID is the Team that owned the subject when it was asked.
	OwnerTeamID            *string `json:"owner_team_id"`
	RequestedByPrincipalID string  `json:"requested_by_principal_id"`
	SessionID              *string `json:"session_id"`
	// OnBehalfOfPrincipalID is the principal the requesting agent Session
	// acts for.
	OnBehalfOfPrincipalID *string       `json:"on_behalf_of_principal_id"`
	Requirements          []Requirement `json:"requirements"`
	ClosedByPrincipalID   *string       `json:"closed_by_principal_id"`
	ClosedDecisionID      *string       `json:"closed_decision_id"`
	Note                  *string       `json:"note"`
	ClosedAt              *time.Time    `json:"closed_at"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
}

// NewApprovalRequestProps describes an ApprovalRequest. A new one leaves
// State, the closing fields, and the timestamps empty; a repository
// loading a stored one sets them.
type NewApprovalRequestProps struct {
	OrganizationID string
	Subject        Subject
	// Title names what waits in one line, such as "Deploy api 1.4.0 to
	// Production / eu-west". The subject's capability writes it.
	Title string
	// Decision is the authorizer's requires_approval decision.
	Decision authorization.Decision
	// Action, Permission, ResourceID, and ProposalDigest describe the
	// authorization request that waits.
	Action                 string
	Permission             string
	ResourceID             string
	ProposalDigest         string
	Summary                json.RawMessage
	ReadResourceID         string
	OwnerTeamID            *string
	RequestedByPrincipalID string
	SessionID              *string
	OnBehalfOfPrincipalID  *string
	Requirements           []Requirement

	State               ApprovalState
	ClosedByPrincipalID *string
	ClosedDecisionID    *string
	Note                *string
	ClosedAt            *time.Time
	CreatedAt           *time.Time
	UpdatedAt           *time.Time
}

// NewApprovalRequest returns an ApprovalRequest, with a new ID unless
// requestID gives a stored one, after checking that it names its subject,
// the decision and rule that made it wait, who asked, and what deciding it
// needs.
func NewApprovalRequest(p NewApprovalRequestProps, requestID *string) (*ApprovalRequest, error) {
	root, err := newRoot(ApprovalRequest{}.Prefix(), requestID)
	if err != nil {
		return nil, err
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	now := clock.Now()
	r := &ApprovalRequest{
		Root:                   root,
		organizationID:         p.OrganizationID,
		subject:                p.Subject,
		title:                  p.Title,
		state:                  p.State,
		decisionID:             p.Decision.ID,
		rule:                   p.Decision.Rule,
		reason:                 p.Decision.Reason,
		action:                 p.Action,
		permission:             p.Permission,
		resourceID:             p.ResourceID,
		proposalDigest:         p.ProposalDigest,
		summary:                slices.Clone(p.Summary),
		readResourceID:         p.ReadResourceID,
		ownerTeamID:            optional.OfPtr(p.OwnerTeamID),
		requestedByPrincipalID: p.RequestedByPrincipalID,
		sessionID:              optional.OfPtr(p.SessionID),
		onBehalfOfPrincipalID:  optional.OfPtr(p.OnBehalfOfPrincipalID),
		requirements:           sortedRequirements(p.Requirements),
		closedByPrincipalID:    optional.OfPtr(p.ClosedByPrincipalID),
		closedDecisionID:       optional.OfPtr(p.ClosedDecisionID),
		note:                   optional.OfPtr(p.Note),
		closedAt:               clock.CopyTime(p.ClosedAt),
		createdAt:              timeOr(p.CreatedAt, now),
		updatedAt:              timeOr(p.UpdatedAt, now),
	}
	if r.state == "" {
		r.state = ApprovalPending
	}
	if r.IsNew() {
		r.RecordEvent(ApprovalRequestCreated{r.Snapshot()})
	}
	return r, nil
}

func (p NewApprovalRequestProps) validate() error {
	if p.OrganizationID == "" {
		return NewErrInvalidInput("an approval request needs an organization")
	}
	if err := p.Subject.validate(); err != nil {
		return err
	}
	if p.Title == "" || utf8.RuneCountInString(p.Title) > MaxTitleLength {
		return errorf("an approval request's title must be 1-%d characters", MaxTitleLength)
	}
	if p.State != "" && !p.State.Valid() {
		return errorf("approval state %q must be pending, approved, rejected, or cancelled", p.State)
	}
	if !id.Valid(authorization.DecisionPrefix, p.Decision.ID) {
		return errorf("%q is not a decision ID", p.Decision.ID)
	}
	if p.Decision.Rule == "" {
		return NewErrInvalidInput("an approval request needs the rule that required approval")
	}
	if p.Decision.Reason == "" || len(p.Decision.Reason) > maxReasonLength {
		return errorf("an approval request's reason must be 1-%d characters", maxReasonLength)
	}
	if p.Action == "" || p.Permission == "" || p.ResourceID == "" {
		return NewErrInvalidInput("an approval request needs the action, permission, and resource it waits for")
	}
	if !digestFormat.MatchString(p.ProposalDigest) {
		return NewErrInvalidInput("an approval request needs its proposal's SHA-256 digest, in hex")
	}
	if !json.Valid(p.Summary) || len(p.Summary) > maxSummaryBytes {
		return errorf("an approval request's summary must be JSON of at most %d bytes", maxSummaryBytes)
	}
	if p.ReadResourceID == "" || p.RequestedByPrincipalID == "" {
		return NewErrInvalidInput("an approval request needs the resource it is read on and the principal that asked")
	}
	if len(p.Requirements) == 0 {
		return NewErrInvalidInput("an approval request needs at least one requirement")
	}
	for _, req := range p.Requirements {
		if err := req.validate(); err != nil {
			return err
		}
	}
	if p.Note != nil && len(*p.Note) > maxNoteLength {
		return errorf("a note must be at most %d characters", maxNoteLength)
	}
	return nil
}

// Prefix is the type prefix of ApprovalRequest IDs.
func (ApprovalRequest) Prefix() string { return "apr" }

func (r *ApprovalRequest) OrganizationID() string         { return r.organizationID }
func (r *ApprovalRequest) Subject() Subject               { return r.subject }
func (r *ApprovalRequest) Title() string                  { return r.title }
func (r *ApprovalRequest) State() ApprovalState           { return r.state }
func (r *ApprovalRequest) DecisionID() string             { return r.decisionID }
func (r *ApprovalRequest) Permission() string             { return r.permission }
func (r *ApprovalRequest) ResourceID() string             { return r.resourceID }
func (r *ApprovalRequest) ReadResourceID() string         { return r.readResourceID }
func (r *ApprovalRequest) RequestedByPrincipalID() string { return r.requestedByPrincipalID }
func (r *ApprovalRequest) Requirements() []Requirement    { return cloneRequirements(r.requirements) }
func (r *ApprovalRequest) Pending() bool                  { return r.state == ApprovalPending }

// AgentSessionRequested reports whether an agent Session asked.
func (r *ApprovalRequest) AgentSessionRequested() bool { return r.sessionID != nil }

// RequestedBy reports whether principalID asked, directly or through an
// agent Session acting for it. Such a principal cannot decide the request.
func (r *ApprovalRequest) RequestedBy(principalID string) bool {
	return principalID == r.requestedByPrincipalID ||
		(r.onBehalfOfPrincipalID != nil && principalID == *r.onBehalfOfPrincipalID)
}

// Snapshot returns the request's state.
func (r *ApprovalRequest) Snapshot() ApprovalRequestSnapshot {
	return ApprovalRequestSnapshot{
		ID:                     r.ID(),
		OrganizationID:         r.organizationID,
		SubjectType:            r.subject.Type,
		SubjectID:              r.subject.ID,
		Title:                  r.title,
		State:                  r.state,
		DecisionID:             r.decisionID,
		Rule:                   r.rule,
		Reason:                 r.reason,
		Action:                 r.action,
		Permission:             r.permission,
		ResourceID:             r.resourceID,
		ProposalDigest:         r.proposalDigest,
		Summary:                slices.Clone(r.summary),
		ReadResourceID:         r.readResourceID,
		OwnerTeamID:            optional.OfPtr(r.ownerTeamID),
		RequestedByPrincipalID: r.requestedByPrincipalID,
		SessionID:              optional.OfPtr(r.sessionID),
		OnBehalfOfPrincipalID:  optional.OfPtr(r.onBehalfOfPrincipalID),
		Requirements:           cloneRequirements(r.requirements),
		ClosedByPrincipalID:    optional.OfPtr(r.closedByPrincipalID),
		ClosedDecisionID:       optional.OfPtr(r.closedDecisionID),
		Note:                   optional.OfPtr(r.note),
		ClosedAt:               clock.CopyTime(r.closedAt),
		CreatedAt:              r.createdAt,
		UpdatedAt:              r.updatedAt,
	}
}

// Approve records that a human approved the request under decisionID.
func (r *ApprovalRequest) Approve(byPrincipalID, decisionID, note string) error {
	return r.close(ApprovalApproved, byPrincipalID, decisionID, note)
}

// Reject records that a human refused the request under decisionID. A
// rejection needs a note, so the requester, often an agent, learns why.
func (r *ApprovalRequest) Reject(byPrincipalID, decisionID, note string) error {
	if note == "" {
		return NewErrInvalidInput("a rejection needs a note saying why")
	}
	return r.close(ApprovalRejected, byPrincipalID, decisionID, note)
}

// Cancel records that the subject stopped waiting other than by a decision,
// such as when it was cancelled, or can no longer be applied.
func (r *ApprovalRequest) Cancel(byPrincipalID, decisionID, note string) error {
	return r.close(ApprovalCancelled, byPrincipalID, decisionID, note)
}

func (r *ApprovalRequest) close(state ApprovalState, byPrincipalID, decisionID, note string) error {
	if !r.Pending() {
		return fmt.Errorf("%w: approval request %s is %s", ErrNotPending, r.ID(), r.state)
	}
	if byPrincipalID == "" || !id.Valid(authorization.DecisionPrefix, decisionID) {
		return NewErrInvalidInput("closing an approval request needs the principal and the decision")
	}
	if len(note) > maxNoteLength {
		return errorf("a note must be at most %d characters", maxNoteLength)
	}
	now := clock.Now()
	r.state = state
	r.closedByPrincipalID = &byPrincipalID
	r.closedDecisionID = &decisionID
	r.note = optional.Of(note)
	r.closedAt = &now
	r.updatedAt = now
	switch state {
	case ApprovalApproved:
		r.RecordEvent(ApprovalRequestApproved{r.Snapshot()})
	case ApprovalRejected:
		r.RecordEvent(ApprovalRequestRejected{r.Snapshot()})
	case ApprovalCancelled:
		r.RecordEvent(ApprovalRequestCancelled{r.Snapshot()})
	}
	return nil
}

// sortedRequirements copies the requirements by permission, then resource:
// their order means nothing.
func sortedRequirements(in []Requirement) []Requirement {
	out := cloneRequirements(in)
	slices.SortFunc(out, func(a, b Requirement) int {
		return cmp.Or(cmp.Compare(a.Permission, b.Permission), cmp.Compare(a.ResourceID, b.ResourceID))
	})
	return out
}

func cloneRequirements(in []Requirement) []Requirement {
	out := make([]Requirement, len(in))
	for i, r := range in {
		out[i] = Requirement{Permission: r.Permission, ResourceID: r.ResourceID, ResourcePath: slices.Clone(r.ResourcePath)}
	}
	return out
}
