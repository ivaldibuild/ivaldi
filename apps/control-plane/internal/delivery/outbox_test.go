package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRecordsDomainEvents(t *testing.T) {
	t.Run("create and update one object", func(t *testing.T) {
		f := newSvcFixture(t)
		before := f.outbox.count()

		e, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{
			OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), Slug: "prod", DisplayName: "Production", Tier: TierProduction,
		})
		require.NoError(t, err)

		created := f.outbox.since(before)
		require.Len(t, created, 1)
		assert.Equal(t, "delivery.v1.environment.created", created[0].Type)
		assert.Equal(t, AggregateEnvironment, created[0].AggregateType)
		assert.Equal(t, e.ID(), created[0].AggregateID)
		assert.Equal(t, svcOrg, created[0].OrganizationID)
		assert.Contains(t, string(created[0].Payload), `"slug":"prod"`)

		before = f.outbox.count()
		name := "Production EU"
		_, err = f.svc.UpdateEnvironment(svcCtx, svcAlice, svcOrg, e.ID(), UpdateEnvironmentProps{DisplayName: &name})
		require.NoError(t, err)

		updated := f.outbox.since(before)
		require.Len(t, updated, 1)
		assert.Equal(t, "delivery.v1.environment.updated", updated[0].Type)
		assert.Equal(t, e.ID(), updated[0].AggregateID)
	})

	t.Run("one transaction can change two objects", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "mainline")
		before := f.outbox.count()

		app, ch, err := f.svc.CreateApplication(svcCtx, svcAlice, &CreateApplicationInput{
			OrganizationID: svcOrg, ProjectID: f.project.ID(), DisplayName: "Checkout", DefaultLifecycleID: l.ID(),
		})
		require.NoError(t, err)

		recorded := f.outbox.since(before)
		require.Len(t, recorded, 2)
		assert.Equal(t, "delivery.v1.application.created", recorded[0].Type)
		assert.Equal(t, AggregateApplication, recorded[0].AggregateType)
		assert.Equal(t, app.ID(), recorded[0].AggregateID)
		assert.Equal(t, "delivery.v1.channel.created", recorded[1].Type)
		assert.Equal(t, AggregateChannel, recorded[1].AggregateType)
		assert.Equal(t, ch.ID(), recorded[1].AggregateID)
	})

	t.Run("a placement has no ID of its own", func(t *testing.T) {
		f := newSvcFixture(t)
		l := f.lifecycle(t, "mainline")
		app, _ := f.app(t, "checkout", l)
		env := f.env(t, "prod")
		target := f.target(t, env, "cluster")
		before := f.outbox.count()

		f.placement(t, app, target, nil)

		recorded := f.outbox.since(before)
		require.Len(t, recorded, 1)
		assert.Equal(t, "delivery.v1.placement.created", recorded[0].Type)
		assert.Equal(t, AggregatePlacement, recorded[0].AggregateType)
		assert.Empty(t, recorded[0].AggregateID)
	})
}

func TestServiceRollsBackRecordedEvents(t *testing.T) {
	f := newSvcFixture(t)
	f.audit.err = svcErrBoom

	_, err := f.svc.CreateEnvironment(svcCtx, svcAlice, CreateEnvironmentInput{
		OrganizationID: svcOrg, WorkspaceID: f.ws.ID(), Slug: "prod", DisplayName: "Production", Tier: TierProduction,
	})

	require.ErrorIs(t, err, svcErrBoom)
	assert.Zero(t, f.outbox.count(), "an event rolls back with the change that raised it")
}

func TestServiceRecordsEachChangedObject(t *testing.T) {
	f := newSvcFixture(t)
	l := f.lifecycle(t, "mainline")
	app, _ := f.app(t, "checkout", l)
	other := f.channel(t, app, l, "canary")
	before := f.outbox.count()

	_, err := f.svc.SetDefaultChannel(svcCtx, svcAlice, svcOrg, other.ID())
	require.NoError(t, err)

	recorded := f.outbox.since(before)
	require.Len(t, recorded, 2)
	assert.Equal(t, "delivery.v1.channel.default_cleared", recorded[0].Type)
	assert.Equal(t, "delivery.v1.channel.made_default", recorded[1].Type)
	assert.Equal(t, other.ID(), recorded[1].AggregateID)
}
