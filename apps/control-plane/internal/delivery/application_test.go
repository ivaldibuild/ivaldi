package delivery

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const (
	domProject = "prj_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	domTeam    = "tea_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	domTeam2   = "tea_01BX5ZZKBKACTAV9WEVGEMMVRZ"
)

func domApplicationProps() NewApplicationProps {
	return NewApplicationProps{
		OrganizationID: domOrg,
		WorkspaceID:    domWorkspace,
		ProjectID:      domProject,
		OwnerTeamID:    domTeam,
		Slug:           "payments-api",
		DisplayName:    "Payments API",
	}
}

func domNewApplication(t *testing.T) *Application {
	t.Helper()
	a, err := NewApplication(domApplicationProps(), nil)
	require.NoError(t, err)
	return a
}

func TestNewApplication(t *testing.T) {
	clock.Fix(t, domT0)
	props := domApplicationProps()
	props.SourceID = new("src_payments")

	a, err := NewApplication(props, nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("app", a.ID()), "a new application gets an app_ ID: %q", a.ID())
	assert.Equal(t, domOrg, a.OrganizationID())
	assert.Equal(t, domWorkspace, a.WorkspaceID())
	assert.Equal(t, domProject, a.ProjectID())
	assert.Equal(t, domTeam, a.OwnerTeamID())
	assert.Equal(t, "payments-api", a.Slug())
	assert.Equal(t, "Payments API", a.DisplayName())
	assert.Equal(t, "src_payments", *a.SourceID())
	assert.Equal(t, map[string]string{WorkloadSelectorLabel: a.ID()}, a.WorkloadSelector(), "the default selector names the application's ID")
	assert.Equal(t, domT0, a.CreatedAt())
	assert.Equal(t, domT0, a.UpdatedAt())
	assert.Nil(t, a.ArchivedAt())
	assert.False(t, a.Archived())
}

func TestNewApplicationOptions(t *testing.T) {
	t.Run("slug derived from display name", func(t *testing.T) {
		props := domApplicationProps()
		props.Slug = ""
		a, err := NewApplication(props, nil)
		require.NoError(t, err)
		assert.Equal(t, "payments-api", a.Slug())
	})

	t.Run("empty source is none", func(t *testing.T) {
		props := domApplicationProps()
		props.SourceID = new("")
		a, err := NewApplication(props, nil)
		require.NoError(t, err)
		assert.Nil(t, a.SourceID())
	})

	t.Run("given selector is kept", func(t *testing.T) {
		props := domApplicationProps()
		props.WorkloadSelector = map[string]string{"app.kubernetes.io/name": "payments", "tier": ""}
		a, err := NewApplication(props, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"app.kubernetes.io/name": "payments", "tier": ""}, a.WorkloadSelector())
	})

	t.Run("empty selector means the default", func(t *testing.T) {
		props := domApplicationProps()
		props.WorkloadSelector = map[string]string{}
		a, err := NewApplication(props, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{WorkloadSelectorLabel: a.ID()}, a.WorkloadSelector())
	})

	t.Run("the default selector uses a given ID", func(t *testing.T) {
		given := id.New("app")
		a, err := NewApplication(domApplicationProps(), &given)
		require.NoError(t, err)
		assert.Equal(t, given, a.ID())
		assert.Equal(t, map[string]string{WorkloadSelectorLabel: given}, a.WorkloadSelector())
	})

	t.Run("selector at its limits", func(t *testing.T) {
		props := domApplicationProps()
		props.WorkloadSelector = map[string]string{}
		for i := range maxSelectorLabels - 1 {
			props.WorkloadSelector[fmt.Sprintf("k%d", i)] = "v"
		}
		props.WorkloadSelector[strings.Repeat("k", maxSelectorKeyLength)] = strings.Repeat("v", maxLabelValueLength)
		_, err := NewApplication(props, nil)
		require.NoError(t, err)
	})
}

func TestNewApplicationWithStoredState(t *testing.T) {
	clock.Fix(t, domT2)
	given := id.New("app")
	props := domApplicationProps()
	props.CreatedAt = new(domT0)
	props.UpdatedAt = new(domT1)
	props.ArchivedAt = new(domT1)

	a, err := NewApplication(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, a.ID())
	assert.Equal(t, domT0, a.CreatedAt())
	assert.Equal(t, domT1, a.UpdatedAt())
	assert.Equal(t, domT1, *a.ArchivedAt())
	assert.True(t, a.Archived())
}

