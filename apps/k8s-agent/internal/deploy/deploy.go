// Package deploy defines what a deployer does with an assignment, whatever
// it applies with: Helm today, Argo CD or Flux later, behind one interface.
package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/values"
)

// The labels and annotations a deployer puts on what it applies.
const (
	// LabelApplication goes on every workload and its Pod template, so the
	// agent can find an Application's workloads.
	LabelApplication = "ivaldi.build/application"
	// AnnotationDeployment and AnnotationRelease go on every object.
	AnnotationDeployment = "ivaldi.build/deployment"
	AnnotationRelease    = "ivaldi.build/release"
)

// Artifact is an OCI artifact pinned by digest.
type Artifact struct {
	Repository string
	Digest     string
}

// Ref is the artifact's reference by digest.
func (a Artifact) Ref() string { return a.Repository + "@" + a.Digest }

// Assignment is one Deployment to apply, in the terms every deployer uses.
type Assignment struct {
	DeploymentID  string
	ApplicationID string
	ReleaseID     string
	Version       string
	TargetID      string
	Namespace     string
	Deployer      string
	// Definition is the chart.
	Definition Artifact
	// Overlays are chart-relative values files, in the order they apply.
	Overlays  []string
	Images    map[string]values.Image
	Variables []values.Variable
	// System are the system variables overlays may bind.
	System map[string]string
	// HelmReleaseName is set when the deployer is helm.
	HelmReleaseName string
	// Reason is rollout, redeploy, or rollback. A rollback is applied like
	// any other assignment: the control plane builds it from the restored
	// Deployment's chart, images, and configuration, and the deployer
	// never uses its own rollback.
	Reason string
	// Restores is the Deployment a rollback replays.
	Restores string
}

// IsRollback reports whether the assignment replays an earlier Deployment.
func (a Assignment) IsRollback() bool { return a.Reason == ReasonRollback }

// ReasonRollback is the reason of an assignment that replays an earlier
// Deployment.
const ReasonRollback = "rollback"

// Step names a stage of an apply in progress reports.
type Step string

// The steps every deployer reports.
const (
	StepFetching    Step = "fetching"
	StepResolving   Step = "resolving"
	StepApplying    Step = "applying"
	StepInterrupted Step = "interrupted"
)

// Progress receives an apply's progress.
type Progress func(step Step, message string)

// Applied is what an apply produced, for the report.
type Applied struct {
	// Skipped is true when the Deployment had already been applied, by
	// an earlier attempt that died before reporting it.
	Skipped bool
	// Ref identifies what was applied in the deployer's own terms, such as
	// a Helm release and revision.
	Ref map[string]any
}

// Deployer applies assignments.
type Deployer interface {
	// Name is the deployer's name in assignments and heartbeats.
	Name() string
	// Apply applies a, reporting progress as it goes. It is idempotent: an
	// assignment already applied is skipped. A failure the person must act
	// on is a *Failure.
	Apply(ctx context.Context, a Assignment, progress Progress) (Applied, error)
}

// Failure is an apply that failed for a reason worth showing on the
// Deployment, as opposed to the agent stopping mid-apply.
type Failure struct {
	Reason string
	// Detail is shown with the reason, such as a command that clears a
	// stuck release.
	Detail map[string]any
	Err    error
}

func (f *Failure) Error() string {
	if f.Err == nil {
		return f.Reason
	}
	return f.Reason + ": " + f.Err.Error()
}

func (f *Failure) Unwrap() error { return f.Err }

// Failf returns a *Failure with a formatted reason.
func Failf(format string, args ...any) *Failure {
	return &Failure{Reason: fmt.Sprintf(format, args...)}
}

// AsFailure returns err as a *Failure, wrapping any other error.
func AsFailure(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return &Failure{Reason: err.Error()}
}

// FromAPI converts the control plane's assignment. clusterSlug is the
// agent's Cluster, for the system variables.
func FromAPI(a apiclient.DeploymentAssignment, clusterSlug string) Assignment {
	images := make(map[string]values.Image, len(a.Apply.Images))
	for name, image := range a.Apply.Images {
		images[name] = values.Image{Repository: image.Repository, Digest: image.Digest}
	}
	var variables []values.Variable
	for _, v := range a.Apply.Variables {
		if v.Kind != "text" {
			continue
		}
		variable := values.Variable{Name: v.Name, Value: v.Value}
		if v.ValuesPath != nil {
			variable.ValuesPath = *v.ValuesPath
		}
		variables = append(variables, variable)
	}
	out := Assignment{
		DeploymentID:  a.Deployment.Id,
		ApplicationID: a.Deployment.ApplicationId,
		ReleaseID:     a.Release.Id,
		Version:       a.Release.Version,
		TargetID:      a.Target.Id,
		Namespace:     a.Apply.Namespace,
		Deployer:      string(a.Apply.Deployer),
		Definition:    Artifact{Repository: a.Apply.Definition.ArtifactName, Digest: a.Apply.Definition.Digest},
		Overlays:      a.Apply.Overlays,
		Images:        images,
		Variables:     variables,
		System: map[string]string{
			"Ivaldi.Application.Id":   a.Deployment.ApplicationId,
			"Ivaldi.Cluster.Slug":     clusterSlug,
			"Ivaldi.Deployment.Id":    a.Deployment.Id,
			"Ivaldi.Environment.Id":   a.Environment.Id,
			"Ivaldi.Environment.Slug": a.Environment.Slug,
			"Ivaldi.Release.Id":       a.Release.Id,
			"Ivaldi.Release.Version":  a.Release.Version,
			"Ivaldi.Target.Id":        a.Target.Id,
			"Ivaldi.Target.Namespace": a.Apply.Namespace,
			"Ivaldi.Target.Slug":      a.Target.Slug,
		},
	}
	if a.Apply.Helm != nil {
		out.HelmReleaseName = a.Apply.Helm.ReleaseName
	}
	out.Reason = string(a.Apply.Reason)
	if a.Apply.Restores != nil {
		out.Restores = *a.Apply.Restores
	}
	return out
}

// Values builds the assignment's values from its chart's overlay files.
func (a Assignment) Values(ctx context.Context, overlays []values.Document) (values.Result, error) {
	return values.Build(ctx, values.Input{
		Overlays:  overlays,
		System:    a.System,
		Variables: a.Variables,
		Generated: values.Generated{
			Application: a.ApplicationID,
			Release:     a.ReleaseID,
			Deployment:  a.DeploymentID,
			Version:     a.Version,
			Artifacts:   a.Images,
		},
	})
}
