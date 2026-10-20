package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sync"
	"time"
)

// Level is where a setting value is stored.
type Level string

const (
	LevelOrganization Level = "organization"
	LevelWorkspace    Level = "workspace"
	LevelProject      Level = "project"
)

// rank orders levels from least to most specific.
func (l Level) rank() int {
	switch l {
	case LevelOrganization:
		return 0
	case LevelWorkspace:
		return 1
	case LevelProject:
		return 2
	}
	return -1
}

// Scope identifies a level: the Organization alone, a Workspace in it, or a
// Project in that Workspace.
type Scope struct {
	OrganizationID string
	WorkspaceID    string
	ProjectID      string
}

// OrganizationScope, WorkspaceScope, and ProjectScope build scopes.
func OrganizationScope(organizationID string) Scope {
	return Scope{OrganizationID: organizationID}
}

func WorkspaceScope(organizationID, workspaceID string) Scope {
	return Scope{OrganizationID: organizationID, WorkspaceID: workspaceID}
}

func ProjectScope(organizationID, workspaceID, projectID string) Scope {
	return Scope{OrganizationID: organizationID, WorkspaceID: workspaceID, ProjectID: projectID}
}

// Level reports the scope's level.
func (s Scope) Level() Level {
	switch {
	case s.ProjectID != "":
		return LevelProject
	case s.WorkspaceID != "":
		return LevelWorkspace
	default:
		return LevelOrganization
	}
}

// ResourceID is the IAM resource the scope's settings are authorized against.
func (s Scope) ResourceID() string {
	switch s.Level() {
	case LevelProject:
		return s.ProjectID
	case LevelWorkspace:
		return s.WorkspaceID
	default:
		return s.OrganizationID
	}
}

func (s Scope) validate() error {
	if s.OrganizationID == "" {
		return fmt.Errorf("%w: scope needs an organization", ErrInvalid)
	}
	if s.ProjectID != "" && s.WorkspaceID == "" {
		return fmt.Errorf("%w: a project scope needs its workspace", ErrInvalid)
	}
	return nil
}

// Setting is one setting value stored at one level.
type Setting struct {
	scope                Scope
	key                  string
	value                json.RawMessage
	version              int64
	updatedByPrincipalID string
	policyDecisionID     string
	createdAt            time.Time
	updatedAt            time.Time
}

