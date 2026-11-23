package grants

type GrantRequest struct {
	DeploymentID string `path:"deployment_id" doc:"The applying Deployment whose secrets the agent is about to read"`
}
