// Package agents implements orchestration.AgentService: the Agent API's
// identity, team membership, and configuration. Identity stays in IAM and
// Teams keep ownership; this package joins them and owns the Agent's
// system prompt (ADR 0007).
package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/audit"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/change"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/outbox"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/team"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
	"github.com/ivaldibuild/ivaldi/pkg/transaction"
)

type ServiceOption func(*options)

type options struct {
	agents    iam.AgentRepository
	configs   orchestration.AgentConfigRepository
	teams     team.TeamService
	directory team.Directory
	authz     authorization.Authorizer
	writer    change.Writer
}

func WithAgentRepo(r iam.AgentRepository) ServiceOption {
	return func(o *options) { o.agents = r }
}
func WithConfigRepo(r orchestration.AgentConfigRepository) ServiceOption {
	return func(o *options) { o.configs = r }
}
func WithTeams(s team.TeamService) ServiceOption {
	return func(o *options) { o.teams = s }
}
func WithTeamDirectory(d team.Directory) ServiceOption {
	return func(o *options) { o.directory = d }
}
func WithAuthorizer(a authorization.Authorizer) ServiceOption {
	return func(o *options) { o.authz = a }
}
func WithTransactor(tx transaction.Transactor) ServiceOption {
	return func(o *options) { o.writer.Tx = tx }
}
func WithOutboxRecorder(r outbox.Recorder) ServiceOption {
	return func(o *options) { o.writer.Outbox = r }
}
func WithAuditRecorder(r audit.Recorder) ServiceOption {
	return func(o *options) { o.writer.Audit = r }
}

// NewService returns the AgentService, authorizing every operation, or an
// error naming a missing dependency.
func NewService(opts ...ServiceOption) (orchestration.AgentService, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.agents == nil:
		return nil, fmt.Errorf("agent service: AgentRepo is required")
	case o.configs == nil:
		return nil, fmt.Errorf("agent service: ConfigRepo is required")
	case o.teams == nil:
		return nil, fmt.Errorf("agent service: Teams is required")
	case o.directory == nil:
		return nil, fmt.Errorf("agent service: TeamDirectory is required")
	case o.authz == nil:
		return nil, fmt.Errorf("agent service: Authorizer is required")
	}
	if err := o.writer.Check(); err != nil {
		return nil, fmt.Errorf("agent service: %w", err)
	}
	return &authorized{next: &service{options: o}, authz: o.authz}, nil
}

type service struct {
	options
}

func (s *service) CreateAgent(ctx context.Context, actor identity.Actor, in orchestration.CreateAgentInput) (*orchestration.AgentDetail, error) {
	var created *team.CreatedAgent
	var config *orchestration.AgentConfig
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.teams.CreateAgent(ctx, actor, team.CreateAgentInput{
			OrganizationID: in.OrganizationID,
			TeamID:         in.TeamID,
			Slug:           in.Slug,
			DisplayName:    in.DisplayName,
			Role:           in.Role,
		})
		if err != nil {
			return err
		}
		config, err = orchestration.NewAgentConfig(orchestration.NewAgentConfigProps{
			OrganizationID: in.OrganizationID,
			AgentID:        created.Agent.ID(),
			SystemPrompt:   in.SystemPrompt,
		})
		if err != nil {
			return err
		}
		if err := s.configs.Upsert(ctx, config); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, orchestration.AggregateAgentConfig, config.AgentID(), config); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: orchestration.EventAgentConfigCreated, OrganizationID: in.OrganizationID,
			ResourceID: config.AgentID(), Actor: actor, After: config.Snapshot(),
		})
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, in.OrganizationID, created.Agent)
}

func (s *service) GetAgent(ctx context.Context, _ identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
	agent, err := s.agents.Get(ctx, organizationID, agentID)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, organizationID, agent)
}