func TestNewApplicationIsRefused(t *testing.T) {
	tooMany := map[string]string{}
	for i := range maxSelectorLabels + 1 {
		tooMany[fmt.Sprintf("k%d", i)] = "v"
	}
	tests := []struct {
		name   string
		mutate func(*NewApplicationProps)
		id     *string
	}{
		{"no organization", func(p *NewApplicationProps) { p.OrganizationID = "" }, nil},
		{"no workspace", func(p *NewApplicationProps) { p.WorkspaceID = "" }, nil},
		{"no project", func(p *NewApplicationProps) { p.ProjectID = "" }, nil},
		{"no owning team", func(p *NewApplicationProps) { p.OwnerTeamID = "" }, nil},
		{"ID with wrong prefix", func(*NewApplicationProps) {}, new(id.New("art"))},
		{"malformed ID", func(*NewApplicationProps) {}, new("app_lowercase0000000000000000")},
		{"no display name", func(p *NewApplicationProps) { p.DisplayName = "" }, nil},
		{"bad slug", func(p *NewApplicationProps) { p.Slug = "payments api" }, nil},
		{"too many selector labels", func(p *NewApplicationProps) { p.WorkloadSelector = tooMany }, nil},
		{"empty selector key", func(p *NewApplicationProps) { p.WorkloadSelector = map[string]string{"": "v"} }, nil},
		{"selector key too long", func(p *NewApplicationProps) {
			p.WorkloadSelector = map[string]string{strings.Repeat("k", maxSelectorKeyLength+1): "v"}
		}, nil},
		{"selector key with a space", func(p *NewApplicationProps) { p.WorkloadSelector = map[string]string{"my app": "v"} }, nil},
		{"selector key with =", func(p *NewApplicationProps) { p.WorkloadSelector = map[string]string{"a=b": "v"} }, nil},
		{"selector key with ,", func(p *NewApplicationProps) { p.WorkloadSelector = map[string]string{"a,b": "v"} }, nil},
		{"selector value too long", func(p *NewApplicationProps) {
			p.WorkloadSelector = map[string]string{"k": strings.Repeat("v", maxLabelValueLength+1)}
		}, nil},
		{"selector value with a tab", func(p *NewApplicationProps) { p.WorkloadSelector = map[string]string{"k": "a\tb"} }, nil},
		{"selector value with ,", func(p *NewApplicationProps) { p.WorkloadSelector = map[string]string{"k": "a,b"} }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := domApplicationProps()
			tt.mutate(&props)
			_, err := NewApplication(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestApplicationGettersReturnCopies(t *testing.T) {
	props := domApplicationProps()
	props.SourceID = new("src_payments")
	props.WorkloadSelector = map[string]string{"app": "payments"}
	props.ArchivedAt = new(domT0)
	a, err := NewApplication(props, nil)
	require.NoError(t, err)
	before := a.Snapshot()

	*props.SourceID = "changed"
	props.WorkloadSelector["app"] = "changed"
	*props.ArchivedAt = domT2
	*a.SourceID() = "changed"
	a.WorkloadSelector()["app"] = "changed"
	*a.ArchivedAt() = domT2
	snap := a.Snapshot()
	*snap.SourceID = "changed"
	snap.WorkloadSelector["app"] = "changed"

	assert.Equal(t, before, a.Snapshot())
}

func TestApplicationSnapshot(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewApplication(t)

	assert.Equal(t, ApplicationSnapshot{
		ID:               a.ID(),
		OrganizationID:   domOrg,
		WorkspaceID:      domWorkspace,
		ProjectID:        domProject,
		OwnerTeamID:      domTeam,
		Slug:             "payments-api",
		DisplayName:      "Payments API",
		WorkloadSelector: map[string]string{WorkloadSelectorLabel: a.ID()},
		CreatedAt:        domT0,
		UpdatedAt:        domT0,
	}, a.Snapshot())
}

func TestApplicationUpdate(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewApplication(t)
	clock.Fix(t, domT1)
	selector := map[string]string{"app": "payments"}

	require.NoError(t, a.Update(UpdateApplicationProps{Slug: new("payments"), DisplayName: new("Payments"), WorkloadSelector: selector}))

	assert.Equal(t, "payments", a.Slug())
	assert.Equal(t, "Payments", a.DisplayName())
	assert.Equal(t, selector, a.WorkloadSelector())
	assert.Equal(t, domT1, a.UpdatedAt())

	selector["app"] = "changed"
	assert.Equal(t, "payments", a.WorkloadSelector()["app"], "the given selector is copied")

	t.Run("a nil selector is kept", func(t *testing.T) {
		require.NoError(t, a.Update(UpdateApplicationProps{DisplayName: new("Payments 2")}))
		assert.Equal(t, map[string]string{"app": "payments"}, a.WorkloadSelector())
		assert.Equal(t, "payments", a.Slug())
	})

	t.Run("an empty selector resets it to the default", func(t *testing.T) {
		require.NoError(t, a.Update(UpdateApplicationProps{WorkloadSelector: map[string]string{}}))
		assert.Equal(t, map[string]string{WorkloadSelectorLabel: a.ID()}, a.WorkloadSelector())
	})
}

func TestApplicationUpdateIsAtomic(t *testing.T) {
	good := map[string]string{"app": "payments"}
	tests := []struct {
		name  string
		props UpdateApplicationProps
	}{
		{"bad slug", UpdateApplicationProps{Slug: new("Bad"), DisplayName: new("New"), WorkloadSelector: good}},
		{"bad display name", UpdateApplicationProps{Slug: new("new"), DisplayName: new(""), WorkloadSelector: good}},
		{"bad selector", UpdateApplicationProps{Slug: new("new"), DisplayName: new("New"), WorkloadSelector: map[string]string{"bad key": "v"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, domT0)
			a := domNewApplication(t)
			before := a.Snapshot()
			clock.Fix(t, domT1)

			assert.ErrorIs(t, a.Update(tt.props), ErrInvalid)
			assert.Equal(t, before, a.Snapshot())
		})
	}
}

func TestApplicationRefusesChangesWhenArchived(t *testing.T) {
	a := domNewApplication(t)
	a.Archive()
	before := a.Snapshot()

	assert.ErrorIs(t, a.Update(UpdateApplicationProps{DisplayName: new("New")}), ErrArchived)
	assert.ErrorIs(t, a.LinkSource("src_x"), ErrArchived)
	assert.ErrorIs(t, a.UnlinkSource(), ErrArchived)
	assert.ErrorIs(t, a.TransferTo(domTeam2), ErrArchived)
	assert.Equal(t, before, a.Snapshot())
}

func TestApplicationSource(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewApplication(t)

	clock.Fix(t, domT1)
	require.NoError(t, a.LinkSource("src_payments"))
	assert.Equal(t, "src_payments", *a.SourceID())
	assert.Equal(t, domT1, a.UpdatedAt())

	t.Run("an empty source is refused", func(t *testing.T) {
		before := a.Snapshot()
		assert.ErrorIs(t, a.LinkSource(""), ErrInvalid)
		assert.Equal(t, before, a.Snapshot())
	})

	clock.Fix(t, domT2)
	require.NoError(t, a.UnlinkSource())
	assert.Nil(t, a.SourceID())
	assert.Equal(t, domT2, a.UpdatedAt())
}

func TestApplicationTransferTo(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewApplication(t)

	clock.Fix(t, domT1)
	require.NoError(t, a.TransferTo(domTeam2))
	assert.Equal(t, domTeam2, a.OwnerTeamID())
	assert.Equal(t, domT1, a.UpdatedAt())

	before := a.Snapshot()
	assert.ErrorIs(t, a.TransferTo(""), ErrInvalid)
	assert.Equal(t, before, a.Snapshot())
}

func TestApplicationArchiveAndRestore(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewApplication(t)

	clock.Fix(t, domT1)
	a.Archive()
	assert.True(t, a.Archived())
	assert.Equal(t, domT1, *a.ArchivedAt())
	assert.Equal(t, domT1, a.UpdatedAt())

	clock.Fix(t, domT2)
	a.Archive()
	assert.Equal(t, domT1, *a.ArchivedAt(), "archiving again does nothing")
	assert.Equal(t, domT1, a.UpdatedAt())

	a.Restore()
	assert.False(t, a.Archived())
	assert.Nil(t, a.ArchivedAt())
	assert.Equal(t, domT2, a.UpdatedAt())

	clock.Fix(t, domT0)
	a.Restore()
	assert.Equal(t, domT2, a.UpdatedAt(), "restoring an active application does nothing")
}
