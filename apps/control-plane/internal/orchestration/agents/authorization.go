package agents

import (
	"context"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/orchestration"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
	"github.com/ivaldibuild/ivaldi/pkg/pagination"
)

// authorized asks the authorizer before each operation and passes its
// decision to next in the context. Every operation is checked on the
// Organization: agent.read to read, agent.manage to change.
type authorized struct {
	next  orchestration.AgentService
	authz authorization.Authorizer
}

var _ orchestration.AgentService = (*authorized)(nil)

func (a *authorized) read(ctx context.Context, actor identity.Actor, organizationID, action string) error {
	_, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionAgentRead, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action,
	})
	return err
}

func (a *authorized) manage(ctx context.Context, actor identity.Actor, organizationID, action string, proposal any) (context.Context, error) {
	d, err := authorization.Require(ctx, a.authz, authorization.Request{
		Actor: actor, Permission: iam.PermissionAgentManage, OrganizationID: organizationID,
		ResourceID: organizationID, Action: action, Proposal: proposal,
	})
	if err != nil {
		return nil, err
	}
	return authorization.WithDecision(ctx, "", d), nil
}

func (a *authorized) CreateAgent(ctx context.Context, actor identity.Actor, in orchestration.CreateAgentInput) (*orchestration.AgentDetail, error) {
	ctx, err := a.manage(ctx, actor, in.OrganizationID, orchestration.ActionAgentCreate, in)
	if err != nil {
		return nil, err
	}
	return a.next.CreateAgent(ctx, actor, in)
}

func (a *authorized) GetAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
	if err := a.read(ctx, actor, organizationID, orchestration.ActionAgentRead); err != nil {
		return nil, err
	}
	return a.next.GetAgent(ctx, actor, organizationID, agentID)
}

func (a *authorized) ListAgents(ctx context.Context, actor identity.Actor, query orchestration.ListAgentsQuery) (*pagination.Result[orchestration.AgentSummary], error) {
	if err := a.read(ctx, actor, query.OrganizationID, orchestration.ActionAgentList); err != nil {
		return nil, err
	}
	return a.next.ListAgents(ctx, actor, query)
}

func (a *authorized) UpdateAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string, props orchestration.UpdateAgentProps) (*orchestration.AgentDetail, error) {
	ctx, err := a.manage(ctx, actor, organizationID, orchestration.ActionAgentUpdate, props)
	if err != nil {
		return nil, err
	}
	return a.next.UpdateAgent(ctx, actor, organizationID, agentID, props)
}

func (a *authorized) SuspendAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
	ctx, err := a.manage(ctx, actor, organizationID, orchestration.ActionAgentSuspend, agentID)
	if err != nil {
		return nil, err
	}
	return a.next.SuspendAgent(ctx, actor, organizationID, agentID)
}

func (a *authorized) ReactivateAgent(ctx context.Context, actor identity.Actor, organizationID, agentID string) (*orchestration.AgentDetail, error) {
	ctx, err := a.manage(ctx, actor, organizationID, orchestration.ActionAgentReactivate, agentID)
	if err != nil {
		return nil, err
	}
	return a.next.ReactivateAgent(ctx, actor, organizationID, agentID)
}
