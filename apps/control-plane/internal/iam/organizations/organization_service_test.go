package organizations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
)

func TestNewOrganizationServiceRequiresItsDependencies(t *testing.T) {
	w := newWorld(t)
	all := w.options(w.authz)
	for i := range all {
		without := append(append([]OrganizationServiceOption{}, all[:i]...), all[i+1:]...)
		_, err := NewOrganizationService(without...)
		assert.Error(t, err, "without option %d", i)
	}
}

func TestGetOrganization(t *testing.T) {
	w := newWorld(t)

	o, err := w.svc.GetOrganization(context.Background(), w.ada, w.acme)
	require.NoError(t, err)
	assert.Equal(t, "acme", o.Slug())
	assert.Equal(t, "Acme Corporation", o.Name())

	require.Len(t, w.authz.asked, 1)
	req := w.authz.asked[0]
	assert.Equal(t, iam.PermissionOrganizationRead, req.Permission)
	assert.Equal(t, iam.ActionOrganizationRead, req.Action)
	assert.Equal(t, w.acme, req.OrganizationID)
	assert.Equal(t, w.acme, req.ResourceID, "checked on the Organization's root resource")
	assert.Empty(t, w.audit, "reading is not audited")
}

func TestUpdateOrganization(t *testing.T) {
	w := newWorld(t)
	later := t0.Add(time.Hour)
	clock.Fix(t, later)

	o, err := w.svc.UpdateOrganization(context.Background(), w.ada, w.acme, iam.UpdateOrganizationProps{
		DisplayName: new("Acme Inc."),
		Name:        new("Acme Incorporated"),
		Slug:        new("acme-inc"),
		LogoURL:     new("https://acme.example.com/logo.png"),
	})
	require.NoError(t, err)
	assert.Equal(t, "acme-inc", o.Slug())
	assert.Equal(t, later, o.UpdatedAt())

	stored := w.orgs[w.acme]
	assert.Equal(t, "Acme Inc.", stored.DisplayName)
	assert.Equal(t, "Acme Incorporated", stored.Name)
	assert.Equal(t, "acme-inc", stored.Slug)
	assert.Equal(t, new("https://acme.example.com/logo.png"), stored.LogoURL)

	t.Run("asks to manage the Organization", func(t *testing.T) {
		req := w.authz.asked[len(w.authz.asked)-1]
		assert.Equal(t, iam.PermissionOrganizationManage, req.Permission)
		assert.Equal(t, iam.ActionOrganizationUpdate, req.Action)
		assert.Equal(t, w.acme, req.ResourceID)
		assert.NotNil(t, req.Proposal)
	})

	t.Run("records the event and the audit record", func(t *testing.T) {
		require.Len(t, w.events, 1)
		e := w.events[0]
		assert.Equal(t, "iam.v1.organization.updated", e.Type)
		assert.Equal(t, w.acme, e.OrganizationID)
		assert.Equal(t, iam.AggregateOrganization, e.AggregateType)
		assert.Equal(t, w.acme, e.AggregateID)

		require.Len(t, w.audit, 1)
		a := w.audit[0]
		assert.Equal(t, iam.EventOrganizationUpdated, a.Type)
		assert.Equal(t, w.acme, a.OrganizationID)
		assert.Equal(t, w.acme, a.ResourceID)
		assert.Equal(t, w.ada, a.Actor)
		assert.Equal(t, "dec-"+iam.ActionOrganizationUpdate, a.DecisionID)
		assert.Equal(t, "acme", a.Before.(iam.OrganizationSnapshot).Slug)
		assert.Equal(t, "acme-inc", a.After.(iam.OrganizationSnapshot).Slug)
	})
}

func TestUpdateOrganizationRemovesTheLogo(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	_, err := w.svc.UpdateOrganization(ctx, w.ada, w.acme, iam.UpdateOrganizationProps{LogoURL: new("https://acme.example.com/logo.png")})
	require.NoError(t, err)

	o, err := w.svc.UpdateOrganization(ctx, w.ada, w.acme, iam.UpdateOrganizationProps{LogoURL: new("")})
	require.NoError(t, err)
	assert.Nil(t, o.LogoURL())
	assert.Nil(t, w.orgs[w.acme].LogoURL)
}

func TestUpdateOrganizationThatChangesNothingRecordsNoEvent(t *testing.T) {
	w := newWorld(t)
	_, err := w.svc.UpdateOrganization(context.Background(), w.ada, w.acme, iam.UpdateOrganizationProps{DisplayName: new("Acme")})
	require.NoError(t, err)
	assert.Empty(t, w.events)
}

func TestUpdateOrganizationRefusals(t *testing.T) {
	cases := map[string]struct {
		props iam.UpdateOrganizationProps
		setup func(*world)
		want  error
	}{
		"a slug another Organization holds": {
			props: iam.UpdateOrganizationProps{Slug: new("globex")},
			want:  iam.ErrAlreadyExists,
		},
		"an invalid slug": {
			props: iam.UpdateOrganizationProps{Slug: new("Not A Slug")},
			want:  iam.ErrInvalid,
		},
		"an empty display name": {
			props: iam.UpdateOrganizationProps{DisplayName: new(" ")},
			want:  iam.ErrInvalid,
		},
		"a logo that is not https": {
			props: iam.UpdateOrganizationProps{LogoURL: new("http://acme.example.com/logo.png")},
			want:  iam.ErrInvalid,
		},
		"denied by the authorizer": {
			props: iam.UpdateOrganizationProps{DisplayName: new("Acme Inc.")},
			setup: func(w *world) {
				w.authz.deny = func(r authorization.Request) bool { return r.Permission == iam.PermissionOrganizationManage }
			},
			want: authorization.ErrDenied,
		},
		"a failed write": {
			props: iam.UpdateOrganizationProps{DisplayName: new("Acme Inc.")},
			setup: func(w *world) { w.failWrite = errors.New("database down") },
			want:  nil,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			if tc.setup != nil {
				tc.setup(w)
			}
			before := w.orgs[w.acme]

			_, err := w.svc.UpdateOrganization(context.Background(), w.ada, w.acme, tc.props)
			require.Error(t, err)
			if tc.want != nil {
				assert.ErrorIs(t, err, tc.want)
			}
			assert.Equal(t, before, w.orgs[w.acme], "nothing changes")
			assert.Empty(t, w.events)
			assert.Empty(t, w.audit)
		})
	}
}

func TestGetOrganizationDenied(t *testing.T) {
	w := newWorld(t)
	w.authz.deny = func(authorization.Request) bool { return true }
	_, err := w.svc.GetOrganization(context.Background(), w.ada, w.acme)
	assert.ErrorIs(t, err, authorization.ErrDenied)
}

// The service under the authorizing wrapper cannot change anything on its
// own: its audit record needs a decision.
func TestUpdateWithoutADecisionFails(t *testing.T) {
	w := newWorld(t)
	inner := w.svc.(*authorized).next
	_, err := inner.UpdateOrganization(context.Background(), w.ada, w.acme, iam.UpdateOrganizationProps{DisplayName: new("Acme Inc.")})
	assert.ErrorIs(t, err, change.ErrUnauthorized)
	assert.Equal(t, "Acme", w.orgs[w.acme].DisplayName)
}
