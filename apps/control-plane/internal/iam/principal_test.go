package iam

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func principalProps() PrincipalProps {
	return PrincipalProps{
		OrganizationID: testOrg,
		DisplayName:    "Ada Lovelace",
		Metadata:       map[string]string{"team": "platform"},
	}
}

func principalUserProps() NewUserProps {
	return NewUserProps{PrincipalProps: principalProps(), AccountID: testAccount}
}

func principalNewUser(t *testing.T) *User {
	t.Helper()
	u, err := NewUser(principalUserProps(), nil)
	require.NoError(t, err)
	return u
}

func TestNewUser(t *testing.T) {
	clock.Fix(t, t0)

	u, err := NewUser(principalUserProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("prn", u.ID()), "a new user gets a prn_ ID: %q", u.ID())
	assert.Equal(t, "prn", u.Prefix())
	assert.Equal(t, testOrg, u.OrganizationID())
	assert.Equal(t, PrincipalUser, u.Kind())
	assert.Equal(t, "Ada Lovelace", u.DisplayName())
	assert.Equal(t, PrincipalActive, u.Status(), "a new principal is active")
	assert.True(t, u.Active())
	assert.Equal(t, map[string]string{"team": "platform"}, u.Metadata())
	assert.Equal(t, testAccount, u.AccountID())
	assert.Equal(t, t0, u.JoinedAt())
	assert.Equal(t, t0, u.CreatedAt())
	assert.Equal(t, t0, u.UpdatedAt())
}

func TestNewUserWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	given := id.New("prn")
	props := principalUserProps()
	props.Status = PrincipalDeactivated
	props.CreatedAt = new(t0)
	props.UpdatedAt = new(t1)
	props.JoinedAt = new(t0)

	u, err := NewUser(props, &given)

	require.NoError(t, err)
	assert.Equal(t, given, u.ID(), "a given ID is kept")
	assert.Equal(t, PrincipalDeactivated, u.Status())
	assert.False(t, u.Active())
	assert.Equal(t, t0, u.CreatedAt(), "given timestamps are kept")
	assert.Equal(t, t1, u.UpdatedAt())
	assert.Equal(t, t0, u.JoinedAt())
}

func TestNewPrincipalIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PrincipalProps)
		id     *string
	}{
		{"no organization", func(p *PrincipalProps) { p.OrganizationID = "" }, nil},
		{"organization with wrong prefix", func(p *PrincipalProps) { p.OrganizationID = testWorkspace }, nil},
		{"no display name", func(p *PrincipalProps) { p.DisplayName = "" }, nil},
		{"display name too long", func(p *PrincipalProps) { p.DisplayName = strings.Repeat("é", maxNameLength+1) }, nil},
		{"unknown status", func(p *PrincipalProps) { p.Status = "deleted" }, nil},
		{"empty metadata key", func(p *PrincipalProps) { p.Metadata = map[string]string{"": "x"} }, nil},
		{"ID with wrong prefix", func(*PrincipalProps) {}, new(id.New("acc"))},
		{"malformed ID", func(*PrincipalProps) {}, new("prn_nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := principalUserProps()
			tt.mutate(&props.PrincipalProps)
			_, err := NewUser(props, tt.id)
			assert.ErrorIs(t, err, ErrInvalid, "user")

			sa := principalServiceAccountProps()
			tt.mutate(&sa.PrincipalProps)
			_, err = NewServiceAccount(sa, tt.id)
			assert.ErrorIs(t, err, ErrInvalid, "service account")

			w := principalWorkloadProps()
			tt.mutate(&w.PrincipalProps)
			_, err = NewWorkload(w, tt.id)
			assert.ErrorIs(t, err, ErrInvalid, "workload")
		})
	}
}

