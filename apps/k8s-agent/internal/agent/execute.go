package agent

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/secrets"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/values"
)

// applyTimeout bounds one apply, from pulling the chart to Helm's return.
const applyTimeout = 10 * time.Minute

// finalReportTimeout bounds the report that ends an apply, which is sent even
// while the agent is stopping.
const finalReportTimeout = 30 * time.Second

// executor applies the Deployments assigned to the agent: each one in
// applying, once, at most max at a time.
type executor struct {
	cp        apiclient.ControlPlane
	deployers map[string]deploy.Deployer
	reporter  report.Reporter
	log       *slog.Logger
	timeout   time.Duration
	secrets   secrets.Resolver
	writer    secrets.Writer
	masker    *secrets.Masker

	slots   chan struct{}
	mu      sync.Mutex
	running map[string]bool
	wg      sync.WaitGroup
}

func newExecutor(cp apiclient.ControlPlane, deployers map[string]deploy.Deployer, reporter report.Reporter, log *slog.Logger, max int) *executor {
	return &executor{
		cp: cp, deployers: deployers, reporter: reporter, log: log, timeout: applyTimeout,
		slots: make(chan struct{}, max), running: map[string]bool{}, masker: secrets.NewMasker(),
	}
}

// withSecrets lets the executor resolve secrets.
func (e *executor) withSecrets(r secrets.Resolver, w secrets.Writer, m *secrets.Masker) *executor {
	e.secrets, e.writer, e.masker = r, w, m
	return e
}

// deployerNames lists the deployers, for the heartbeat.
func deployerNames(deployers map[string]deploy.Deployer) []string {
	return slices.Sorted(maps.Keys(deployers))
}

// poll fetches the assignments and starts an apply for each Deployment in
// applying that is not already running, while slots are free. The rest are
// picked up by a later poll. It returns the assignments.
func (e *executor) poll(ctx context.Context, clusterSlug string) ([]apiclient.DeploymentAssignment, error) {
	assignments, err := e.cp.Assignments(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range assignments {
		if a.Deployment.Status != apiclient.DeploymentSnapshotStatusApplying {
			continue
		}
		id := a.Deployment.Id
		e.mu.Lock()
		busy := e.running[id]
		e.mu.Unlock()
		if busy {
			continue
		}
		select {
		case e.slots <- struct{}{}:
		default:
			return assignments, nil
		}
		e.mu.Lock()
		e.running[id] = true
		e.mu.Unlock()
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			defer func() {
				e.mu.Lock()
				delete(e.running, id)
				e.mu.Unlock()
				<-e.slots
			}()
			e.apply(ctx, deploy.FromAPI(a, clusterSlug))
		}()
	}
	return assignments, nil
}

// boundPaths returns the assignment's secrets with the bindings in their
// paths filled in, so one reference can serve every Environment:
// payments/#{Ivaldi.Environment.Slug}/database.
func boundPaths(a deploy.Assignment) ([]apiclient.AssignmentSecret, error) {
	paths := make([]string, len(a.SecretRefs))
	for i, s := range a.SecretRefs {
		paths[i] = s.Ref.Path
	}
	bound, err := values.Bind(a.System, a.Variables, paths)
	if err != nil {
		return nil, &deploy.Failure{Reason: "could not fill in a secret's path", Err: err}
	}
	out := slices.Clone(a.SecretRefs)
	for i := range out {
		out[i].Ref.Path = bound[i]
	}
	return out, nil
}

// resolveSecrets resolves the assignment's secrets and writes those
// delivered as a Secret, filling a's Secrets, SecretName, and SecretPins.
func (e *executor) resolveSecrets(ctx context.Context, a *deploy.Assignment, progress deploy.Progress) ([]secrets.Resolved, error) {
	if len(a.SecretRefs) == 0 {
		return nil, nil
	}
	if e.secrets == nil {
		return nil, deploy.Failf("this agent cannot resolve secrets")
	}
	progress(deploy.StepResolving, fmt.Sprintf("resolving %d secrets", len(a.SecretRefs)))
	refs, err := boundPaths(*a)
	if err != nil {
		return nil, err
	}
	resolved, err := e.secrets.Resolve(ctx, a.Namespace, refs)
	if err != nil {
		return nil, err
	}
	for _, r := range resolved {
		a.Secrets = append(a.Secrets, values.Secret{Name: r.Name, Value: r.Value, ValuesPath: r.ValuesPath, AsValue: r.AsValue})
		a.SecretPins = append(a.SecretPins, r.Pin())
	}
	a.SecretName, err = e.writer.Write(ctx, a.Namespace, a.ApplicationID, a.DeploymentID, resolved)
	return resolved, err
}

