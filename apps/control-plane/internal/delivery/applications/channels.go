package applications

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// CreateChannel adds a Channel to an active Application.
func (s *service) CreateChannel(ctx context.Context, actor identity.Actor, in delivery.CreateChannelInput) (*delivery.Channel, error) {
	rules := make([]delivery.NewChannelRuleProps, len(in.Rules))
	for i, r := range in.Rules {
		rules[i] = delivery.NewChannelRuleProps{Kind: r.Kind, ArtifactID: r.ArtifactID, Config: r.Config}
	}
	c, err := delivery.NewChannel(delivery.NewChannelProps{
		OrganizationID: in.OrganizationID,
		ApplicationID:  in.ApplicationID,
		LifecycleID:    in.LifecycleID,
		Slug:           in.Slug,
		DisplayName:    in.DisplayName,
		Description:    &in.Description,
		Rules:          rules,
	}, nil)
	if err != nil {
		return nil, err
	}
	err = s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		app, err := s.activeApplication(ctx, in.OrganizationID, in.ApplicationID)
		if err != nil {
			return err
		}
		if _, err := s.lifecycleFor(ctx, in.OrganizationID, app.WorkspaceID(), c.LifecycleID()); err != nil {
			return err
		}
		if err := s.checkRuleArtifacts(ctx, c); err != nil {
			return err
		}
		if err := s.applications.CreateChannel(ctx, c); err != nil {
			return fmt.Errorf("create channel: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateChannel, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventChannelCreated, OrganizationID: in.OrganizationID, ResourceID: c.ID(), Actor: actor, After: c.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// checkRuleArtifacts checks that every artifact the Channel's rules are
// scoped to is an active artifact of its Application.
func (s *service) checkRuleArtifacts(ctx context.Context, c *delivery.Channel) error {
	for _, artifactID := range c.ArtifactIDs() {
		a, err := s.applications.GetArtifact(ctx, c.OrganizationID(), artifactID)
		if errors.Is(err, delivery.ErrNotFound) || (err == nil && a.ApplicationID() != c.ApplicationID()) {
			return fmt.Errorf("%w: application %s has no artifact %s", delivery.ErrNotFound, c.ApplicationID(), artifactID)
		}
		if err != nil {
			return err
		}
		if a.Archived() {
			return fmt.Errorf("%w: artifact %s", delivery.ErrArchived, artifactID)
		}
	}
	return nil
}

func (s *service) GetChannel(ctx context.Context, _ identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	return s.applications.GetChannel(ctx, organizationID, channelID)
}

func (s *service) ListChannels(ctx context.Context, _ identity.Actor, organizationID, applicationID string, includeArchived bool) ([]*delivery.Channel, error) {
	return s.applications.ListChannels(ctx, organizationID, applicationID, includeArchived)
}

func (s *service) changeChannel(ctx context.Context, actor identity.Actor, organizationID, channelID, event string, apply func(context.Context, *delivery.Channel) error) (*delivery.Channel, error) {
	return change.Run(ctx, s.w, actor, change.Spec[*delivery.Channel]{
		AggregateType: delivery.AggregateChannel,
		AggregateID:   func(c *delivery.Channel) string { return c.ID() },
		// Loaded with the Application's row locked, as a Channel's changes and
		// a variable's value scoped to it must not interleave: archiving a
		// Channel counts the values scoped to it, and a save of one holds this
		// row first.
		Load: func(ctx context.Context) (*delivery.Channel, error) {
			found, err := s.applications.GetChannel(ctx, organizationID, channelID)
			if err != nil {
				return nil, err
			}
			if _, err := s.applications.GetApplicationForUpdate(ctx, organizationID, found.ApplicationID()); err != nil {
				return nil, err
			}
			return s.applications.GetChannel(ctx, organizationID, channelID)
		},
		// An archived Application's Channels change only once it is restored.
		Apply: func(ctx context.Context, c *delivery.Channel) error {
			if _, err := s.activeApplication(ctx, organizationID, c.ApplicationID()); err != nil {
				return err
			}
			return apply(ctx, c)
		},
		Save:     s.applications.UpdateChannel,
		Snapshot: func(c *delivery.Channel) any { return c.Snapshot() },
		Event: func(c *delivery.Channel) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: c.ID()}
		},
	})
}

// UpdateChannel changes an active Channel's slug, name, or description.
func (s *service) UpdateChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string, props delivery.UpdateChannelProps) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelUpdated,
		func(_ context.Context, c *delivery.Channel) error { return c.Update(props) })
}