func TestNewUserIsRefused(t *testing.T) {
	for _, accountID := range []string{"", testOrg, "acc_nope"} {
		t.Run(accountID, func(t *testing.T) {
			props := principalUserProps()
			props.AccountID = accountID
			_, err := NewUser(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestPrincipalGettersReturnCopies(t *testing.T) {
	props := principalUserProps()
	u, err := NewUser(props, nil)
	require.NoError(t, err)

	props.Metadata["team"] = "changed"
	u.Metadata()["team"] = "changed"
	snap := u.Snapshot()
	snap.Metadata["team"] = "changed"

	assert.Equal(t, "platform", u.Metadata()["team"])
}

func TestUserSnapshot(t *testing.T) {
	clock.Fix(t, t0)
	u := principalNewUser(t)

	assert.Equal(t, UserSnapshot{
		PrincipalSnapshot: PrincipalSnapshot{
			ID:             u.ID(),
			OrganizationID: testOrg,
			Kind:           PrincipalUser,
			DisplayName:    "Ada Lovelace",
			Status:         PrincipalActive,
			Metadata:       map[string]string{"team": "platform"},
			CreatedAt:      t0,
			UpdatedAt:      t0,
		},
		AccountID: testAccount,
		JoinedAt:  t0,
	}, u.Snapshot())
}

func TestPrincipalRename(t *testing.T) {
	clock.Fix(t, t0)
	u := principalNewUser(t)
	clock.Fix(t, t1)

	require.NoError(t, u.Rename("Ada King"))
	assert.Equal(t, "Ada King", u.DisplayName())
	assert.Equal(t, t1, u.UpdatedAt())

	assert.ErrorIs(t, u.Rename(" "), ErrInvalid)
	assert.Equal(t, "Ada King", u.DisplayName())
}

func TestPrincipalSetMetadata(t *testing.T) {
	clock.Fix(t, t0)
	u := principalNewUser(t)
	clock.Fix(t, t1)

	given := map[string]string{"team": "delivery"}
	require.NoError(t, u.SetMetadata(given))
	given["team"] = "changed"
	assert.Equal(t, map[string]string{"team": "delivery"}, u.Metadata(), "the given map is copied")
	assert.Equal(t, t1, u.UpdatedAt())

	assert.ErrorIs(t, u.SetMetadata(map[string]string{"": "x"}), ErrInvalid)
	assert.Equal(t, map[string]string{"team": "delivery"}, u.Metadata())

	require.NoError(t, u.SetMetadata(nil))
	assert.Nil(t, u.Metadata(), "nil removes the metadata")
}

func TestPrincipalStatusChanges(t *testing.T) {
	tests := []struct {
		name    string
		from    PrincipalStatus
		change  func(*User) error
		want    PrincipalStatus
		wantErr error
	}{
		{"suspend active", PrincipalActive, (*User).Suspend, PrincipalSuspended, nil},
		{"suspend suspended", PrincipalSuspended, (*User).Suspend, PrincipalSuspended, nil},
		{"suspend deactivated", PrincipalDeactivated, (*User).Suspend, PrincipalDeactivated, ErrStatus},
		{"deactivate active", PrincipalActive, (*User).Deactivate, PrincipalDeactivated, nil},
		{"deactivate suspended", PrincipalSuspended, (*User).Deactivate, PrincipalDeactivated, nil},
		{"deactivate deactivated", PrincipalDeactivated, (*User).Deactivate, PrincipalDeactivated, nil},
		{"reactivate suspended", PrincipalSuspended, (*User).Reactivate, PrincipalActive, nil},
		{"reactivate deactivated", PrincipalDeactivated, (*User).Reactivate, PrincipalActive, nil},
		{"reactivate active", PrincipalActive, (*User).Reactivate, PrincipalActive, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock.Fix(t, t0)
			props := principalUserProps()
			props.Status = tt.from
			u, err := NewUser(props, nil)
			require.NoError(t, err)
			clock.Fix(t, t1)

			err = tt.change(u)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, u.Status())
			if tt.from == tt.want {
				assert.Equal(t, t0, u.UpdatedAt(), "no change leaves UpdatedAt alone")
			} else {
				assert.Equal(t, t1, u.UpdatedAt())
			}
		})
	}
}

func principalServiceAccountProps() NewServiceAccountProps {
	props := principalProps()
	props.DisplayName = "CI"
	return NewServiceAccountProps{
		PrincipalProps:       props,
		Description:          "Deploys from CI",
		CreatedByPrincipalID: testCreator,
	}
}

func TestNewServiceAccount(t *testing.T) {
	clock.Fix(t, t0)

	s, err := NewServiceAccount(principalServiceAccountProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("prn", s.ID()))
	assert.Equal(t, PrincipalServiceAccount, s.Kind())
	assert.Equal(t, "Deploys from CI", s.Description())
	assert.Equal(t, testCreator, s.CreatedByPrincipalID())
	assert.Equal(t, ServiceAccountSnapshot{
		PrincipalSnapshot: PrincipalSnapshot{
			ID:             s.ID(),
			OrganizationID: testOrg,
			Kind:           PrincipalServiceAccount,
			DisplayName:    "CI",
			Status:         PrincipalActive,
			Metadata:       map[string]string{"team": "platform"},
			CreatedAt:      t0,
			UpdatedAt:      t0,
		},
		Description:          "Deploys from CI",
		CreatedByPrincipalID: testCreator,
	}, s.Snapshot())
}

func TestNewServiceAccountAllowsNoDescription(t *testing.T) {
	props := principalServiceAccountProps()
	props.Description = ""

	s, err := NewServiceAccount(props, nil)

	require.NoError(t, err)
	assert.Empty(t, s.Description())
}

func TestNewServiceAccountIsRefused(t *testing.T) {
	for _, creator := range []string{"", testAccount, "prn_nope"} {
		t.Run(creator, func(t *testing.T) {
			props := principalServiceAccountProps()
			props.CreatedByPrincipalID = creator
			_, err := NewServiceAccount(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestServiceAccountSetDescription(t *testing.T) {
	clock.Fix(t, t0)
	s, err := NewServiceAccount(principalServiceAccountProps(), nil)
	require.NoError(t, err)
	clock.Fix(t, t1)

	s.SetDescription("Runs SCIM provisioning")
	assert.Equal(t, "Runs SCIM provisioning", s.Description())
	assert.Equal(t, t1, s.UpdatedAt())

	s.SetDescription("")
	assert.Empty(t, s.Description(), "an empty description removes it")
}

func principalWorkloadProps() NewWorkloadProps {
	props := principalProps()
	props.DisplayName = "worker-eu-1"
	return NewWorkloadProps{
		PrincipalProps: props,
		Component:      ComponentWorker,
		Pool:           "eu-west",
	}
}

func TestNewWorkload(t *testing.T) {
	clock.Fix(t, t0)

	w, err := NewWorkload(principalWorkloadProps(), nil)

	require.NoError(t, err)
	assert.True(t, id.Valid("prn", w.ID()))
	assert.Equal(t, PrincipalWorkload, w.Kind())
	assert.Equal(t, ComponentWorker, w.Component())
	assert.Equal(t, "eu-west", w.Pool())
	assert.Equal(t, t0, w.EnrolledAt())
	assert.Nil(t, w.LastSeenAt())
	assert.Equal(t, WorkloadSnapshot{
		PrincipalSnapshot: PrincipalSnapshot{
			ID:             w.ID(),
			OrganizationID: testOrg,
			Kind:           PrincipalWorkload,
			DisplayName:    "worker-eu-1",
			Status:         PrincipalActive,
			Metadata:       map[string]string{"team": "platform"},
			CreatedAt:      t0,
			UpdatedAt:      t0,
		},
		Component:  ComponentWorker,
		Pool:       "eu-west",
		EnrolledAt: t0,
	}, w.Snapshot())
}

func TestNewWorkloadWithStoredState(t *testing.T) {
	clock.Fix(t, t2)
	props := principalWorkloadProps()
	props.EnrolledAt = new(t0)
	props.LastSeenAt = new(t1)

	w, err := NewWorkload(props, nil)

	require.NoError(t, err)
	assert.Equal(t, t0, w.EnrolledAt())
	require.NotNil(t, w.LastSeenAt())
	assert.Equal(t, t1, *w.LastSeenAt())

	*props.LastSeenAt = t2
	*w.LastSeenAt() = t2
	*w.Snapshot().LastSeenAt = t2
	assert.Equal(t, t1, *w.LastSeenAt(), "times are copied")
}

func TestNewWorkloadAcceptsEveryComponent(t *testing.T) {
	for _, c := range []WorkloadComponent{ComponentWorker, ComponentIndexer, ComponentGateway, ComponentRuntimeAgent} {
		t.Run(string(c), func(t *testing.T) {
			props := principalWorkloadProps()
			props.Component = c
			w, err := NewWorkload(props, nil)
			require.NoError(t, err)
			assert.Equal(t, c, w.Component())
		})
	}
}

func TestNewWorkloadIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NewWorkloadProps)
	}{
		{"no component", func(p *NewWorkloadProps) { p.Component = "" }},
		{"unknown component", func(p *NewWorkloadProps) { p.Component = "scheduler" }},
		{"no pool", func(p *NewWorkloadProps) { p.Pool = "" }},
		{"pool too long", func(p *NewWorkloadProps) { p.Pool = strings.Repeat("p", maxPoolLength+1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := principalWorkloadProps()
			tt.mutate(&props)
			_, err := NewWorkload(props, nil)
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}

func TestWorkloadSeen(t *testing.T) {
	clock.Fix(t, t0)
	w, err := NewWorkload(principalWorkloadProps(), nil)
	require.NoError(t, err)
	clock.Fix(t, t1)

	w.Seen()

	require.NotNil(t, w.LastSeenAt())
	assert.Equal(t, t1, *w.LastSeenAt())
	assert.Equal(t, t0, w.UpdatedAt(), "checking in is not a change to the workload")
}
