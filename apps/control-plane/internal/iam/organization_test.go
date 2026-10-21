package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func orgProps() NewOrganizationProps {
	return NewOrganizationProps{
		Slug:        "acme",
		Name:        "Acme Corporation Ltd",
		DisplayName: "Acme",
		LogoURL:     new("https://acme.example/logo.png"),
		Metadata:    map[string]string{"plan": "team"},
	}
}

func orgNew(t *testing.T) *Organization {
	t.Helper()
	o, err := NewOrganization(orgProps(), nil)
	require.NoError(t, err)
	return o
}

func TestNewOrganization(t *testing.T) {
	clock.Fix(t, t0)

	o, err := NewOrganization(orgProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("org", o.ID()), "a new organization gets an org_ ID: %q", o.ID())
	assert.Equal(t, "acme", o.Slug())
	assert.Equal(t, "Acme Corporation Ltd", o.Name())
	assert.Equal(t, "Acme", o.DisplayName())
	require.NotNil(t, o.LogoURL())
	assert.Equal(t, "https://acme.example/logo.png", *o.LogoURL())
	assert.Equal(t, OrganizationActive, o.Status(), "a new organization is active")
	assert.True(t, o.Active())
	assert.Equal(t, map[string]string{"plan": "team"}, o.Metadata())
	assert.Equal(t, t0, o.CreatedAt())
	assert.Equal(t, t0, o.UpdatedAt())
}

func TestNewOrganizationDefaults(t *testing.T) {
	props := orgProps()
	props.Slug = ""
	props.Name = ""
	props.DisplayName = "Acme Labs (EU)"
	props.LogoURL = new("")

	o, err := NewOrganization(props, nil)

	require.NoError(t, err)
	assert.Equal(t, "acme-labs-eu", o.Slug(), "the slug is derived from the display name")
	assert.Equal(t, "Acme Labs (EU)", o.Name(), "the name defaults to the display name")
	assert.Nil(t, o.LogoURL(), "an empty logo URL means none")
}

func TestNewOrganizationWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("org")
	props := orgProps()
	props.Status = OrganizationSuspended
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)

	o, err := NewOrganization(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, o.ID(), "a given ID is kept")
	assert.Equal(t, OrganizationSuspended, o.Status())
	assert.False(t, o.Active())
	assert.Equal(t, t0, o.CreatedAt(), "given timestamps are kept")
	assert.Equal(t, t1, o.UpdatedAt())
}

func TestNewOrganizationIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewOrganizationProps)
		id     *string
	}{
		{"no display name", func(p *NewOrganizationProps) { p.DisplayName = "" }, nil},
		{"blank display name", func(p *NewOrganizationProps) { p.DisplayName = "   " }, nil},
		{"display name too long", func(p *NewOrganizationProps) { p.DisplayName = strings.Repeat("é", maxNameLength+1) }, nil},
		{"name too long", func(p *NewOrganizationProps) { p.Name = strings.Repeat("a", maxNameLength+1) }, nil},
		{"uppercase slug", func(p *NewOrganizationProps) { p.Slug = "Acme" }, nil},
		{"underivable slug", func(p *NewOrganizationProps) { p.Slug = ""; p.DisplayName = "!!!" }, nil},
		{"http logo", func(p *NewOrganizationProps) { p.LogoURL = new("http://acme.example/logo.png") }, nil},
		{"logo with credentials", func(p *NewOrganizationProps) { p.LogoURL = new("https://u:p@acme.example/logo.png") }, nil},
		{"unknown status", func(p *NewOrganizationProps) { p.Status = "archived" }, nil},
		{"empty metadata key", func(p *NewOrganizationProps) { p.Metadata = map[string]string{"": "x"} }, nil},
		{"too much metadata", func(p *NewOrganizationProps) { p.Metadata = orgManyMetadata(maxMetadataEntries + 1) }, nil},
		{"ID with wrong prefix", func(*NewOrganizationProps) {}, new(id.New("acc"))},
		{"malformed ID", func(*NewOrganizationProps) {}, new("org_nope")},
		{"empty ID", func(*NewOrganizationProps) {}, new("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := orgProps()
			tt.mutate(&props)
			_, err := NewOrganization(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func orgManyMetadata(n int) map[string]string {
	m := map[string]string{}
	for i := range n {
		m[strings.Repeat("k", i+1)] = "v"
	}
	return m
}

func TestOrganizationResource(t *testing.T) {
	o := orgNew(t)

	assert.Equal(t, Resource{ID: o.ID(), OrganizationID: o.ID(), Type: ResourceOrganization}, o.Resource())
}

func TestOrganizationGettersReturnCopies(t *testing.T) {
	o := orgNew(t)

	*o.LogoURL() = "https://evil.example/x.png"
	o.Metadata()["plan"] = "free"
	snap := o.Snapshot()
	*snap.LogoURL = "https://evil.example/x.png"
	snap.Metadata["plan"] = "free"

	assert.Equal(t, "https://acme.example/logo.png", *o.LogoURL())
	assert.Equal(t, "team", o.Metadata()["plan"])
}

func TestNewOrganizationCopiesMetadata(t *testing.T) {
	props := orgProps()
	o, err := NewOrganization(props, nil)
	require.NoError(t, err)

	props.Metadata["plan"] = "free"

	assert.Equal(t, "team", o.Metadata()["plan"])
}

func TestOrganizationSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	o := orgNew(t)

	assert.Equal(t, OrganizationSnapshot{
		ID:          o.ID(),
		Slug:        "acme",
		Name:        "Acme Corporation Ltd",
		DisplayName: "Acme",
		LogoURL:     new("https://acme.example/logo.png"),
		Status:      OrganizationActive,
		Metadata:    map[string]string{"plan": "team"},
		CreatedAt:   t0,
		UpdatedAt:   t0,
	}, o.Snapshot())
}

func TestOrganizationUpdate(t *testing.T) {
	clock.Fix(t, t0)
	o := orgNew(t)
	clock.Fix(t, t1)

	err := o.Update(UpdateOrganizationProps{
		Slug:        new("acme-co"),
		Name:        new("Acme Co"),
		DisplayName: new("Acme Co."),
		LogoURL:     new(""),
		Metadata:    map[string]string{"plan": "enterprise"},
	})

	require.NoError(t, err)
	assert.Equal(t, "acme-co", o.Slug())
	assert.Equal(t, "Acme Co", o.Name())
	assert.Equal(t, "Acme Co.", o.DisplayName())
	assert.Nil(t, o.LogoURL(), "an empty logo URL removes it")
	assert.Equal(t, map[string]string{"plan": "enterprise"}, o.Metadata())
	assert.Equal(t, t0, o.CreatedAt())
	assert.Equal(t, t1, o.UpdatedAt())
}

func TestOrganizationUpdateLeavesUnsetFields(t *testing.T) {
	o := orgNew(t)
	before := o.Snapshot()

	require.NoError(t, o.Update(UpdateOrganizationProps{DisplayName: new("Acme Two")}))

	after := o.Snapshot()
	assert.Equal(t, before.Slug, after.Slug)
	assert.Equal(t, before.Name, after.Name)
	assert.Equal(t, before.LogoURL, after.LogoURL)
	assert.Equal(t, before.Metadata, after.Metadata)
}

func TestOrganizationUpdateIsRefused(t *testing.T) {
	tests := []struct {
		name  string
		props UpdateOrganizationProps
	}{
		{"bad slug", UpdateOrganizationProps{DisplayName: new("New"), Slug: new("Bad Slug")}},
		{"empty name", UpdateOrganizationProps{DisplayName: new("New"), Name: new("")}},
		{"empty display name", UpdateOrganizationProps{Slug: new("new"), DisplayName: new("")}},
		{"http logo", UpdateOrganizationProps{DisplayName: new("New"), LogoURL: new("http://x.example/l.png")}},
		{"bad metadata", UpdateOrganizationProps{DisplayName: new("New"), Metadata: map[string]string{"": "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := orgNew(t)
			before := o.Snapshot()

			assert.ErrorIs(t, o.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, o.Snapshot(), "nothing changes")
		})
	}
}

func TestOrganizationUpdateRefusedWhenDeleted(t *testing.T) {
	o := orgNew(t)
	o.Delete()

	assert.ErrorIs(t, o.Update(UpdateOrganizationProps{DisplayName: new("New")}), ErrStatus)
}

func TestOrganizationSuspendAndReactivate(t *testing.T) {
	clock.Fix(t, t0)
	o := orgNew(t)

	clock.Fix(t, t1)
	require.NoError(t, o.Suspend())
	assert.Equal(t, OrganizationSuspended, o.Status())
	assert.Equal(t, t1, o.UpdatedAt())

	clock.Fix(t, t2)
	require.NoError(t, o.Suspend(), "suspending a suspended organization does nothing")
	assert.Equal(t, t1, o.UpdatedAt())

	require.NoError(t, o.Reactivate())
	assert.Equal(t, OrganizationActive, o.Status())
	assert.Equal(t, t2, o.UpdatedAt())

	clock.Fix(t, t2.Add(1))
	require.NoError(t, o.Reactivate(), "reactivating an active organization does nothing")
	assert.Equal(t, t2, o.UpdatedAt())
}

func TestOrganizationDelete(t *testing.T) {
	clock.Fix(t, t0)
	o := orgNew(t)
	clock.Fix(t, t1)

	o.Delete()

	assert.Equal(t, OrganizationDeleted, o.Status())
	assert.Equal(t, t1, o.UpdatedAt())

	clock.Fix(t, t2)
	o.Delete()
	assert.Equal(t, t1, o.UpdatedAt(), "deleting a deleted organization does nothing")

	assert.ErrorIs(t, o.Suspend(), ErrStatus)
	assert.ErrorIs(t, o.Reactivate(), ErrStatus, "deleting cannot be undone")
	assert.Equal(t, OrganizationDeleted, o.Status())
}
