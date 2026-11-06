package iam

import (
	"fmt"
	"maps"
	"time"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/slug"
)

// OrganizationStatus is where an Organization is in its life. A deleted
// Organization stays as a record, because audit and provenance refer to it.
type OrganizationStatus string

const (
	OrganizationActive    OrganizationStatus = "active"
	OrganizationSuspended OrganizationStatus = "suspended"
	OrganizationDeleted   OrganizationStatus = "deleted"
)

// Organization is the tenant. It owns members, identity-provider
// configuration, and billing, and contains many Workspaces. It is the root
// of IAM's resource hierarchy, under its own ID.
type Organization struct {
	aggregate.Root
	slug        string
	name        string
	displayName string
	logoURL     *string
	status      OrganizationStatus
	metadata    map[string]string
	createdAt   time.Time
	updatedAt   time.Time
}

// OrganizationSnapshot is an Organization's state, for repositories, audit
// records, and events. Its JSON form is part of the Organization events'
// contract.
type OrganizationSnapshot struct {
	ID          string             `json:"id"`
	Slug        string             `json:"slug"`
	Name        string             `json:"name"`
	DisplayName string             `json:"display_name"`
	LogoURL     *string            `json:"logo_url"`
	Status      OrganizationStatus `json:"status"`
	Metadata    map[string]string  `json:"metadata"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// NewOrganizationProps describes an Organization. A new one leaves Status
// and the timestamps empty; a repository loading a stored one sets them.
type NewOrganizationProps struct {
	// Slug is globally unique, and derived from DisplayName when empty.
	Slug string
	// Name is the Organization's full name, such as its legal name; it
	// defaults to DisplayName.
	Name        string
	DisplayName string
	LogoURL     *string
	// Status defaults to OrganizationActive.
	Status    OrganizationStatus
	Metadata  map[string]string
	CreatedAt *time.Time
	UpdatedAt *time.Time
}

// UpdateOrganizationProps changes the fields that are set. An empty LogoURL
// removes it, and a non-nil Metadata replaces it.
type UpdateOrganizationProps struct {
	Slug        *string
	Name        *string
	DisplayName *string
	LogoURL     *string
	Metadata    map[string]string
}

// NewOrganization returns an Organization, with a new ID unless
// organizationID gives a stored one.
func NewOrganization(props NewOrganizationProps, organizationID *string) (*Organization, error) {
	root, err := newRoot(Organization{}.Prefix(), organizationID)
	if err != nil {
		return nil, err
	}
	if err := validateName("display name", props.DisplayName, maxNameLength); err != nil {
		return nil, err
	}
	if props.Name == "" {
		props.Name = props.DisplayName
	}
	if err := validateName("name", props.Name, maxNameLength); err != nil {
		return nil, err
	}
	if props.Slug == "" {
		props.Slug = slug.Create(props.DisplayName)
	}
	if err := validateSlug(props.Slug); err != nil {
		return nil, err
	}
	logo := optionalString(props.LogoURL)
	if logo != nil {
		if err := validateHTTPSURL("logo URL", *logo); err != nil {
			return nil, err
		}
	}
	if props.Status == "" {
		props.Status = OrganizationActive
	}
	if err := validateOrganizationStatus(props.Status); err != nil {
		return nil, err
	}
	if err := validateMetadata(props.Metadata); err != nil {
		return nil, err
	}

	now := clock.Now()
	o := &Organization{
		Root:        root,
		slug:        props.Slug,
		name:        props.Name,
		displayName: props.DisplayName,
		logoURL:     logo,
		status:      props.Status,
		metadata:    copyMetadata(props.Metadata),
		createdAt:   timeOr(props.CreatedAt, now),
		updatedAt:   timeOr(props.UpdatedAt, now),
	}
	if o.IsNew() {
		o.RecordEvent(OrganizationCreated{o.Snapshot()})
	}
	return o, nil
}

func validateOrganizationStatus(s OrganizationStatus) error {
	switch s {
	case OrganizationActive, OrganizationSuspended, OrganizationDeleted:
		return nil
	}
	return NewErrInvalidInput(fmt.Sprintf("organization status %q must be active, suspended, or deleted", s))
}

func (Organization) Prefix() string                 { return "org" }
func (o *Organization) Slug() string                { return o.slug }
func (o *Organization) Name() string                { return o.name }
func (o *Organization) DisplayName() string         { return o.displayName }
func (o *Organization) LogoURL() *string            { return copyString(o.logoURL) }
func (o *Organization) Status() OrganizationStatus  { return o.status }
func (o *Organization) Metadata() map[string]string { return copyMetadata(o.metadata) }
func (o *Organization) CreatedAt() time.Time        { return o.createdAt }
func (o *Organization) UpdatedAt() time.Time        { return o.updatedAt }

func (o *Organization) Active() bool { return o.status == OrganizationActive }

// OrganizationID returns the Organization's own ID: an Organization is its
// own tenant, so its changes are written and audited like any aggregate's.
func (o *Organization) OrganizationID() string { return o.ID() }

// Resource returns the Organization's root entry in IAM's resource hierarchy.
func (o *Organization) Resource() Resource {
	return Resource{ID: o.ID(), OrganizationID: o.ID(), Type: ResourceOrganization}
}

func (o *Organization) Snapshot() OrganizationSnapshot {
	return OrganizationSnapshot{
		ID:          o.ID(),
		Slug:        o.slug,
		Name:        o.name,
		DisplayName: o.displayName,
		LogoURL:     copyString(o.logoURL),
		Status:      o.status,
		Metadata:    copyMetadata(o.metadata),
		CreatedAt:   o.createdAt,
		UpdatedAt:   o.updatedAt,
	}
}

func (o *Organization) checkNotDeleted() error {
	if o.status == OrganizationDeleted {
		return fmt.Errorf("%w: organization %s is deleted", ErrStatus, o.ID())
	}
	return nil
}

// Update changes an Organization that is not deleted. Nothing changes if
// any field is invalid.
func (o *Organization) Update(props UpdateOrganizationProps) error {
	if err := o.checkNotDeleted(); err != nil {
		return err
	}
	if props.Slug != nil {
		if err := validateSlug(*props.Slug); err != nil {
			return err
		}
	}
	if props.Name != nil {
		if err := validateName("name", *props.Name, maxNameLength); err != nil {
			return err
		}
	}
	if props.DisplayName != nil {
		if err := validateName("display name", *props.DisplayName, maxNameLength); err != nil {
			return err
		}
	}
	logo := optionalString(props.LogoURL)
	if logo != nil {
		if err := validateHTTPSURL("logo URL", *logo); err != nil {
			return err
		}
	}
	if err := validateMetadata(props.Metadata); err != nil {
		return err
	}

	changed := anyTrue(
		set(&o.slug, props.Slug),
		set(&o.name, props.Name),
		set(&o.displayName, props.DisplayName),
		setOptional(&o.logoURL, props.LogoURL),
	)
	if props.Metadata != nil && !maps.Equal(props.Metadata, o.metadata) {
		o.metadata = copyMetadata(props.Metadata)
		changed = true
	}
	o.updatedAt = clock.Now()
	if changed {
		o.RecordEvent(OrganizationUpdated{o.Snapshot()})
	}
	return nil
}

// Suspend stops everyone in an active Organization from acting in it.
// Suspending a suspended one does nothing.
func (o *Organization) Suspend() error {
	if err := o.checkNotDeleted(); err != nil {
		return err
	}
	o.setStatus(OrganizationSuspended)
	return nil
}

// Reactivate undoes Suspend. Reactivating an active Organization does nothing.
func (o *Organization) Reactivate() error {
	if err := o.checkNotDeleted(); err != nil {
		return err
	}
	o.setStatus(OrganizationActive)
	return nil
}

// Delete marks the Organization deleted, which cannot be undone. Deleting a
// deleted one does nothing.
func (o *Organization) Delete() {
	o.setStatus(OrganizationDeleted)
}

// setStatus moves the Organization to status, recording the change.
func (o *Organization) setStatus(status OrganizationStatus) {
	if o.status != status {
		previous := o.status
		o.status = status
		o.updatedAt = clock.Now()
		o.RecordEvent(OrganizationStatusChanged{Organization: o.Snapshot(), PreviousStatus: previous})
	}
}