// SettingData is a Setting's state as plain values, for repositories.
// Changing it does not change the Setting.
type SettingData struct {
	Scope Scope
	Key   string
	Value json.RawMessage
	// Version increases with every change. Updates compare it so that
	// concurrent changes cannot silently overwrite each other.
	Version              int64
	UpdatedByPrincipalID string
	// PolicyDecisionID is the authorizer's decision for the last change,
	// including any human approval.
	PolicyDecisionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewSetting creates the first value for a key at a level, at version 1.
// Checking the value against the key's definition is the caller's job,
// because the definition's validation may need other capabilities.
func NewSetting(scope Scope, key string, value json.RawMessage, updatedBy, decisionID string) (*Setting, error) {
	if err := scope.validate(); err != nil {
		return nil, err
	}
	if !settingKeyFormat.MatchString(key) {
		return nil, fmt.Errorf("%w: setting key %q must be namespaced, such as capability.name", ErrInvalid, key)
	}
	if err := checkValue(value); err != nil {
		return nil, err
	}

	now := now()

	return &Setting{
		scope:                scope,
		key:                  key,
		value:                bytes.Clone(value),
		version:              1,
		updatedByPrincipalID: updatedBy,
		policyDecisionID:     decisionID,
		createdAt:            now,
		updatedAt:            now,
	}, nil
}

// ReconstituteSetting rebuilds a Setting that is already stored, without
// applying creation rules. It is for repositories only.
func ReconstituteSetting(d SettingData) *Setting {
	return &Setting{
		scope:                d.Scope,
		key:                  d.Key,
		value:                bytes.Clone(d.Value),
		version:              d.Version,
		updatedByPrincipalID: d.UpdatedByPrincipalID,
		policyDecisionID:     d.PolicyDecisionID,
		createdAt:            d.CreatedAt,
		updatedAt:            d.UpdatedAt,
	}
}

func (s *Setting) Scope() Scope                 { return s.scope }
func (s *Setting) Key() string                  { return s.key }
func (s *Setting) Value() json.RawMessage       { return bytes.Clone(s.value) }
func (s *Setting) Version() int64               { return s.version }
func (s *Setting) UpdatedByPrincipalID() string { return s.updatedByPrincipalID }
func (s *Setting) PolicyDecisionID() string     { return s.policyDecisionID }
func (s *Setting) CreatedAt() time.Time         { return s.createdAt }
func (s *Setting) UpdatedAt() time.Time         { return s.updatedAt }

// Data returns the Setting's state.
func (s *Setting) Data() SettingData {
	return SettingData{
		Scope:                s.scope,
		Key:                  s.key,
		Value:                bytes.Clone(s.value),
		Version:              s.version,
		UpdatedByPrincipalID: s.updatedByPrincipalID,
		PolicyDecisionID:     s.policyDecisionID,
		CreatedAt:            s.createdAt,
		UpdatedAt:            s.updatedAt,
	}
}

// Change replaces the value and moves the Setting to its next version,
// recording who changed it and the authorizer's decision.
func (s *Setting) Change(value json.RawMessage, updatedBy, decisionID string) error {
	if err := checkValue(value); err != nil {
		return err
	}

	s.value = bytes.Clone(value)
	s.version++
	s.updatedByPrincipalID = updatedBy
	s.policyDecisionID = decisionID
	s.updatedAt = now()
	return nil
}

// MaxSettingValueBytes is the largest value a setting may hold.
const MaxSettingValueBytes = 16 * 1024

// Definition describes one setting. The capability that owns the setting
// registers it at startup.
type Definition struct {
	// Key is namespaced by the owning capability, such as delivery.default_lifecycle.
	Key         string
	Description string
	// Levels lists where the setting may be set. Values are always resolved
	// from the most specific level that has one.
	Levels []Level
	// Default applies when no level has a value. It may be nil, meaning no value.
	Default json.RawMessage
	// Permission is what the authorizer checks on the level's resource before a change.
	Permission string
	// Validate checks a proposed value, for example that a Lifecycle ID exists
	// in the Workspace. It may be nil when JSON well-formedness is enough.
	Validate func(ctx context.Context, scope Scope, value json.RawMessage) error
}

func (d Definition) allows(l Level) bool { return slices.Contains(d.Levels, l) }

var settingKeyFormat = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Definitions is the registry of setting definitions. It is safe for
// concurrent use.
type Definitions struct {
	mu   sync.RWMutex
	defs map[string]Definition
}

// NewDefinitions returns an empty registry.
func NewDefinitions() *Definitions {
	return &Definitions{defs: map[string]Definition{}}
}

// Register adds a definition. It refuses malformed keys, duplicate keys,
// unknown levels, a missing permission, and a default that is not valid JSON.
func (r *Definitions) Register(d Definition) error {
	if !settingKeyFormat.MatchString(d.Key) {
		return fmt.Errorf("%w: setting key %q must be namespaced, such as capability.name", ErrInvalid, d.Key)
	}
	if len(d.Levels) == 0 {
		return fmt.Errorf("%w: setting %s allows no levels", ErrInvalid, d.Key)
	}
	for _, l := range d.Levels {
		if l.rank() < 0 {
			return fmt.Errorf("%w: setting %s has unknown level %q", ErrInvalid, d.Key, l)
		}
	}
	if d.Permission == "" {
		return fmt.Errorf("%w: setting %s needs a permission", ErrInvalid, d.Key)
	}
	if d.Default != nil {
		if err := checkValue(d.Default); err != nil {
			return fmt.Errorf("setting %s default: %w", d.Key, err)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.defs[d.Key]; ok {
		return fmt.Errorf("%w: setting %s is already registered", ErrInvalid, d.Key)
	}
	r.defs[d.Key] = d
	return nil
}

// Lookup returns the definition for key.
func (r *Definitions) Lookup(key string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.defs[key]
	return d, ok
}

func checkValue(v json.RawMessage) error {
	if len(v) > MaxSettingValueBytes {
		return fmt.Errorf("%w: setting value is larger than %d bytes", ErrInvalid, MaxSettingValueBytes)
	}
	if !json.Valid(v) {
		return fmt.Errorf("%w: setting value is not valid JSON", ErrInvalid)
	}
	return nil
}

// Resolved is a setting's effective value for a scope.
type Resolved struct {
	Key   string
	Value json.RawMessage
	// Level is where Value came from. It is empty when Value is the
	// definition's default, or when there is no value at all.
	Level Level
	// Locked reports that an Organization or Workspace lock fixed the value,
	// so values below the lock's level were ignored.
	Locked bool
}

// FromDefault reports whether the value is the definition's default.
func (r Resolved) FromDefault() bool { return r.Level == "" }

// resolve picks the effective value from the values stored at a scope and
// the levels above it. lockedAt is the level the key is locked at, or empty.
// Values below a lock's level are ignored; otherwise the most specific wins.
func resolve(def Definition, stored []*Setting, lockedAt Level) Resolved {
	limit := LevelProject.rank()
	if lockedAt != "" {
		limit = lockedAt.rank()
	}

	var best *Setting
	for _, s := range stored {
		r := s.scope.Level().rank()
		if r > limit {
			continue
		}
		if best == nil || r > best.scope.Level().rank() {
			best = s
		}
	}

	out := Resolved{Key: def.Key, Locked: lockedAt != ""}
	if best != nil {
		out.Value = best.Value()
		out.Level = best.scope.Level()
	} else if def.Default != nil {
		out.Value = bytes.Clone(def.Default)
	}
	return out
}

// SettingChange is the proposal sent to the authorizer when a setting is
// changed or removed. Value is nil for a removal.
type SettingChange struct {
	Scope           Scope
	Key             string
	Level           Level
	Before          json.RawMessage
	Value           json.RawMessage
	ExpectedVersion int64
}
