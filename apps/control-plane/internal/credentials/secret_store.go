package credentials

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
	"github.com/ivaldibuild/ivaldi/pkg/secretstore"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

// environmentPrefix is the ID prefix of delivery's Environments, which a
// Secret Store names without a foreign key.
const environmentPrefix = "env"

// SecretStore is a secret manager a runtime agent can read: Kubernetes
// Secrets in a Target's namespace, AWS Secrets Manager, Vault, or Doppler.
// Its settings say where the store is and its access how it is opened. It
// never holds a credential: in agent mode the agent authenticates with its
// own identity, or with a credential its operator configured under the name
// the access gives (ADR 0004, ADR 0017).
type SecretStore struct {
	aggregate.Root
	organizationID string
	workspaceID    string
	slug           string
	displayName    string
	kind           secretstore.Kind
	settings       Settings
	access         Access
	environmentIDs []string
	createdAt      time.Time
	updatedAt      time.Time
	archivedAt     *time.Time
}

// credentialName is what a credential's name may be: a DNS subdomain, so a
// Kubernetes Secret's name can serve as one.
var credentialName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// SecretStoreSnapshot is a Secret Store's state, for repositories, audit
// records, and events. Its JSON form is part of the events' contract.
type SecretStoreSnapshot struct {
	ID             string           `json:"id"`
	OrganizationID string           `json:"organization_id"`
	WorkspaceID    string           `json:"workspace_id"`
	Slug           string           `json:"slug"`
	DisplayName    string           `json:"display_name"`
	Kind           secretstore.Kind `json:"kind" enum:"kubernetes,aws_secrets_manager,vault,doppler"`
	Settings       Settings         `json:"settings"`
	Access         Access           `json:"access"`
	// EnvironmentIDs are the Environments whose Deployments may read the
	// store; empty means every Environment in the Workspace.
	EnvironmentIDs []string   `json:"environment_ids"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ArchivedAt     *time.Time `json:"archived_at"`
}

// NewSecretStoreProps describes a Secret Store. A new one leaves the
// timestamps nil; a repository loading a stored one sets them.
type NewSecretStoreProps struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Kind        secretstore.Kind
	Settings    Settings
	// Access defaults to agent mode when its mode is empty.
	Access         Access
	EnvironmentIDs []string
	CreatedAt      *time.Time
	UpdatedAt      *time.Time
	ArchivedAt     *time.Time
}

// UpdateSecretStoreProps changes the fields that are set. A store's kind
// never changes: secret references are written for it.
type UpdateSecretStoreProps struct {
	Slug        *string
	DisplayName *string
	Settings    *Settings
	Access      *Access
	// EnvironmentIDs replaces the list when set; an empty list allows
	// every Environment in the Workspace.
	EnvironmentIDs *[]string
}

// NewSecretStore returns a Secret Store, with a new ID unless one is given.
func NewSecretStore(props NewSecretStoreProps, storeID *string) (*SecretStore, error) {
	if props.OrganizationID == "" || props.WorkspaceID == "" {
		return nil, NewErrInvalidInput("organization and workspace IDs are required")
	}
	root, err := aggregate.NewRoot(SecretStore{}.Prefix(), storeID)
	if err != nil {
		return nil, NewErrInvalidInput(err.Error())
	}
	if err := validateDisplayName(props.DisplayName); err != nil {
		return nil, err
	}
	s := props.Slug
	if s == "" {
		s = slug.Create(props.DisplayName)
	}
	if err := validateSlug(s); err != nil {
		return nil, err
	}
	if !slices.Contains(secretstore.Kinds, props.Kind) {
		return nil, NewErrInvalidInput(fmt.Sprintf("secret store kind %q must be kubernetes, aws_secrets_manager, vault, or doppler", props.Kind))
	}
	settings, err := props.Settings.normalize(props.Kind)
	if err != nil {
		return nil, err
	}
	access, err := props.Access.normalize(props.Kind, settings)
	if err != nil {
		return nil, err
	}
	environments, err := normalizeEnvironments(props.Kind, props.EnvironmentIDs)
	if err != nil {
		return nil, err
	}

	now := clock.Now()
	store := &SecretStore{
		Root:           root,
		organizationID: props.OrganizationID,
		workspaceID:    props.WorkspaceID,
		slug:           s,
		displayName:    props.DisplayName,
		kind:           props.Kind,
		settings:       settings,
		access:         access,
		environmentIDs: environments,
		createdAt:      optional.Or(props.CreatedAt, now),
		updatedAt:      optional.Or(props.UpdatedAt, now),
		archivedAt:     clock.CopyTime(props.ArchivedAt),
	}
	if store.IsNew() {
		store.RecordEvent(SecretStoreCreated{store.Snapshot()})
	}
	return store, nil
}

func validateDisplayName(s string) error {
	if s == "" || utf8.RuneCountInString(s) > maxDisplayNameLength {
		return NewErrInvalidInput(fmt.Sprintf("display name must be 1-%d characters", maxDisplayNameLength))
	}
	return nil
}

func validateSlug(s string) error {
	if err := slug.Validate(s); err != nil {
		return NewErrInvalidInput(err.Error())
	}
	return nil
}

// normalize checks that the settings are the ones the kind needs, and
// returns a copy holding only those.
func (s Settings) normalize(kind secretstore.Kind) (Settings, error) {
	set := map[secretstore.Kind]bool{
		secretstore.KindAWSSecretsManager: s.AWS != nil,
		secretstore.KindVault:             s.Vault != nil,
		secretstore.KindDoppler:           s.Doppler != nil,
	}
	for other, given := range set {
		if given && other != kind {
			return Settings{}, NewErrInvalidInput(fmt.Sprintf("a %s store takes no %s settings", kind, other))
		}
	}
	required := func(name, value string) error {
		if value == "" || utf8.RuneCountInString(value) > maxSettingLength {
			return NewErrInvalidInput(fmt.Sprintf("%s must be 1-%d characters", name, maxSettingLength))
		}
		return nil
	}
	short := func(name, value string) error {
		if utf8.RuneCountInString(value) > maxSettingLength {
			return NewErrInvalidInput(fmt.Sprintf("%s can have at most %d characters", name, maxSettingLength))
		}
		return nil
	}
	switch kind {
	case secretstore.KindAWSSecretsManager:
		if s.AWS == nil {
			return Settings{}, NewErrInvalidInput("an aws_secrets_manager store needs its region")
		}
		aws := *s.AWS
		if err := required("the AWS region", aws.Region); err != nil {
			return Settings{}, err
		}
		return Settings{AWS: &aws}, nil
	case secretstore.KindVault:
		if s.Vault == nil {
			return Settings{}, NewErrInvalidInput("a vault store needs its address and mount")
		}
		vault := *s.Vault
		err := firstError(
			required("the Vault address", vault.Address), required("the KV v2 mount", vault.Mount),
			short("the Vault namespace", vault.Namespace),
		)
		if err != nil {
			return Settings{}, err
		}
		if u, err := url.Parse(vault.Address); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return Settings{}, NewErrInvalidInput("the Vault address must be an http or https URL")
		}
		return Settings{Vault: &vault}, nil
	case secretstore.KindDoppler:
		// A service token implies its config, so the block is optional;
		// given, it names both.
		if s.Doppler == nil {
			return Settings{}, nil
		}
		doppler := *s.Doppler
		err := firstError(required("the Doppler project", doppler.Project), required("the Doppler config", doppler.Config))
		if err != nil {
			return Settings{}, err
		}
		return Settings{Doppler: &doppler}, nil
	}
	return Settings{}, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// normalizeEnvironments checks the Environments a store names and returns
// them sorted and without duplicates. A Doppler token serves one config, so
// a Doppler store says which Environments that config is for.
func normalizeEnvironments(kind secretstore.Kind, ids []string) ([]string, error) {
	if len(ids) > maxStoreEnvironments {
		return nil, NewErrInvalidInput(fmt.Sprintf("a secret store can name at most %d environments", maxStoreEnvironments))
	}
	for _, environmentID := range ids {
		if !id.Valid(environmentPrefix, environmentID) {
			return nil, NewErrInvalidInput(fmt.Sprintf("%q is not an environment ID", environmentID))
		}
	}
	out := slices.Compact(slices.Sorted(slices.Values(ids)))
	if out == nil {
		out = []string{}
	}
	if kind == secretstore.KindDoppler && len(out) == 0 {
		return nil, NewErrInvalidInput("a doppler store serves one config, so it names the environments that config is for")
	}
	return out, nil
}

func (SecretStore) Prefix() string { return "sst" }

func (s *SecretStore) OrganizationID() string   { return s.organizationID }
func (s *SecretStore) WorkspaceID() string      { return s.workspaceID }
func (s *SecretStore) Slug() string             { return s.slug }
func (s *SecretStore) DisplayName() string      { return s.displayName }
func (s *SecretStore) Kind() secretstore.Kind   { return s.kind }
func (s *SecretStore) Settings() Settings       { return s.settings.clone() }
func (s *SecretStore) Access() Access           { return s.access }
func (s *SecretStore) EnvironmentIDs() []string { return slices.Clone(s.environmentIDs) }
func (s *SecretStore) CreatedAt() time.Time     { return s.createdAt }
func (s *SecretStore) UpdatedAt() time.Time     { return s.updatedAt }
func (s *SecretStore) ArchivedAt() *time.Time   { return clock.CopyTime(s.archivedAt) }

func (s *SecretStore) Archived() bool { return s.archivedAt != nil }

// Allows reports whether Deployments to an Environment may read the store.
func (s *SecretStore) Allows(environmentID string) bool {
	return len(s.environmentIDs) == 0 || slices.Contains(s.environmentIDs, environmentID)
}

func (s *SecretStore) Snapshot() SecretStoreSnapshot {
	return SecretStoreSnapshot{
		ID:             s.ID(),
		OrganizationID: s.organizationID,
		WorkspaceID:    s.workspaceID,
		Slug:           s.slug,
		DisplayName:    s.displayName,
		Kind:           s.kind,
		Settings:       s.settings.clone(),
		Access:         s.access,
		EnvironmentIDs: slices.Clone(s.environmentIDs),
		CreatedAt:      s.createdAt,
		UpdatedAt:      s.updatedAt,
		ArchivedAt:     clock.CopyTime(s.archivedAt),
	}
}

// Update changes an active Secret Store. Nothing changes if any field is
// invalid. The caller checks the Environments it names.
func (s *SecretStore) Update(props UpdateSecretStoreProps) error {
	if s.Archived() {
		return fmt.Errorf("%w: secret store %s", ErrArchived, s.ID())
	}
	next := *s
	if props.Slug != nil {
		if err := validateSlug(*props.Slug); err != nil {
			return err
		}
		next.slug = *props.Slug
	}
	if props.DisplayName != nil {
		if err := validateDisplayName(*props.DisplayName); err != nil {
			return err
		}
		next.displayName = *props.DisplayName
	}
	if props.Settings != nil {
		settings, err := props.Settings.normalize(s.kind)
		if err != nil {
			return err
		}
		next.settings = settings
	}
	if props.Access != nil || props.Settings != nil {
		access := next.access
		if props.Access != nil {
			access = *props.Access
		}
		// Checked against the settings it will have, which may decide what
		// the access needs.
		access, err := access.normalize(s.kind, next.settings)
		if err != nil {
			return err
		}
		next.access = access
	}
	environments := s.environmentIDs
	if props.EnvironmentIDs != nil {
		environments = *props.EnvironmentIDs
	}
	environments, err := normalizeEnvironments(s.kind, environments)
	if err != nil {
		return err
	}
	next.environmentIDs = environments

	changed := next.slug != s.slug || next.displayName != s.displayName ||
		!next.settings.equal(s.settings) || next.access != s.access || !slices.Equal(next.environmentIDs, s.environmentIDs)
	s.slug, s.displayName, s.settings, s.access, s.environmentIDs = next.slug, next.displayName, next.settings, next.access, next.environmentIDs
	s.updatedAt = clock.Now()
	if changed {
		s.RecordEvent(SecretStoreUpdated{s.Snapshot()})
	}
	return nil
}

// Archive archives the Secret Store, which frees its slug. Archiving an
// archived one does nothing.
func (s *SecretStore) Archive() {
	if !s.Archived() {
		now := clock.Now()
		s.archivedAt = &now
		s.updatedAt = now
		s.RecordEvent(SecretStoreArchived{s.Snapshot()})
	}
}