func (s *service) ListAgents(ctx context.Context, _ identity.Actor, query orchestration.ListAgentsQuery) (*pagination.Result[orchestration.AgentSummary], error) {
	page, err := s.agents.List(ctx, iam.ListAgentsQuery{
		OrganizationID: query.OrganizationID, Search: query.Search,
		IncludeInactive: query.IncludeInactive, Page: query.Page,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(page.Items))
	for i, agent := range page.Items {
		ids[i] = agent.ID()
	}
	configs, err := s.configs.ListByAgentIDs(ctx, query.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	memberships, err := s.directory.ListTeamsForMembers(ctx, query.OrganizationID, ids)
	if err != nil {
		return nil, err
	}
	items := make([]orchestration.AgentSummary, len(page.Items))
	for i, agent := range page.Items {
		config := configs[agent.ID()]
		items[i] = summary(agent, config, memberships[agent.ID()])
	}
	return pagination.NewResult(items, page.TotalCount, page.HasNext, page.HasPrevious, page.NextCursor, page.PreviousCursor), nil
}

func (s *service) UpdateAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string, props orchestration.UpdateAgentProps) (*orchestration.AgentDetail, error) {
	var agent *iam.Agent
	err := s.writer.Tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		agent, err = s.agents.Get(ctx, organizationID, agentID)
		if err != nil {
			return err
		}
		if props.DisplayName != nil && *props.DisplayName != agent.DisplayName() {
			before := agent.Snapshot()
			if err := agent.Rename(*props.DisplayName); err != nil {
				return err
			}
			if err := s.agents.Write(ctx, agent); err != nil {
				return err
			}
			if err := s.writer.Events(ctx, iam.AggregateAgent, agent.ID(), agent); err != nil {
				return err
			}
			if err := s.writer.AuditRecord(ctx, audit.Event{
				Type: iam.EventAgentUpdated, OrganizationID: organizationID, ResourceID: agent.ID(),
				Actor: actor, Before: before, After: agent.Snapshot(),
			}); err != nil {
				return err
			}
		}
		if props.SystemPrompt != nil {
			if err := s.setSystemPrompt(ctx, actor, organizationID, agentID, *props.SystemPrompt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, organizationID, agent)
}

// setSystemPrompt updates an Agent's configuration, creating it when the
// Agent predates configuration storage.
func (s *service) setSystemPrompt(ctx context.Context, actor identity.Actor, organizationID, agentID, prompt string) error {
	config, err := s.configs.Get(ctx, organizationID, agentID)
	switch {
	case errors.Is(err, orchestration.ErrNotFound):
		config, err = orchestration.NewAgentConfig(orchestration.NewAgentConfigProps{
			OrganizationID: organizationID, AgentID: agentID, SystemPrompt: prompt,
		})
		if err != nil {
			return err
		}
		if err := s.configs.Upsert(ctx, config); err != nil {
			return err
		}
		if err := s.writer.Events(ctx, orchestration.AggregateAgentConfig, agentID, config); err != nil {
			return err
		}
		return s.writer.AuditRecord(ctx, audit.Event{
			Type: orchestration.EventAgentConfigCreated, OrganizationID: organizationID,
			ResourceID: agentID, Actor: actor, After: config.Snapshot(),
		})
	case err != nil:
		return err
	}
	if config.SystemPrompt() == prompt {
		return nil
	}
	before := config.Snapshot()
	if err := config.SetSystemPrompt(prompt); err != nil {
		return err
	}
	if err := s.configs.Upsert(ctx, config); err != nil {
		return err
	}
	if err := s.writer.Events(ctx, orchestration.AggregateAgentConfig, agentID, config); err != nil {
		return err
	}
	return s.writer.AuditRecord(ctx, audit.Event{
		Type: orchestration.EventAgentConfigUpdated, OrganizationID: organizationID,
		ResourceID: agentID, Actor: actor, Before: before, After: config.Snapshot(),
	})
}

func (s *service) SuspendAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
	agent, err := change.Run(ctx, s.writer, actor, change.Spec[*iam.Agent]{
		AggregateType: iam.AggregateAgent,
		AggregateID:   func(a *iam.Agent) string { return a.ID() },
		Load:          func(ctx context.Context) (*iam.Agent, error) { return s.agents.Get(ctx, organizationID, agentID) },
		Apply: func(_ context.Context, a *iam.Agent) error {
			if a.Status() == iam.PrincipalSuspended {
				return change.ErrUnchanged
			}
			return a.Suspend()
		},
		Save:     s.agents.Write,
		Snapshot: func(a *iam.Agent) any { return a.Snapshot() },
		Event: func(a *iam.Agent) audit.Event {
			return audit.Event{Type: iam.EventAgentSuspended, OrganizationID: organizationID, ResourceID: a.ID()}
		},
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, organizationID, agent)
}

func (s *service) ReactivateAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
	agent, err := change.Run(ctx, s.writer, actor, change.Spec[*iam.Agent]{
		AggregateType: iam.AggregateAgent,
		AggregateID:   func(a *iam.Agent) string { return a.ID() },
		Load:          func(ctx context.Context) (*iam.Agent, error) { return s.agents.Get(ctx, organizationID, agentID) },
		Apply: func(_ context.Context, a *iam.Agent) error {
			if a.Status() == iam.PrincipalActive {
				return change.ErrUnchanged
			}
			return a.Reactivate()
		},
		Save:     s.agents.Write,
		Snapshot: func(a *iam.Agent) any { return a.Snapshot() },
		Event: func(a *iam.Agent) audit.Event {
			return audit.Event{Type: iam.EventAgentReactivated, OrganizationID: organizationID, ResourceID: a.ID()}
		},
	})
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, organizationID, agent)
}

func (s *service) detail(ctx context.Context, organizationID string, agent *iam.Agent) (*orchestration.AgentDetail, error) {
	config, err := s.configs.Get(ctx, organizationID, agent.ID())
	switch {
	case errors.Is(err, orchestration.ErrNotFound):
		config = nil
	case err != nil:
		return nil, err
	}
	memberships, err := s.directory.ListTeamsForMembers(ctx, organizationID, []string{agent.ID()})
	if err != nil {
		return nil, err
	}
	return detail(agent, config, memberships[agent.ID()]), nil
}

func summary(agent *iam.Agent, config *orchestration.AgentConfig, teams []team.Membership) orchestration.AgentSummary {
	s := agent.Snapshot()
	return orchestration.AgentSummary{
		ID: s.ID, OrganizationID: s.OrganizationID, Kind: s.Kind, DisplayName: s.DisplayName,
		Status: s.Status, Metadata: s.Metadata, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Slug: s.Slug, Teams: nonNil(teams),
		HasSystemPrompt: config != nil && config.SystemPrompt() != "",
	}
}

func detail(agent *iam.Agent, config *orchestration.AgentConfig, teams []team.Membership) *orchestration.AgentDetail {
	s := agent.Snapshot()
	prompt := ""
	if config != nil {
		prompt = config.SystemPrompt()
	}
	return &orchestration.AgentDetail{
		ID: s.ID, OrganizationID: s.OrganizationID, Kind: s.Kind, DisplayName: s.DisplayName,
		Status: s.Status, Metadata: s.Metadata, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		Slug: s.Slug, SystemPrompt: prompt, Teams: nonNil(teams),
	}
}

func nonNil(memberships []team.Membership) []team.Membership {
	if memberships == nil {
		return []team.Membership{}
	}
	return memberships
}
