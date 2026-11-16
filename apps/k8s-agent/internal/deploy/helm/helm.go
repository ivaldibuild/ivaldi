// Package helm is the Helm deployer: it pulls a chart by digest, builds its
// values, and installs or upgrades it with the Helm 4 SDK using server-side
// apply. It never rolls back: a rollback is a new Deployment of an earlier
// Release's exact configuration.
package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// Name is the deployer's name in assignments and heartbeats.
const Name = "helm"

// maxHistory is how many revisions Helm keeps per release.
const maxHistory = 10

// releaseLabel names, on a Helm revision, the Deployment that made it.
const releaseLabel = "ivaldi.build/deployment"

// pinsAnnotation names, on a Helm revision's Secret, the secret versions the
// apply that made it read. Helm keeps one revision per Secret and rebuilds a
// revision only when it supersedes it, so the annotation outlives reporting.
const pinsAnnotation = "ivaldi.build/secrets"

// maxPinsSize bounds what is kept in the annotation, which Kubernetes bounds
// at 256 KiB with everything else on the object. A Deployment whose secrets
// do not fit keeps none, and a retry is told rather than guessing.
const maxPinsSize = 128 << 10

// Config configures the Helm deployer.
type Config struct {
	RESTConfig *rest.Config
	Kube       kubernetes.Interface
	// AgentNamespace holds the registry pull Secret.
	AgentNamespace string
	// PullSecret is a docker-config Secret with registry credentials for
	// charts; empty means anonymous pulls.
	PullSecret string
	// PlainHTTPRegistries are registry hosts charts are pulled from over
	// plain HTTP rather than HTTPS.
	PlainHTTPRegistries []string
	// Timeout bounds one apply, and is how old a pending revision left by
	// someone else must be before it is treated as abandoned.
	Timeout time.Duration
	Logger  *slog.Logger
}

