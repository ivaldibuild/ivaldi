package delivery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// CreateChannelInput describes a new, non-default Channel.
type CreateChannelInput struct {
	OrganizationID string
	ApplicationID  string
	// LifecycleID must be an active Lifecycle in the Application's Workspace.
	LifecycleID string
	// Slug is derived from DisplayName when empty.
	Slug        string
	DisplayName string
	Description string
	Rules       []ChannelRuleProps
}

// CreateChannel adds a Channel to an active Application.
func (s *Service) CreateChannel(ctx context.Context, actor identity.Actor, in CreateChannelInput) (*Channel, error) {
	rules := make([]NewChannelRuleProps, len(in.Rules))
	for i, r := range in.Rules {
		rules[i] = NewChannelRuleProps{Kind: r.Kind, ArtifactID: r.ArtifactID, Config: r.Config}
	}
	c, err := NewChannel(NewChannelProps{
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
	d, err := s.authorizeApplication(ctx, actor, PermissionApplicationManage, ActionChannelCreate, in.OrganizationID, in.ApplicationID, in)
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
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
		return s.record(ctx, d, audit.Event{Type: EventChannelCreated, OrganizationID: in.OrganizationID, ResourceID: c.ID(), Actor: actor, After: c.Snapshot()})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// checkRuleArtifacts checks that every artifact the Channel's rules are
// scoped to is an active artifact of its Application.
func (s *Service) checkRuleArtifacts(ctx context.Context, c *Channel) error {
	for _, artifactID := range c.ArtifactIDs() {
		a, err := s.applications.GetArtifact(ctx, c.OrganizationID(), artifactID)
		if errors.Is(err, ErrNotFound) || (err == nil && a.ApplicationID() != c.ApplicationID()) {
			return fmt.Errorf("%w: application %s has no artifact %s", ErrNotFound, c.ApplicationID(), artifactID)
		}
		if err != nil {
			return err
		}
		if a.Archived() {
			return fmt.Errorf("%w: artifact %s", ErrArchived, artifactID)
		}
	}
	return nil
}

// GetChannel returns a Channel if the actor may read its Application.
func (s *Service) GetChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error) {
	c, err := s.applications.GetChannel(ctx, organizationID, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorizeApplication(ctx, actor, PermissionApplicationRead, ActionChannelRead, organizationID, c.ApplicationID(), nil); err != nil {
		return nil, err
	}
	return c, nil
}

// ListChannels lists an Application's Channels.
func (s *Service) ListChannels(ctx context.Context, actor identity.Actor, organizationID, applicationID string, includeArchived bool) ([]*Channel, error) {
	if _, err := s.authorizeApplication(ctx, actor, PermissionApplicationRead, ActionChannelList, organizationID, applicationID, nil); err != nil {
		return nil, err
	}
	return s.applications.ListChannels(ctx, organizationID, applicationID, includeArchived)
}

func (s *Service) changeChannel(ctx context.Context, actor identity.Actor, organizationID, channelID, action, event string, proposal any, apply func(context.Context, *Channel) error) (*Channel, error) {
	return run(ctx, s, actor, change[*Channel]{
		load: func(ctx context.Context) (*Channel, error) {
			return s.applications.GetChannel(ctx, organizationID, channelID)
		},
		authorize: func(ctx context.Context, c *Channel) (authorization.Decision, error) {
			return s.authorizeApplication(ctx, actor, PermissionApplicationManage, action, organizationID, c.ApplicationID(), proposal)
		},
		// An archived Application's Channels change only once it is restored.
		apply: func(ctx context.Context, c *Channel) error {
			if _, err := s.activeApplication(ctx, organizationID, c.ApplicationID()); err != nil {
				return err
			}
			return apply(ctx, c)
		},
		save:     s.applications.UpdateChannel,
		snapshot: func(c *Channel) any { return c.Snapshot() },
		event: func(c *Channel) audit.Event {
			return audit.Event{Type: event, OrganizationID: organizationID, ResourceID: c.ID()}
		},
	})
}

// UpdateChannel changes an active Channel's slug, name, or description.
func (s *Service) UpdateChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string, props UpdateChannelProps) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelUpdate, EventChannelUpdated, props,
		func(_ context.Context, c *Channel) error { return c.Update(props) })
}

