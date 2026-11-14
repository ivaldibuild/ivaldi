package agent

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/apiclient"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/report"
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

	slots   chan struct{}
	mu      sync.Mutex
	running map[string]bool
	wg      sync.WaitGroup
}

func newExecutor(cp apiclient.ControlPlane, deployers map[string]deploy.Deployer, reporter report.Reporter, log *slog.Logger, max int) *executor {
	return &executor{
		cp: cp, deployers: deployers, reporter: reporter, log: log, timeout: applyTimeout,
		slots: make(chan struct{}, max), running: map[string]bool{},
	}
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
		log.Info(message, "step", step)
		_ = e.reporter.Send(applyCtx, a.DeploymentID, report.Report{
			Step: string(step), Kind: report.KindProgress,
			Detail: map[string]any{"step": string(step), "message": message},
		})
	}

	var applied deploy.Applied
	deployer, ok := e.deployers[a.Deployer]
	err := error(deploy.Failf("this agent has no %q deployer", a.Deployer))
	if ok {
		applied, err = deployer.Apply(applyCtx, a, progress)
	}
	if ctx.Err() != nil {
		log.Info("stopped mid-apply; the deployment stays applying")
		return
	}

	final, done := context.WithTimeout(context.WithoutCancel(ctx), finalReportTimeout)
	defer done()
	if err != nil {
		failure := deploy.AsFailure(err)
		if applyCtx.Err() != nil {
			failure = &deploy.Failure{Reason: "the apply did not finish within " + e.timeout.String(), Err: err}
		}
		log.Error("the apply failed", "reason", failure.Error())
		detail := map[string]any{"reason": failure.Error()}
		for k, v := range failure.Detail {
			detail[k] = v
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
	_ = e.reporter.Send(final, a.DeploymentID, report.Report{
		Step: "applied", Kind: report.KindStatusChanged, Status: apiclient.ReportDeploymentRequestBodyStatusProgressing,
		Detail: detail,
	})
}
