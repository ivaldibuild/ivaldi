package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

func TestNewApplicationRecordsCreated(t *testing.T) {
	a := domNewApplication(t)

	assert.Equal(t, []aggregate.Event{ApplicationCreated{a.Snapshot()}}, a.PullEvents())
}

func TestNewApplicationRecordsNothingWhenRebuilt(t *testing.T) {
	stored := id.New("app")

	a, err := NewApplication(domApplicationProps(), &stored)
	require.NoError(t, err)

	assert.Empty(t, a.PullEvents(), "loading a stored application raises no creation event")
}

func TestApplicationRecordsWhatChanged(t *testing.T) {
	tests := []struct {
		name   string
		change func(a *Application) error
		want   func(s ApplicationSnapshot) aggregate.Event
	}{
		{
			name:   "update",
			change: func(a *Application) error { return a.Update(UpdateApplicationProps{DisplayName: new("Payments")}) },
			want:   func(s ApplicationSnapshot) aggregate.Event { return ApplicationUpdated{s} },
		},
		{
			name:   "link source",
			change: func(a *Application) error { return a.LinkSource("src_1") },
			want:   func(s ApplicationSnapshot) aggregate.Event { return ApplicationSourceLinked{s} },
		},
		{
			name: "unlink source",
			change: func(a *Application) error {
				require.NoError(t, a.LinkSource("src_1"))
				a.PullEvents()
				return a.UnlinkSource()
			},
			want: func(s ApplicationSnapshot) aggregate.Event { return ApplicationSourceUnlinked{s} },
		},
		{
			name:   "transfer",
			change: func(a *Application) error { return a.TransferTo(domTeam2) },
			want: func(s ApplicationSnapshot) aggregate.Event {
				return ApplicationTransferred{Application: s, PreviousOwnerTeamID: domTeam}
			},
		},
		{
			name:   "archive",
			change: func(a *Application) error { a.Archive(); return nil },
			want:   func(s ApplicationSnapshot) aggregate.Event { return ApplicationArchived{s} },
		},
		{
			name: "restore",
			change: func(a *Application) error {
				a.Archive()
				a.PullEvents()
				a.Restore()
				return nil
			},
			want: func(s ApplicationSnapshot) aggregate.Event { return ApplicationRestored{s} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := domNewApplication(t)
			a.PullEvents()

			require.NoError(t, tt.change(a))

			assert.Equal(t, []aggregate.Event{tt.want(a.Snapshot())}, a.PullEvents(), "the event carries the application after the change")
		})
	}
}

func TestApplicationRecordsNothingWhenNothingChanged(t *testing.T) {
	tests := []struct {
		name   string
		change func(a *Application) error
	}{
		{"empty update", func(a *Application) error { return a.Update(UpdateApplicationProps{}) }},
		{"same values", func(a *Application) error {
			return a.Update(UpdateApplicationProps{Slug: new("payments-api"), DisplayName: new("Payments API")})
		}},
		{"default selector again", func(a *Application) error {
			return a.Update(UpdateApplicationProps{WorkloadSelector: map[string]string{}})
		}},
		{"same source", func(a *Application) error {
			require.NoError(t, a.LinkSource("src_1"))
			a.PullEvents()
			return a.LinkSource("src_1")
		}},
		{"no source to unlink", func(a *Application) error { return a.UnlinkSource() }},
		{"same team", func(a *Application) error { return a.TransferTo(domTeam) }},
		{"archive twice", func(a *Application) error {
			a.Archive()
			a.PullEvents()
			a.Archive()
			return nil
		}},
		{"restore an active one", func(a *Application) error { a.Restore(); return nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := domNewApplication(t)
			a.PullEvents()

			require.NoError(t, tt.change(a))

			assert.Empty(t, a.PullEvents())
		})
	}
}

// The snapshot's JSON is the Application events' contract: a change here is a
// change for every consumer.
func TestApplicationSnapshotJSON(t *testing.T) {
	archived := domT1
	b, err := json.Marshal(ApplicationTransferred{
		Application: ApplicationSnapshot{
			ID: "app_1", OrganizationID: "org_1", WorkspaceID: "wsp_1", ProjectID: "prj_1", OwnerTeamID: "tea_2",
			Slug: "payments-api", DisplayName: "Payments API", SourceID: new("src_1"),
			WorkloadSelector: map[string]string{"tier": "api"}, CreatedAt: domT0, UpdatedAt: domT1, ArchivedAt: &archived,
		},
		PreviousOwnerTeamID: "tea_1",
	})

	require.NoError(t, err)
	assert.JSONEq(t, `{
		"application": {
			"id": "app_1", "organization_id": "org_1", "workspace_id": "wsp_1", "project_id": "prj_1",
			"owner_team_id": "tea_2", "slug": "payments-api", "display_name": "Payments API", "source_id": "src_1",
			"workload_selector": {"tier": "api"},
			"created_at": "2026-10-20T12:00:00Z", "updated_at": "2026-10-20T13:00:00Z", "archived_at": "2026-10-20T13:00:00Z"
		},
		"previous_owner_team_id": "tea_1"
	}`, string(b))
}

func TestApplicationEventsReachHandlersWhole(t *testing.T) {
	clock.Fix(t, domT0)
	a := domNewApplication(t)
	a.PullEvents()
	require.NoError(t, a.TransferTo(domTeam2))
	envelopes, err := events.Wrap(context.Background(), events.Source{
		OrganizationID: domOrg, AggregateType: "delivery.Application", AggregateID: a.ID(),
	}, a.PullEvents()...)
	require.NoError(t, err)

	var r events.Router
	var got ApplicationTransferred
	events.On(&r, func(_ context.Context, _ events.Envelope, e ApplicationTransferred) error {
		got = e
		return nil
	})
	require.NoError(t, r.Dispatch(context.Background(), envelopes[0]))

	assert.Equal(t, ApplicationTransferred{Application: a.Snapshot(), PreviousOwnerTeamID: domTeam}, got)
}