// New returns the Helm deployer.
func New(cfg Config) (deploy.Deployer, error) {
	switch {
	case cfg.RESTConfig == nil:
		return nil, fmt.Errorf("the Helm deployer needs a REST config")
	case cfg.Kube == nil:
		return nil, fmt.Errorf("the Helm deployer needs a Kubernetes client")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	disc, mapper, err := newDiscovery(cfg.RESTConfig)
	if err != nil {
		return nil, fmt.Errorf("kubernetes discovery: %w", err)
	}
	d := &deployer{cfg: cfg, mapper: mapper, now: time.Now}
	d.configure = func(namespace string, restConfig *rest.Config) (*action.Configuration, error) {
		c := action.NewConfiguration(action.ConfigurationSetLogger(cfg.Logger.Handler()))
		if err := c.Init(newRESTGetter(restConfig, disc, mapper, namespace), namespace, "secret"); err != nil {
			return nil, err
		}
		return c, nil
	}
	d.load = d.fetch
	return d, nil
}

type deployer struct {
	cfg    Config
	mapper meta.RESTMapper
	now    func() time.Time
	// configure builds the Helm configuration for one apply, whose
	// Kubernetes requests go through restConfig.
	configure func(namespace string, restConfig *rest.Config) (*action.Configuration, error)
	load      func(ctx context.Context, artifact deploy.Artifact) (*chart.Chart, error)
}

func (d *deployer) Name() string { return Name }

func (d *deployer) Apply(ctx context.Context, a deploy.Assignment, progress deploy.Progress) (deploy.Applied, error) {
	if a.HelmReleaseName == "" {
		return deploy.Applied{}, deploy.Failf("the assignment names no Helm release")
	}
	// Every Kubernetes request of this apply, Helm's writes, hook waits,
	// and release storage, is cancelled when ctx ends, so nothing keeps
	// changing the cluster after the agent has given the apply up.
	bound := rest.CopyConfig(d.cfg.RESTConfig)
	bound.Wrap(func(rt http.RoundTripper) http.RoundTripper { return bind(ctx, rt) })
	cfg, err := d.configure(a.Namespace, bound)
	if err != nil {
		return deploy.Applied{}, fmt.Errorf("helm configuration: %w", err)
	}

	install, err := d.prepare(cfg.Releases, a, progress)
	if err != nil {
		return deploy.Applied{}, err
	}
	if install == nil {
		return d.alreadyApplied(ctx, cfg.Releases, a)
	}

	progress(deploy.StepFetching, "pulling chart "+a.Definition.Ref())
	c, err := d.load(ctx, a.Definition)
	if err != nil {
		return deploy.Applied{}, err
	}
	if crds := c.CRDObjects(); len(crds) > 0 {
		return deploy.Applied{}, deploy.Failf("the chart ships CRDs (%s), which are cluster-scoped; install them separately", crds[0].Name)
	}

	progress(deploy.StepResolving, "building values")
	docs, err := overlays(c, a.Overlays)
	if err != nil {
		return deploy.Applied{}, err
	}
	built, err := a.Values(ctx, docs)
	if err != nil {
		return deploy.Applied{}, &deploy.Failure{Reason: "could not build the values", Err: err}
	}

	message := fmt.Sprintf("applying Helm release %s in %s", a.HelmReleaseName, a.Namespace)
	if a.IsRollback() {
		message = fmt.Sprintf("rolling back Helm release %s in %s to what Deployment %s applied", a.HelmReleaseName, a.Namespace, a.Restores)
	}
	progress(deploy.StepApplying, message)
	renderer := &postRenderer{
		applicationID: a.ApplicationID, deploymentID: a.DeploymentID, releaseID: a.ReleaseID,
		namespace: a.Namespace, mapper: d.mapper,
	}
	labels := map[string]string{releaseLabel: a.DeploymentID}
	if *install {
		act := action.NewInstall(cfg)
		act.ReleaseName = a.HelmReleaseName
		act.Namespace = a.Namespace
		act.ServerSideApply = true
		act.WaitStrategy = kube.HookOnlyStrategy
		act.Timeout = d.cfg.Timeout
		act.Labels = labels
		act.PostRenderer = renderer
		act.SkipCRDs = true
		// Not ctx: Helm returns on cancellation while its install goroutine
		// carries on. The bound requests stop the operation instead, and
		// waiting for it means it has stopped when Apply returns.
		_, err = act.RunWithContext(context.WithoutCancel(ctx), c, built.Values)
	} else {
		act := action.NewUpgrade(cfg)
		act.Namespace = a.Namespace
		act.ServerSideApply = "true"
		act.WaitStrategy = kube.HookOnlyStrategy
		act.Timeout = d.cfg.Timeout
		act.MaxHistory = maxHistory
		act.Labels = labels
		act.PostRenderer = renderer
		act.SkipCRDs = true
		_, err = act.RunWithContext(context.WithoutCancel(ctx), a.HelmReleaseName, c, built.Values)
	}
	if err != nil {
		if ctx.Err() != nil {
			return deploy.Applied{}, ctx.Err()
		}
		return deploy.Applied{}, &deploy.Failure{Reason: "helm could not apply release " + a.HelmReleaseName, Err: err}
	}
	d.keepPins(ctx, cfg.Releases, a)
	return deploy.Applied{Ref: d.ref(cfg.Releases, a.HelmReleaseName), SecretsInValues: built.SecretsInValues}, nil
}

// alreadyApplied reports a Deployment an earlier attempt applied: its revision
// is the one that committed, so its pins are what the Deployment records. An
// earlier attempt may have died before keeping them, in which case the report
// carries none and the caller refuses rather than report this attempt's.
func (d *deployer) alreadyApplied(ctx context.Context, store *storage.Storage, a deploy.Assignment) (deploy.Applied, error) {
	out := deploy.Applied{Skipped: true, Ref: d.ref(store, a.HelmReleaseName)}
	last, err := latest(store, a.HelmReleaseName)
	if err != nil {
		return deploy.Applied{}, fmt.Errorf("read helm release %s: %w", a.HelmReleaseName, err)
	}
	pins, err := d.readPins(ctx, a.Namespace, a.HelmReleaseName, last.Version)
	if err != nil {
		return deploy.Applied{}, &deploy.Failure{
			Reason: fmt.Sprintf("could not read the versions of the secrets Deployment %s applied", a.DeploymentID),
			Err:    err,
		}
	}
	out.SecretPins = pins
	return out, nil
}

// keepPins keeps what this apply resolved on its revision, so a retry that
// finds the Deployment already applied reports the truth. Failing to keep them
// does not fail the apply: this attempt's own report is right either way, and
// a later attempt is refused rather than guessing.
func (d *deployer) keepPins(ctx context.Context, store *storage.Storage, a deploy.Assignment) {
	if len(a.SecretPins) == 0 {
		return
	}
	last, err := latest(store, a.HelmReleaseName)
	if err != nil {
		d.cfg.Logger.Warn("could not keep the deployment's secret versions", "deployment_id", a.DeploymentID, "error", err)
		return
	}
	if err := d.writePins(ctx, a.Namespace, a.HelmReleaseName, last, a.SecretPins); err != nil {
		d.cfg.Logger.Warn("could not keep the deployment's secret versions", "deployment_id", a.DeploymentID, "error", err)
	}
}

// revisionSecret names the Secret the Helm secret storage keeps a revision in:
// <helm storage type>.<release name>.v<version>.
func revisionSecret(release string, version int) string {
	return fmt.Sprintf("%s.%s.v%d", storage.HelmStorageType, release, version)
}

// readPins returns the pins kept on a revision, or nil when it kept none.
func (d *deployer) readPins(ctx context.Context, namespace, release string, version int) ([]deploy.SecretPin, error) {
	secret, err := d.cfg.Kube.CoreV1().Secrets(namespace).Get(ctx, revisionSecret(release, version), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read the release revision: %w", err)
	}
	raw := secret.Annotations[pinsAnnotation]
	if raw == "" {
		return nil, nil
	}
	var pins []deploy.SecretPin
	if err := json.Unmarshal([]byte(raw), &pins); err != nil {
		return nil, fmt.Errorf("read the release revision's pins: %w", err)
	}
	return pins, nil
}

// writePins adds the pins to a revision's annotations, without disturbing
// anything else on the object.
func (d *deployer) writePins(ctx context.Context, namespace, release string, rel *releasev1.Release, pins []deploy.SecretPin) error {
	raw, err := json.Marshal(pins)
	if err != nil {
		return err
	}
	if len(raw) > maxPinsSize {
		return fmt.Errorf("%d secrets are too many to keep with the release", len(pins))
	}
	patch, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": map[string]string{pinsAnnotation: string(raw)}},
	})
	if err != nil {
		return err
	}
	_, err = d.cfg.Kube.CoreV1().Secrets(namespace).Patch(ctx, revisionSecret(release, rel.Version), types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("keep the release revision's pins: %w", err)
	}
	return nil
}

