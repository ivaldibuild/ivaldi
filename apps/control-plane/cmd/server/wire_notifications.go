package server

import (
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	iampg "github.com/ivaldibuild/ivaldi-control-plane/internal/iam/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications/inbox"
	notificationspg "github.com/ivaldibuild/ivaldi-control-plane/internal/notifications/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/notifications/routing"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/policy/approvals"
	policypg "github.com/ivaldibuild/ivaldi-control-plane/internal/policy/postgres"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	teampg "github.com/ivaldibuild/ivaldi-control-plane/internal/team/postgres"
	"github.com/ivaldibuild/ivaldi/pkg/events"
	"github.com/ivaldibuild/ivaldi/pkg/events/redisstream"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

// wireNotifications tells people about work that waits for them: a
// consumer reads the outbox stream with its own group and records
// notifications, and the inbox serves them.
func (a *app) wireNotifications(cfg *config.Config, logger logging.Logger) error {
	repo := notificationspg.NewNotificationRepository(a.db)
	lookup, err := approvals.NewLookup(
		approvals.WithApprovalRequestRepo(policypg.NewApprovalRequestRepository(a.db)),
		approvals.WithHeldResources(iampg.NewHeldResourceReader(a.db)),
	)
	if err != nil {
		return err
	}
	handler, err := routing.NewHandler(
		routing.WithNotificationRepo(repo),
		routing.WithApprovals(lookup),
		routing.WithTeamMembers(team.NewMemberLookup(teampg.NewTeamRepository(a.db))),
		routing.WithContacts(iampg.NewContactReader(a.db)),
		routing.WithTransactor(a.tx),
		routing.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	var router events.Router
	handler.Register(&router)

	name, err := consumerName(cfg)
	if err != nil {
		return err
	}
	a.notificationEvents, err = redisstream.NewConsumer(redisstream.ConsumerConfig{
		Client:           a.redis,
		Stream:           cfg.Outbox.Stream,
		Group:            cfg.Notifications.ConsumerGroup,
		Consumer:         name,
		Handler:          &router,
		Batch:            100,
		Concurrency:      cfg.Notifications.Concurrency,
		BlockTimeout:     cfg.Events.BlockTimeout,
		ClaimIdleTimeout: cfg.Events.ClaimIdleTimeout,
		MaxDeliveries:    cfg.Events.MaxDeliveries,
		DeadLetterStream: cfg.Notifications.DeadLetterStream,
		Logger:           logger,
	})
	if err != nil {
		return fmt.Errorf("notifications consumer: %w", err)
	}

	a.services.notifications, err = inbox.NewService(inbox.WithNotificationRepo(repo), inbox.WithAuthorizer(a.authz))
	return err
}
