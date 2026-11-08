package provenance

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// Change is a set of commits in a Source and the Session and Work Item that
// produced them. A commit belongs to at most one Change in its Source, so
// release notes map it once.
type Change struct {
	aggregate.Root
	organizationID string
	sourceID       string
	sessionID      *string
	workItemID     *string
	title          string
	externalRef    *string
	commitSHAs     []string
	createdBy      *string
	createdAt      time.Time
	updatedAt      time.Time
}

// ChangeSnapshot is a Change's state, for repositories, audit records, and
// events. Its JSON form is part of the Change events' contract.
type ChangeSnapshot struct {
	ID                   string    `json:"id"`
	OrganizationID       string    `json:"organization_id"`
	SourceID             string    `json:"source_id"`
	SessionID            *string   `json:"session_id"`
	WorkItemID           *string   `json:"work_item_id"`
	Title                string    `json:"title"`
	ExternalRef          *string   `json:"external_ref"`
	CommitSHAs           []string  `json:"commit_shas"`
	CreatedByPrincipalID *string   `json:"created_by_principal_id"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// NewChangeProps describes a Change. A new one leaves the timestamps nil; a
// repository loading a stored one sets them.
type NewChangeProps struct {
	OrganizationID       string
	SourceID             string
	SessionID            *string
	WorkItemID           *string
	Title                string
	ExternalRef          *string
	CommitSHAs           []string
	CreatedByPrincipalID *string
	CreatedAt            *time.Time
	UpdatedAt            *time.Time
}

// NewChange returns a Change, with a new ID unless changeID gives a stored one.
func NewChange(props NewChangeProps, changeID *string) (*Change, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateRequired("source ID", props.SourceID); err != nil {
		return nil, err
	}
	if err := validateOptionalReference("session ID", props.SessionID); err != nil {
		return nil, err
	}
	if err := validateOptionalReference("work item ID", props.WorkItemID); err != nil {
		return nil, err
	}
	if err := validateTitle(props.Title); err != nil {
		return nil, err
	}
	if err := validateText("external ref", deref(props.ExternalRef), maxExternalRefLength, false); err != nil {
		return nil, err
	}
	commits, err := normalizeCommits(props.CommitSHAs)
	if err != nil {
		return nil, err
	}
	root, err := newRoot(Change{}.Prefix(), changeID)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	c := &Change{
		Root:           root,
		organizationID: props.OrganizationID,
		sourceID:       props.SourceID,
		sessionID:      optional.OfPtr(props.SessionID),
		workItemID:     optional.OfPtr(props.WorkItemID),
		title:          strings.TrimSpace(props.Title),
		externalRef:    optional.OfPtr(props.ExternalRef),
		commitSHAs:     commits,
		createdBy:      optional.OfPtr(props.CreatedByPrincipalID),
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
	}
	if c.IsNew() {
		c.RecordEvent(ChangeRecorded{c.Snapshot()})
	}
	return c, nil
}

func normalizeCommits(shas []string) ([]string, error) {
	if len(shas) == 0 {
		return nil, NewErrInvalidInput("a change needs at least one commit")
	}
	if len(shas) > maxChangeCommits {
		return nil, NewErrInvalidInput(fmt.Sprintf("a change may contain at most %d commits", maxChangeCommits))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(shas))
	for _, sha := range shas {
		if len(sha) > maxCommitSHALength || !commitPattern.MatchString(sha) {
			return nil, NewErrInvalidInput(fmt.Sprintf("commit %q must be 7-64 lower-case hex characters", sha))
		}
		if seen[sha] {
			continue
		}
		seen[sha] = true
		out = append(out, sha)
	}
	slices.Sort(out)
	return out, nil
}

func validateTitle(title string) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxTitleLength {
		return NewErrInvalidInput(fmt.Sprintf("title must be 1-%d characters", maxTitleLength))
	}
	return nil
}

func validateOptionalReference(name string, ref *string) error {
	if ref == nil || *ref == "" {
		return nil
	}
	return validateText(name, *ref, maxReferenceLength, false)
}

func (Change) Prefix() string                   { return "chg" }
func (c *Change) OrganizationID() string        { return c.organizationID }
func (c *Change) SourceID() string              { return c.sourceID }
func (c *Change) SessionID() *string            { return copyString(c.sessionID) }
func (c *Change) WorkItemID() *string           { return copyString(c.workItemID) }
func (c *Change) Title() string                 { return c.title }
func (c *Change) ExternalRef() *string          { return copyString(c.externalRef) }
func (c *Change) CommitSHAs() []string          { return slices.Clone(c.commitSHAs) }
func (c *Change) CreatedByPrincipalID() *string { return copyString(c.createdBy) }
func (c *Change) CreatedAt() time.Time          { return c.createdAt }
func (c *Change) UpdatedAt() time.Time          { return c.updatedAt }

func (c *Change) Snapshot() ChangeSnapshot {
	return ChangeSnapshot{
		ID:                   c.ID(),
		OrganizationID:       c.organizationID,
		SourceID:             c.sourceID,
		SessionID:            copyString(c.sessionID),
		WorkItemID:           copyString(c.workItemID),
		Title:                c.title,
		ExternalRef:          copyString(c.externalRef),
		CommitSHAs:           slices.Clone(c.commitSHAs),
		CreatedByPrincipalID: copyString(c.createdBy),
		CreatedAt:            c.createdAt,
		UpdatedAt:            c.updatedAt,
	}
}