// prepare reads the release's latest revision and decides what to do:
// nothing (nil, the Deployment is already applied), install (true), or
// upgrade (false). It clears a pending revision an earlier attempt left
// behind, and refuses one another operation still holds.
func (d *deployer) prepare(store *storage.Storage, a deploy.Assignment, progress deploy.Progress) (*bool, error) {
	last, err := latest(store, a.HelmReleaseName)
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return new(true), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read helm release %s: %w", a.HelmReleaseName, err)
	}
	ours := last.Labels[releaseLabel] == a.DeploymentID
	switch status := last.Info.Status; {
	case status == releasecommon.StatusDeployed && ours:
		return nil, nil
	case status.IsPending() && ours:
		progress(deploy.StepInterrupted, fmt.Sprintf("an earlier attempt was interrupted at revision %d; applying again", last.Version))
		if err := markFailed(store, last, "interrupted: the agent stopped mid-apply"); err != nil {
			return nil, stuck(a, last, err)
		}
	case status.IsPending():
		age := d.now().Sub(last.Info.LastDeployed)
		if age < d.cfg.Timeout {
			return nil, &deploy.Failure{
				Reason: fmt.Sprintf("helm release %s has another operation in progress (%s, revision %d, %s ago)",
					a.HelmReleaseName, status, last.Version, age.Round(time.Second)),
			}
		}
		progress(deploy.StepInterrupted, fmt.Sprintf("revision %d has been %s for %s; treating it as abandoned",
			last.Version, status, age.Round(time.Second)))
		if err := markFailed(store, last, "abandoned: pending longer than the agent's apply timeout"); err != nil {
			return nil, stuck(a, last, err)
		}
	}
	// A release whose only revision failed to install cannot be upgraded.
	history, err := store.History(a.HelmReleaseName)
	if err != nil {
		return nil, fmt.Errorf("read helm history %s: %w", a.HelmReleaseName, err)
	}
	for _, h := range history {
		r, ok := h.(*releasev1.Release)
		if ok && (r.Info.Status == releasecommon.StatusDeployed || r.Info.Status == releasecommon.StatusSuperseded) {
			return new(false), nil
		}
	}
	if _, err := store.Delete(a.HelmReleaseName, last.Version); err != nil {
		return nil, fmt.Errorf("remove the failed first install of %s: %w", a.HelmReleaseName, err)
	}
	return new(true), nil
}

// latest returns the release's newest revision.
func latest(store *storage.Storage, name string) (*releasev1.Release, error) {
	r, err := store.Last(name)
	if err != nil {
		return nil, err
	}
	rel, ok := r.(*releasev1.Release)
	if !ok {
		return nil, fmt.Errorf("helm release %s is stored in an unknown format", name)
	}
	return rel, nil
}

// markFailed marks a pending revision failed, as Helm does when an operation
// times out, so the next operation is not refused.
func markFailed(store *storage.Storage, rel *releasev1.Release, msg string) error {
	rel.SetStatus(releasecommon.StatusFailed, msg)
	return store.Update(rel)
}

// stuck is the failure when a pending revision cannot be cleared: the
// Deployment says how to clear it by hand. Helm reads a revision's status
// from the release encoded in its Secret, not from the Secret's labels, so
// the revision's Secret is deleted; the next apply then starts from the
// revision before it, or installs afresh if it was the first.
func stuck(a deploy.Assignment, rel *releasev1.Release, err error) *deploy.Failure {
	return &deploy.Failure{
		Reason: fmt.Sprintf("helm release %s is stuck in %s", a.HelmReleaseName, rel.Info.Status),
		Detail: map[string]any{"clear_with": revisionSecretCommand(a.Namespace, a.HelmReleaseName, rel.Version)},
		Err:    err,
	}
}

// revisionSecretCommand deletes the Secret Helm keeps one revision in.
func revisionSecretCommand(namespace, release string, version int) string {
	return fmt.Sprintf("kubectl -n %s delete secret sh.helm.release.v1.%s.v%d", namespace, release, version)
}

// ref names the release's current revision for the report.
func (d *deployer) ref(store *storage.Storage, name string) map[string]any {
	out := map[string]any{"type": Name, "name": name}
	if rel, err := latest(store, name); err == nil {
		out["revision"] = strconv.Itoa(rel.Version)
	}
	return out
}
