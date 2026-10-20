package delivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/release"
)

// Channel is one release strategy for an Application, such as mainline or
// hotfix. Its rules decide which Releases may join it, and its Lifecycle
// decides where they may be deployed. It owns its rules: they change only
// through the Channel's methods. Every Application has exactly one default
// Channel, which cannot be archived.
type Channel struct {
	id             string
	organizationID string
	applicationID  string
	lifecycleID    string
	slug           string
	displayName    string
	description    *string
	isDefault      bool
	rules          []*ChannelRule
	createdAt      time.Time
	updatedAt      time.Time
	archivedAt     *time.Time
}

// ChannelRule is one condition a Release must meet to join its Channel.
type ChannelRule struct {
	id         string
	kind       release.RuleKind
	artifactID *string
	config     json.RawMessage
	createdAt  time.Time
	updatedAt  time.Time
}

// ChannelSnapshot is a Channel's state, for repositories and audit records.
type ChannelSnapshot struct {
	ID             string
	OrganizationID string
	ApplicationID  string
	LifecycleID    string
	Slug           string
	DisplayName    string
	Description    *string
	IsDefault      bool
	Rules          []ChannelRuleSnapshot
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ArchivedAt     *time.Time
}

// ChannelRuleSnapshot is a rule's state.
type ChannelRuleSnapshot struct {
	ID         string
	Kind       release.RuleKind
	ArtifactID *string
	Config     json.RawMessage
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewChannelProps describes a Channel. A new one leaves the timestamps nil;
// a repository loading a stored one sets them.
type NewChannelProps struct {
	OrganizationID string
	ApplicationID  string
	LifecycleID    string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description *string
	IsDefault   bool
	Rules       []NewChannelRuleProps
	CreatedAt   *time.Time
	UpdatedAt   *time.Time
	ArchivedAt  *time.Time
}

// NewChannelRuleProps describes a rule given to NewChannel. A new one leaves
// ID and the timestamps nil; a stored one sets them.
type NewChannelRuleProps struct {
	ID         *string
	Kind       release.RuleKind
	ArtifactID *string
	Config     json.RawMessage
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

// ChannelRuleProps is the content of a rule added to or changed in a Channel.
type ChannelRuleProps struct {
	Kind release.RuleKind
	// ArtifactID scopes the rule to one of the Application's artifacts.
	ArtifactID *string
	// Config holds the rule's parameters, as release.VersionRule or
	// release.GitRefRule JSON.
	Config json.RawMessage
}

// UpdateChannelProps changes the fields that are set. An empty Description removes it.
type UpdateChannelProps struct {
	Slug        *string
	DisplayName *string
	Description *string
}

// NewChannel returns a Channel, with new IDs for it and its rules unless they are given.
func NewChannel(props NewChannelProps, channelID *string) (*Channel, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if !id.Valid(Application{}.Prefix(), props.ApplicationID) {
		return nil, NewErrInvalidInput("a channel needs an application ID")
	}
	if !id.Valid(Lifecycle{}.Prefix(), props.LifecycleID) {
		return nil, NewErrInvalidInput("a channel needs a lifecycle ID")
	}
	if err := validateOwnID(Channel{}.Prefix(), channelID); err != nil {
		return nil, err
	}
	if err := validateDisplayName(props.DisplayName); err != nil {
		return nil, err
	}
	s, err := slugOrDerived(props.Slug, props.DisplayName)
	if err != nil {
		return nil, err
	}
	if props.IsDefault && props.ArchivedAt != nil {
		return nil, fmt.Errorf("%w: a default channel cannot be archived", ErrDefaultChannel)
	}
	if len(props.Rules) > maxChannelRules {
		return nil, NewErrInvalidInput(fmt.Sprintf("a channel can have at most %d rules", maxChannelRules))
	}

	now := clock.Now()
	c := &Channel{
		organizationID: props.OrganizationID,
		applicationID:  props.ApplicationID,
		lifecycleID:    props.LifecycleID,
		slug:           s,
		displayName:    props.DisplayName,
		description:    optionalString(props.Description),
		isDefault:      props.IsDefault,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		archivedAt:     clock.CopyTime(props.ArchivedAt),
	}
	for _, r := range props.Rules {
		rule, err := newChannelRule(r, now)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(c.rules, func(o *ChannelRule) bool { return o.id == rule.id }) {
			return nil, NewErrInvalidInput(fmt.Sprintf("rule %s appears twice", rule.id))
		}
		c.rules = append(c.rules, rule)
	}
	if channelID != nil {
		c.id = *channelID
	}
	id.Assign(&c.id, c)
	return c, nil
}

func newChannelRule(p NewChannelRuleProps, now time.Time) (*ChannelRule, error) {
	if err := validateOwnID(ChannelRule{}.Prefix(), p.ID); err != nil {
		return nil, err
	}
	content, err := ChannelRuleProps{Kind: p.Kind, ArtifactID: p.ArtifactID, Config: p.Config}.normalize()
	if err != nil {
		return nil, err
	}
	r := &ChannelRule{
		kind:       content.Kind,
		artifactID: content.ArtifactID,
		config:     content.Config,
		createdAt:  timeOr(p.CreatedAt, now),
		updatedAt:  timeOr(p.UpdatedAt, now),
	}
	if p.ID != nil {
		r.id = *p.ID
	}
	id.Assign(&r.id, r)
	return r, nil
}

// normalize checks a rule and returns it with its configuration in
// canonical form, so equal rules store equal JSON.
func (p ChannelRuleProps) normalize() (ChannelRuleProps, error) {
	artifact := optionalString(p.ArtifactID)
	if artifact != nil && !id.Valid(Artifact{}.Prefix(), *artifact) {
		return ChannelRuleProps{}, NewErrInvalidInput(fmt.Sprintf("%q is not an artifact ID", *artifact))
	}
	config, err := release.NormalizeRuleConfig(p.Kind, p.Config)
	if err != nil {
		return ChannelRuleProps{}, NewErrInvalidInput(err.Error())
	}
	return ChannelRuleProps{Kind: p.Kind, ArtifactID: artifact, Config: config}, nil
}

func (Channel) Prefix() string     { return "chn" }
func (ChannelRule) Prefix() string { return "chr" }

func (c *Channel) ID() string             { return c.id }
func (c *Channel) OrganizationID() string { return c.organizationID }
func (c *Channel) ApplicationID() string  { return c.applicationID }
func (c *Channel) LifecycleID() string    { return c.lifecycleID }
func (c *Channel) Slug() string           { return c.slug }
func (c *Channel) DisplayName() string    { return c.displayName }
func (c *Channel) Description() *string   { return copyString(c.description) }
func (c *Channel) IsDefault() bool        { return c.isDefault }
func (c *Channel) CreatedAt() time.Time   { return c.createdAt }
func (c *Channel) UpdatedAt() time.Time   { return c.updatedAt }
func (c *Channel) ArchivedAt() *time.Time { return clock.CopyTime(c.archivedAt) }

func (c *Channel) Archived() bool { return c.archivedAt != nil }

// Rules returns the Channel's rules in the order they were added.
func (c *Channel) Rules() []ChannelRuleSnapshot {
	out := make([]ChannelRuleSnapshot, len(c.rules))
	for i, r := range c.rules {
		out[i] = r.snapshot()
	}
	return out
}

// ArtifactIDs returns the artifacts the Channel's rules are scoped to.
func (c *Channel) ArtifactIDs() []string {
	var out []string
	for _, r := range c.rules {
		if r.artifactID != nil && !slices.Contains(out, *r.artifactID) {
			out = append(out, *r.artifactID)
		}
	}
	return out
}

func (r *ChannelRule) snapshot() ChannelRuleSnapshot {
	return ChannelRuleSnapshot{
		ID:         r.id,
		Kind:       r.kind,
		ArtifactID: copyString(r.artifactID),
		Config:     bytes.Clone(r.config),
		CreatedAt:  r.createdAt,
		UpdatedAt:  r.updatedAt,
	}
}

func (c *Channel) Snapshot() ChannelSnapshot {
	return ChannelSnapshot{
		ID:             c.id,
		OrganizationID: c.organizationID,
		ApplicationID:  c.applicationID,
		LifecycleID:    c.lifecycleID,
		Slug:           c.slug,
		DisplayName:    c.displayName,
		Description:    copyString(c.description),
		IsDefault:      c.isDefault,
		Rules:          c.Rules(),
		CreatedAt:      c.createdAt,
		UpdatedAt:      c.updatedAt,
		ArchivedAt:     clock.CopyTime(c.archivedAt),
	}
}

func (c *Channel) checkActive() error {
	if c.Archived() {
		return fmt.Errorf("%w: channel %s", ErrArchived, c.id)
	}
	return nil
}

// Update changes an active Channel. Nothing changes if any field is invalid.
func (c *Channel) Update(props UpdateChannelProps) error {
	if err := c.checkActive(); err != nil {
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

	if props.Slug != nil {
		c.slug = *props.Slug
	}
	if props.DisplayName != nil {
		c.displayName = *props.DisplayName
	}
	if props.Description != nil {
		c.description = optionalString(props.Description)
	}
	c.updatedAt = clock.Now()
	return nil
}

// UseLifecycle makes the Channel follow another Lifecycle. The caller checks
// that it is active and in the Application's Workspace.
func (c *Channel) UseLifecycle(lifecycleID string) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if !id.Valid(Lifecycle{}.Prefix(), lifecycleID) {
		return NewErrInvalidInput(fmt.Sprintf("%q is not a lifecycle ID", lifecycleID))
	}
	c.lifecycleID = lifecycleID
	c.updatedAt = clock.Now()
	return nil
}

// AddRule adds a rule and returns it. The caller checks that a scoped
// artifact belongs to the Application.
func (c *Channel) AddRule(props ChannelRuleProps) (ChannelRuleSnapshot, error) {
	if err := c.checkActive(); err != nil {
		return ChannelRuleSnapshot{}, err
	}
	if len(c.rules) >= maxChannelRules {
		return ChannelRuleSnapshot{}, NewErrInvalidInput(fmt.Sprintf("a channel can have at most %d rules", maxChannelRules))
	}
	now := clock.Now()
	rule, err := newChannelRule(NewChannelRuleProps{Kind: props.Kind, ArtifactID: props.ArtifactID, Config: props.Config}, now)
	if err != nil {
		return ChannelRuleSnapshot{}, err
	}
	c.rules = append(slices.Clone(c.rules), rule)
	c.updatedAt = now
	return rule.snapshot(), nil
}

// UpdateRule replaces a rule's kind, scope, and configuration.
func (c *Channel) UpdateRule(ruleID string, props ChannelRuleProps) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	i, err := c.ruleIndex(ruleID)
	if err != nil {
		return err
	}
	content, err := props.normalize()
	if err != nil {
		return err
	}
	now := clock.Now()
	updated := *c.rules[i]
	updated.kind = content.Kind
	updated.artifactID = content.ArtifactID
	updated.config = content.Config
	updated.updatedAt = now

	rules := slices.Clone(c.rules)
	rules[i] = &updated
	c.rules = rules
	c.updatedAt = now
	return nil
}

// RemoveRule removes a rule.
func (c *Channel) RemoveRule(ruleID string) error {
	if err := c.checkActive(); err != nil {
		return err
	}
	i, err := c.ruleIndex(ruleID)
	if err != nil {
		return err
	}
	c.rules = slices.Delete(slices.Clone(c.rules), i, i+1)
	c.updatedAt = clock.Now()
	return nil
}

func (c *Channel) ruleIndex(ruleID string) (int, error) {
	i := slices.IndexFunc(c.rules, func(r *ChannelRule) bool { return r.id == ruleID })
	if i < 0 {
		return 0, fmt.Errorf("%w: rule %s in channel %s", ErrNotFound, ruleID, c.id)
	}
	return i, nil
}

// MakeDefault makes the Channel its Application's default. The caller
// clears the previous default first.
func (c *Channel) MakeDefault() error {
	if err := c.checkActive(); err != nil {
		return err
	}
	if !c.isDefault {
		c.isDefault = true
		c.updatedAt = clock.Now()
	}
	return nil
}

// ClearDefault stops the Channel being its Application's default.
func (c *Channel) ClearDefault() {
	if c.isDefault {
		c.isDefault = false
		c.updatedAt = clock.Now()
	}
}

// Archive archives a Channel that is not the default. Archiving an archived
// one does nothing.
func (c *Channel) Archive() error {
	if c.isDefault {
		return fmt.Errorf("%w: make another channel the default first", ErrDefaultChannel)
	}
	if !c.Archived() {
		now := clock.Now()
		c.archivedAt = &now
		c.updatedAt = now
	}
	return nil
}

// Restore undoes Archive. Restoring an active Channel does nothing.
func (c *Channel) Restore() {
	if c.Archived() {
		c.archivedAt = nil
		c.updatedAt = clock.Now()
	}
}