// SetChannelLifecycle makes a Channel follow another active Lifecycle in
// its Application's Workspace.
func (s *Service) SetChannelLifecycle(ctx context.Context, actor identity.Actor, organizationID, channelID, lifecycleID string) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelSetLifecycle, EventChannelUpdated, lifecycleID,
		func(ctx context.Context, c *Channel) error {
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
func (s *Service) AddChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID string, props ChannelRuleProps) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelAddRule, EventChannelUpdated, props,
		func(ctx context.Context, c *Channel) error {
			if _, err := c.AddRule(props); err != nil {
				return err
			}
			return s.checkRuleArtifacts(ctx, c)
		})
}

// UpdateChannelRule replaces one of a Channel's rules.
func (s *Service) UpdateChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string, props ChannelRuleProps) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelUpdateRule, EventChannelUpdated, props,
		func(ctx context.Context, c *Channel) error {
			if err := c.UpdateRule(ruleID, props); err != nil {
				return err
			}
			return s.checkRuleArtifacts(ctx, c)
		})
}

// RemoveChannelRule removes one of a Channel's rules.
func (s *Service) RemoveChannelRule(ctx context.Context, actor identity.Actor, organizationID, channelID, ruleID string) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelRemoveRule, EventChannelUpdated, ruleID,
		func(_ context.Context, c *Channel) error { return c.RemoveRule(ruleID) })
}

// SetDefaultChannel makes an active Channel its active Application's
// default, in place of the current one.
func (s *Service) SetDefaultChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error) {
	c, err := s.applications.GetChannel(ctx, organizationID, channelID)
	if err != nil {
		return nil, err
	}
	d, err := s.authorizeApplication(ctx, actor, PermissionApplicationManage, ActionChannelSetDefault, organizationID, c.ApplicationID(), channelID)
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Locked so two moves of the default for one Application run one at a time.
		app, err := s.applications.GetApplicationForUpdate(ctx, organizationID, c.ApplicationID())
		if err != nil {
			return err
		}
		if app.Archived() {
			return fmt.Errorf("%w: application %s", ErrArchived, app.ID())
		}
		c, err = s.applications.GetChannel(ctx, organizationID, channelID)
		if err != nil {
			return err
		}
		if c.IsDefault() {
			return nil
		}
		current, err := s.applications.GetDefaultChannel(ctx, organizationID, app.ID())
		if err != nil && !errors.Is(err, ErrNotFound) {
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
		}
		if err := s.applications.UpdateChannel(ctx, c); err != nil {
			return fmt.Errorf("set default channel: %w", err)
		}
		return s.record(ctx, d, audit.Event{Type: EventChannelDefaultMoved, OrganizationID: organizationID, ResourceID: c.ID(), Actor: actor, Before: before, After: c.ID()})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ArchiveChannel archives a Channel that is not its Application's default.
// Its Releases keep referring to it.
func (s *Service) ArchiveChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelArchive, EventChannelArchived, nil,
		func(_ context.Context, c *Channel) error {
			if c.Archived() {
				return errUnchanged
			}
			return c.Archive()
		})
}

// RestoreChannel restores an archived Channel of an active Application whose
// Lifecycle, and the artifacts its rules are scoped to, are still active.
func (s *Service) RestoreChannel(ctx context.Context, actor identity.Actor, organizationID, channelID string) (*Channel, error) {
	return s.changeChannel(ctx, actor, organizationID, channelID, ActionChannelRestore, EventChannelRestored, nil,
		func(ctx context.Context, c *Channel) error {
			if !c.Archived() {
				return errUnchanged
			}
			app, err := s.activeApplication(ctx, organizationID, c.ApplicationID())
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