// SetChannelLifecycle makes a Channel follow another active Lifecycle in
// its Application's Workspace.
func (s *service) SetChannelLifecycle(ctx context.Context, actor identity.Actor, organizationID, channelID, lifecycleID string) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelUpdated,
		func(ctx context.Context, c *delivery.Channel) error {
			app, err := s.applications.GetApplication(ctx, organizationID, c.ApplicationID())
			if err != nil {
				return err
			}
			if _, err := s.lifecycleFor(ctx, organizationID, app.WorkspaceID(), lifecycleID); err != nil {
				return err
			}
			return c.UseLifecycle(lifecycleID)
		})
}

// AddChannelRule adds a rule to a Channel.
func (s *service) AddChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID string, props delivery.ChannelRuleProps) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelUpdated,
		func(ctx context.Context, c *delivery.Channel) error {
			if _, err := c.AddRule(props); err != nil {
				return err
			}
			return s.checkRuleArtifacts(ctx, c)
		})
}

// UpdateChannelRule replaces one of a Channel's rules.
func (s *service) UpdateChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string, props delivery.ChannelRuleProps) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelUpdated,
		func(ctx context.Context, c *delivery.Channel) error {
			if err := c.UpdateRule(ruleID, props); err != nil {
				return err
			}
			return s.checkRuleArtifacts(ctx, c)
		})
}

// RemoveChannelRule removes one of a Channel's rules.
func (s *service) RemoveChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelUpdated,
		func(_ context.Context, c *delivery.Channel) error { return c.RemoveRule(ruleID) })
}

// SetDefaultChannel makes an active Channel its active Application's
// default, in place of the current one. Making the default the default
// does nothing.
func (s *service) SetDefaultChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	var c *delivery.Channel
	err := s.w.Tx.WithinTx(ctx, func(ctx context.Context) error {
		found, err := s.applications.GetChannel(ctx, organizationID, channelID)
		if err != nil {
			return err
		}
		// Locked so two moves of the default for one Application run one at a time.
		app, err := s.applications.GetApplicationForUpdate(ctx, organizationID, found.ApplicationID())
		if err != nil {
			return err
		}
		if app.Archived() {
			return fmt.Errorf("%w: application %s", delivery.ErrArchived, app.ID())
		}
		// Read again under the lock.
		c, err = s.applications.GetChannel(ctx, organizationID, channelID)
		if err != nil {
			return err
		}
		if c.IsDefault() {
			return nil
		}
		current, err := s.applications.GetDefaultChannel(ctx, organizationID, app.ID())
		if err != nil && !errors.Is(err, delivery.ErrNotFound) {
			return fmt.Errorf("get default channel: %w", err)
		}
		if err := c.MakeDefault(); err != nil {
			return err
		}
		// Clear the old default first: at most one default is allowed at a time.
		var before string
		if current != nil {
			before = current.ID()
			current.ClearDefault()
			if err := s.applications.UpdateChannel(ctx, current); err != nil {
				return fmt.Errorf("clear default channel: %w", err)
			}
			if err := s.w.Events(ctx, delivery.AggregateChannel, current.ID(), current); err != nil {
				return err
			}
		}
		if err := s.applications.UpdateChannel(ctx, c); err != nil {
			return fmt.Errorf("set default channel: %w", err)
		}
		if err := s.w.Events(ctx, delivery.AggregateChannel, c.ID(), c); err != nil {
			return err
		}
		return s.w.AuditRecord(ctx, audit.Event{Type: delivery.EventChannelDefaultMoved, OrganizationID: organizationID, ResourceID: c.ID(), Actor: actor, Before: before, After: c.ID()})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ArchiveChannel archives a Channel that is not its Application's default
// and that no variable's value is scoped to.
// Its Releases keep referring to it. Archiving an archived one does nothing.
func (s *service) ArchiveChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelArchived,
		func(ctx context.Context, c *delivery.Channel) error {
			if c.Archived() {
				return change.ErrUnchanged
			}
			n, err := s.variables.CountActiveVariablesWithChannel(ctx, organizationID, c.ID())
			if err != nil {
				return fmt.Errorf("count variables: %w", err)
			}
			if n > 0 {
				return fmt.Errorf("%w: %d variables have a value scoped to channel %s", delivery.ErrInUse, n, c.ID())
			}
			return c.Archive()
		})
}

// RestoreChannel restores an archived Channel of an active Application whose
// Lifecycle, and the artifacts its rules are scoped to, are still active.
func (s *service) RestoreChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*delivery.Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, delivery.EventChannelRestored,
		func(ctx context.Context, c *delivery.Channel) error {
			if !c.Archived() {
				return change.ErrUnchanged
			}
			app, err := s.applications.GetApplication(ctx, organizationID, c.ApplicationID())
			if err != nil {
				return err
			}
			if _, err := s.lifecycleFor(ctx, organizationID, app.WorkspaceID(), c.LifecycleID()); err != nil {
				return err
			}
			if err := s.checkRuleArtifacts(ctx, c); err != nil {
				return err
			}
			c.Restore()
			return nil
		})
}
