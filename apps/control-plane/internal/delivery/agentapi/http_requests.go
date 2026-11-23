package agentapi

import (
	"encoding/json"
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
)

type HeartbeatRequest struct {
	Body struct {
		AgentVersion      string                   `json:"agent_version" required:"true" minLength:"1" maxLength:"255" doc:"The agent's version"`
		KubernetesVersion string                   `json:"kubernetes_version" required:"true" minLength:"1" maxLength:"255" doc:"The Kubernetes version the agent runs on"`
		WatchMode         string                   `json:"watch_mode" required:"true" enum:"cluster,namespaces" doc:"Whether the agent watches the whole cluster or only listed namespaces"`
		Deployers         []string                 `json:"deployers,omitempty" required:"false" doc:"The deployers the agent has, such as helm"`
		SecretStoreKinds  []string                 `json:"secret_store_kinds,omitempty" required:"false" doc:"The secret-store kinds the agent can read"`
		SecretCredentials []string                 `json:"secret_store_credentials,omitempty" required:"false" doc:"The names of the credentials the agent's operator configured for secret stores, never their values"`
		Targets           []TargetHeartbeatRequest `json:"targets,omitempty" required:"false" doc:"What the agent can do in each Target's namespace"`
	}
}

type TargetHeartbeatRequest struct {
	TargetID string `json:"target_id" required:"true" doc:"The Target"`
	Observe  bool   `json:"observe" doc:"Whether the agent can read the namespace"`
	Execute  bool   `json:"execute" doc:"Whether the agent can change the namespace"`
	Reason   string `json:"reason,omitempty" required:"false" maxLength:"512" doc:"Why a capability is false, when it is"`
}

func (r *HeartbeatRequest) input() HeartbeatInput {
	in := HeartbeatInput{
		AgentVersion:      r.Body.AgentVersion,
		KubernetesVersion: r.Body.KubernetesVersion,
		WatchMode:         r.Body.WatchMode,
		Deployers:         r.Body.Deployers,
		SecretStoreKinds:  r.Body.SecretStoreKinds,
		SecretCredentials: r.Body.SecretCredentials,
	}
	for _, target := range r.Body.Targets {
		in.Targets = append(in.Targets, TargetHeartbeat{
			TargetID: target.TargetID, Observe: target.Observe, Execute: target.Execute, Reason: target.Reason,
		})
	}
	return in
}

type ObservationRequest struct {
	Body struct {
		ReportID      string                      `json:"report_id" required:"true" minLength:"1" maxLength:"255" doc:"The agent's idempotent report ID"`
		TargetID      string                      `json:"target_id" required:"true" doc:"The Target the images run on"`
		ApplicationID string                      `json:"application_id" required:"true" doc:"The Application the images belong to"`
		Artifacts     []ObservedArtifactRequest   `json:"artifacts" required:"true" minItems:"1" maxItems:"500" doc:"The images the agent saw, by published artifact name"`
		Workloads     []delivery.ObservedWorkload `json:"workloads,omitempty" required:"false" maxItems:"500" doc:"The workloads the images came from, kept on the Deployment's first status event"`
		ExternalRef   json.RawMessage             `json:"external_ref,omitempty" required:"false" doc:"Who deployed it, when the agent can tell"`
		ObservedAt    *time.Time                  `json:"observed_at,omitempty" required:"false" doc:"When the agent saw it, by its clock"`
	}
}

type ObservedArtifactRequest struct {
	ArtifactName string `json:"artifact_name" required:"true" doc:"The image's published name, such as ghcr.io/acme/api"`
	Digest       string `json:"digest" required:"true" doc:"The image's digest, such as sha256:…"`
}

func (r *ObservationRequest) input(organizationID string) delivery.ObserveInput {
	in := delivery.ObserveInput{
		OrganizationID: organizationID,
		TargetID:       r.Body.TargetID,
		ApplicationID:  r.Body.ApplicationID,
		Workloads:      r.Body.Workloads,
		ExternalRef:    r.Body.ExternalRef,
		ReportID:       r.Body.ReportID,
		ObservedAt:     r.Body.ObservedAt,
	}
	for _, artifact := range r.Body.Artifacts {
		in.Artifacts = append(in.Artifacts, delivery.ObservedArtifact{
			ArtifactName: artifact.ArtifactName, Digest: artifact.Digest,
		})
	}
	return in
}