// wait waits for the applies in flight to finish their reports.
func (e *executor) wait() { e.wg.Wait() }

// apply applies one Deployment and reports how it went. When ctx ends
// mid-apply, because the agent lost its Lease or is stopping, it reports
// nothing: the next agent finds the Deployment still applying and resumes.
func (e *executor) apply(ctx context.Context, a deploy.Assignment) {
	log := e.log.With("deployment_id", a.DeploymentID, "target_id", a.TargetID, "namespace", a.Namespace)
	applyCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	progress := func(step deploy.Step, message string) {
		message = e.masker.Mask(message)
		log.Info(message, "step", step)
		_ = e.reporter.Send(applyCtx, a.DeploymentID, report.Report{
			Step: string(step), Kind: report.KindProgress,
			Detail: map[string]any{"step": string(step), "message": message},
		})
	}

	var applied deploy.Applied
	resolved, err := e.resolveSecrets(applyCtx, &a, progress)
	defer func() {
		if e.secrets != nil {
			e.secrets.Release(resolved)
		}
	}()
	if err == nil {
		deployer, ok := e.deployers[a.Deployer]
		err = deploy.Failf("this agent has no %q deployer", a.Deployer)
		if ok {
			applied, err = deployer.Apply(applyCtx, a, progress)
		}
	}
	if ctx.Err() != nil {
		log.Info("stopped mid-apply; the deployment stays applying")
		return
	}
	// An earlier attempt applied this Deployment and kept no pins, so what
	// the chart holds may not be what this attempt resolved. Recording this
	// attempt's versions would be a guess; saying so is not.
	if err == nil && applied.Skipped && len(a.SecretPins) > 0 && applied.SecretPins == nil {
		err = deploy.Failf("an earlier attempt applied this deployment without keeping the versions of its secrets; request the deployment again")
	}

	final, done := context.WithTimeout(context.WithoutCancel(ctx), finalReportTimeout)
	defer done()
	if err != nil {
		failure := deploy.AsFailure(err)
		if applyCtx.Err() != nil {
			failure = &deploy.Failure{Reason: "the apply did not finish within " + e.timeout.String(), Err: err}
		}
		log.Error("the apply failed", "reason", failure.Error())
		detail := map[string]any{"reason": e.masker.Mask(failure.Error())}
		for k, v := range failure.Detail {
			detail[k] = e.masker.MaskAny(v)
		}
		_ = e.reporter.Send(final, a.DeploymentID, report.Report{
			Step: "failed", Kind: report.KindStatusChanged, Status: apiclient.ReportDeploymentRequestBodyStatusFailed, Detail: detail,
		})
		return
	}
	log.Info("applied", "skipped", applied.Skipped, "ref", applied.Ref)
	detail := map[string]any{"external_ref": applied.Ref, "already_applied": applied.Skipped}
	if a.IsRollback() {
		detail["restores"] = a.Restores
	}
	// The pins a rollback replays, never the values: the ones the apply that
	// committed kept when this attempt found it already applied.
	pins := a.SecretPins
	if applied.Skipped && applied.SecretPins != nil {
		pins = applied.SecretPins
	}
	if len(pins) > 0 {
		detail["secrets"] = pins
	}
	if len(applied.SecretsInValues) > 0 {
		// These are in the deployer's records too, such as Helm's release
		// history, which the deploy sheet warns about.
		detail["secrets_in_values"] = applied.SecretsInValues
	}
	_ = e.reporter.Send(final, a.DeploymentID, report.Report{
		Step: "applied", Kind: report.KindStatusChanged, Status: apiclient.ReportDeploymentRequestBodyStatusProgressing,
		Detail: detail,
	})
}
