package apiclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrNotClusterAgent matches a 403 from an agent operation: the caller
	// is not the agent of any active Cluster, usually because another
	// agent was enrolled for it.
	ErrNotClusterAgent = errors.New("the control plane says this agent does not serve the cluster")
	// ErrInvalid matches a 422: the control plane refused what was sent,
	// such as a heartbeat naming a Target that is no longer active.
	ErrInvalid = errors.New("the control plane refused the request as invalid")
	// ErrConflict matches a 409, such as a grant asked for a Deployment
	// that is no longer applying.
	ErrConflict = errors.New("the control plane refused the request in the current state")
)

// StatusError is a control-plane response other than success.
type StatusError struct {
	Operation string
	Status    int
	Detail    string
}

func (e *StatusError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s: the control plane answered %d", e.Operation, e.Status)
	}
	return fmt.Sprintf("%s: the control plane answered %d: %s", e.Operation, e.Status, e.Detail)
}

// Is makes errors.Is match ErrNotClusterAgent and ErrInvalid by status.
func (e *StatusError) Is(target error) bool {
	switch target {
	case ErrNotClusterAgent:
		return e.Status == http.StatusForbidden
	case ErrInvalid:
		return e.Status == http.StatusUnprocessableEntity
	case ErrConflict:
		return e.Status == http.StatusConflict
	}
	return false
}

// ControlPlane is the agent operations the agent calls in its loops.
type ControlPlane interface {
	// Targets returns the agent's Cluster, its Targets, and the intervals
	// the agent runs at.
	Targets(ctx context.Context) (*TargetsResponse, error)
	// Heartbeat reports the agent's contact and what it can do in each
	// Target.
	Heartbeat(ctx context.Context, body HeartbeatRequestBody) error
	// Assignments returns the in-flight Deployments on the Cluster's
	// Targets.
	Assignments(ctx context.Context) ([]DeploymentAssignment, error)
	// ReportDeployment records one report about a Deployment. A report
	// with a report ID already recorded is stored once.
	ReportDeployment(ctx context.Context, deploymentID string, body ReportDeploymentRequestBody) error
	// Observe reports the images an Application runs on a Target and
	// returns the Deployment the control plane answered with. An
	// observation with a report ID already recorded is stored once.
	Observe(ctx context.Context, body ObservationRequestBody) (*DeploymentSnapshot, error)
	// SecretGrants asks for access to the Secret Stores an applying
	// Deployment reads, in the modes that need a grant. What it returns
	// holds credentials.
	SecretGrants(ctx context.Context, deploymentID string) ([]GrantView, error)
}

// NewControlPlane returns the ControlPlane at serverURL. client must
// authenticate as the agent.
func NewControlPlane(serverURL string, client *http.Client) (ControlPlane, error) {
	c, err := NewClientWithResponses(serverURL, WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("control plane client: %w", err)
	}
	return &controlPlane{client: c}, nil
}

type controlPlane struct {
	client *ClientWithResponses
}

func (c *controlPlane) Targets(ctx context.Context) (*TargetsResponse, error) {
	resp, err := c.client.AgentTargetsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent targets: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, statusError("agent targets", resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}
	return &resp.JSON200.Data, nil
}

func (c *controlPlane) Heartbeat(ctx context.Context, body HeartbeatRequestBody) error {
	resp, err := c.client.AgentHeartbeatWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("agent heartbeat: %w", err)
	}
	if resp.StatusCode() != http.StatusNoContent {
		return statusError("agent heartbeat", resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}
	return nil
}

func (c *controlPlane) Assignments(ctx context.Context) ([]DeploymentAssignment, error) {
	resp, err := c.client.AgentAssignmentsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("agent assignments: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, statusError("agent assignments", resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}
	return resp.JSON200.Data, nil
}

func (c *controlPlane) ReportDeployment(ctx context.Context, deploymentID string, body ReportDeploymentRequestBody) error {
	resp, err := c.client.ReportDeploymentWithResponse(ctx, deploymentID, body)
	if err != nil {
		return fmt.Errorf("report deployment %s: %w", deploymentID, err)
	}
	if resp.JSON200 == nil {
		return statusError("report deployment "+deploymentID, resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}
	return nil
}

func (c *controlPlane) Observe(ctx context.Context, body ObservationRequestBody) (*DeploymentSnapshot, error) {
	resp, err := c.client.AgentObservationWithResponse(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("agent observation: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, statusError("agent observation", resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}
	return &resp.JSON200.Data, nil
}

func (c *controlPlane) SecretGrants(ctx context.Context, deploymentID string) ([]GrantView, error) {
	resp, err := c.client.AgentSecretGrantsWithResponse(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("secret grants for %s: %w", deploymentID, err)
	}
	if resp.JSON200 == nil {
		return nil, statusError("secret grants for "+deploymentID, resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}
	return resp.JSON200.Data.Grants, nil
}

// Transient reports whether err is worth retrying: no response, or a
// response saying the control plane is busy or failing.
func Transient(err error) bool {
	var status *StatusError
	if !errors.As(err, &status) {
		return true
	}
	return status.Status == http.StatusTooManyRequests || status.Status >= 500
}

func statusError(operation string, status int, problem *ErrorModel) error {
	out := &StatusError{Operation: operation, Status: status}
	if problem != nil && problem.Detail != nil {
		out.Detail = *problem.Detail
	}
	return out
}
