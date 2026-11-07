package delivery

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

// RegistryKind names the protocol a Registry speaks. OCI covers container
// images and OCI-hosted Helm charts.
type RegistryKind string

const RegistryOCI RegistryKind = "oci"

// LookupFrom says where lookups against a Registry run: in the control
// plane, or through a data-plane workload when only the customer's
// environment can reach the Registry (ADR 0001, ADR 0008).
type LookupFrom string

const (
	LookupFromControlPlane LookupFrom = "control_plane"
	LookupFromDataPlane    LookupFrom = "data_plane"
)

// Registry is where a Workspace's artifacts are stored, such as an OCI
// registry. Ivaldi uses it only for lookups; clusters pull with their own
// credentials. It refers to its credentials by reference and never holds them.
type Registry struct {
	aggregate.Root
	organizationID string
	workspaceID    string
	slug           string
	displayName    string
	kind           RegistryKind
	url            string
	credentialRef  *string
	lookupFrom     LookupFrom
	lookupPool     *string
	createdAt      time.Time
	updatedAt      time.Time
	deletedAt      *time.Time
}

// RegistrySnapshot is a Registry's state, for repositories, audit records, and
// events. Its JSON form is part of the Registry events' contract.
type RegistrySnapshot struct {
	ID             string       `json:"id"`
	OrganizationID string       `json:"organization_id"`
	WorkspaceID    string       `json:"workspace_id"`
	Slug           string       `json:"slug"`
	DisplayName    string       `json:"display_name"`
	Kind           RegistryKind `json:"kind"`
	URL            string       `json:"url"`
	CredentialRef  *string      `json:"credential_ref"`
	LookupFrom     LookupFrom   `json:"lookup_from"`
	LookupPool     *string      `json:"lookup_pool"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	DeletedAt      *time.Time   `json:"deleted_at"`
}

// NewRegistryProps describes a Registry. A new one leaves the timestamps
// nil; a repository loading a stored one sets them.
type NewRegistryProps struct {
	OrganizationID string
	WorkspaceID    string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Kind        RegistryKind
	// URL is the Registry's https or http address, without credentials.
	URL string
	// CredentialRef points to the credentials capability (ADR 0004).
	CredentialRef *string
	// LookupFrom defaults to LookupFromControlPlane.
	LookupFrom LookupFrom
	// LookupPool names the data-plane pool that runs lookups; it is required
	// with LookupFromDataPlane and not allowed otherwise.
	LookupPool *string
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
	DeletedAt  *time.Time
}

// UpdateRegistryProps changes the fields that are set.
type UpdateRegistryProps struct {
	Slug        *string
	DisplayName *string
	URL         *string
}

// NewRegistry returns a Registry, with a new ID unless registryID gives a stored one.
func NewRegistry(props NewRegistryProps, registryID *string) (*Registry, error) {
	if err := validateRequired("organization ID", props.OrganizationID); err != nil {
		return nil, err
	}
	if err := validateRequired("workspace ID", props.WorkspaceID); err != nil {
		return nil, err
	}
	root, err := newRoot(Registry{}.Prefix(), registryID)
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
	if props.Kind != RegistryOCI {
		return nil, NewErrInvalidInput(fmt.Sprintf("registry kind %q is not supported; use oci", props.Kind))
	}
	if err := validateRegistryURL(props.URL); err != nil {
		return nil, err
	}
	if props.LookupFrom == "" {
		props.LookupFrom = LookupFromControlPlane
	}
	pool := optional.OfPtr(props.LookupPool)
	if err := validateLookup(props.LookupFrom, pool); err != nil {
		return nil, err
	}

	now := clock.Now()
	r := &Registry{
		Root:           root,
		organizationID: props.OrganizationID,
		workspaceID:    props.WorkspaceID,
		slug:           s,
		displayName:    props.DisplayName,
		kind:           props.Kind,
		url:            props.URL,
		credentialRef:  optional.OfPtr(props.CredentialRef),
		lookupFrom:     props.LookupFrom,
		lookupPool:     pool,
		createdAt:      timeOr(props.CreatedAt, now),
		updatedAt:      timeOr(props.UpdatedAt, now),
		deletedAt:      clock.CopyTime(props.DeletedAt),
	}
	if r.IsNew() {
		r.RecordEvent(RegistryCreated{r.Snapshot()})
	}
	return r, nil
}

// validateRegistryURL accepts an absolute https or http URL with a host. It
// refuses credentials in the URL, because no secrets are stored.
func validateRegistryURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return NewErrInvalidInput(fmt.Sprintf("registry URL %q must be an https or http URL", raw))
	}
	if u.User != nil {
		return NewErrInvalidInput("a registry URL must not contain credentials; use a credential reference")
	}
	// Checked on the raw string, so an empty trailing "?" or "#" is refused too.
	if strings.ContainsAny(raw, "?#") {
		return NewErrInvalidInput("a registry URL must not have a query or fragment")
	}
	return nil
}

func validateLookup(from LookupFrom, pool *string) error {
	switch from {
	case LookupFromControlPlane:
		if pool != nil {
			return NewErrInvalidInput("a lookup pool applies only to lookups from the data plane")
		}
	case LookupFromDataPlane:
		if pool == nil || utf8.RuneCountInString(*pool) > maxPoolLength {
			return NewErrInvalidInput(fmt.Sprintf("lookups from the data plane need a pool name of 1-%d characters", maxPoolLength))
		}
	default:
		return NewErrInvalidInput(fmt.Sprintf("lookup_from %q must be control_plane or data_plane", from))
	}
	return nil
}

func (Registry) Prefix() string            { return "reg" }
func (r *Registry) OrganizationID() string { return r.organizationID }
func (r *Registry) WorkspaceID() string    { return r.workspaceID }
func (r *Registry) Slug() string           { return r.slug }
func (r *Registry) DisplayName() string    { return r.displayName }
func (r *Registry) Kind() RegistryKind     { return r.kind }
func (r *Registry) URL() string            { return r.url }
func (r *Registry) CredentialRef() *string { return copyString(r.credentialRef) }
func (r *Registry) LookupFrom() LookupFrom { return r.lookupFrom }
func (r *Registry) LookupPool() *string    { return copyString(r.lookupPool) }
func (r *Registry) CreatedAt() time.Time   { return r.createdAt }
func (r *Registry) UpdatedAt() time.Time   { return r.updatedAt }
func (r *Registry) DeletedAt() *time.Time  { return clock.CopyTime(r.deletedAt) }

func (r *Registry) Archived() bool { return r.deletedAt != nil }

func (r *Registry) Snapshot() RegistrySnapshot {
	return RegistrySnapshot{
		ID:             r.ID(),
		OrganizationID: r.organizationID,
		WorkspaceID:    r.workspaceID,
		Slug:           r.slug,
		DisplayName:    r.displayName,
		Kind:           r.kind,
		URL:            r.url,
		CredentialRef:  copyString(r.credentialRef),
		LookupFrom:     r.lookupFrom,
		LookupPool:     copyString(r.lookupPool),
		CreatedAt:      r.createdAt,
		UpdatedAt:      r.updatedAt,
		DeletedAt:      clock.CopyTime(r.deletedAt),
	}
}

func (r *Registry) checkActive() error {
	if r.Archived() {
		return fmt.Errorf("%w: registry %s", ErrArchived, r.ID())
	}
	return nil
}

// Update changes an active Registry. Nothing changes if any field is invalid.
func (r *Registry) Update(props UpdateRegistryProps) error {
	if err := r.checkActive(); err != nil {
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
	if props.URL != nil {
		if err := validateRegistryURL(*props.URL); err != nil {
			return err
		}
	}

	changed := anyTrue(
		set(&r.slug, props.Slug),
		set(&r.displayName, props.DisplayName),
		set(&r.url, props.URL),
	)
	r.updatedAt = clock.Now()
	if changed {
		r.RecordEvent(RegistryUpdated{r.Snapshot()})
	}
	return nil
}

// SetCredential points the Registry at a credential in the credentials capability.
func (r *Registry) SetCredential(credentialRef string) error {
	if err := r.checkActive(); err != nil {
		return err
	}
	if credentialRef == "" {
		return NewErrInvalidInput("a credential reference is required; use ClearCredential to remove it")
	}
	changed := !equalPtr(r.credentialRef, &credentialRef)
	r.credentialRef = &credentialRef
	r.updatedAt = clock.Now()
	if changed {
		r.RecordEvent(RegistryCredentialSet{r.Snapshot()})
	}
	return nil
}

// ClearCredential makes the Registry anonymous.
func (r *Registry) ClearCredential() error {
	if err := r.checkActive(); err != nil {
		return err
	}
	changed := r.credentialRef != nil
	r.credentialRef = nil
	r.updatedAt = clock.Now()
	if changed {
		r.RecordEvent(RegistryCredentialCleared{r.Snapshot()})
	}
	return nil
}

// SetLookup changes where lookups run. pool is required for
// LookupFromDataPlane and must be empty otherwise.
func (r *Registry) SetLookup(from LookupFrom, pool string) error {
	if err := r.checkActive(); err != nil {
		return err
	}
	p := optional.OfPtr(&pool)
	if err := validateLookup(from, p); err != nil {
		return err
	}
	changed := from != r.lookupFrom || !equalPtr(p, r.lookupPool)
	r.lookupFrom = from
	r.lookupPool = p
	r.updatedAt = clock.Now()
	if changed {
		r.RecordEvent(RegistryLookupChanged{r.Snapshot()})
	}
	return nil
}

// Archive soft-deletes the Registry. Archiving an archived one does nothing.
func (r *Registry) Archive() {
	if !r.Archived() {
		now := clock.Now()
		r.deletedAt = &now
		r.updatedAt = now
		r.RecordEvent(RegistryArchived{r.Snapshot()})
	}
}

// Restore undoes Archive. Restoring an active Registry does nothing.
func (r *Registry) Restore() {
	if r.Archived() {
		r.deletedAt = nil
		r.updatedAt = clock.Now()
		r.RecordEvent(RegistryRestored{r.Snapshot()})
	}
}
