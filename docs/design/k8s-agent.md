# Kubernetes agent

- Status: Draft for review
- Date: 2026-11-08
- Related: [ADR 0001](../adr/0001-deployment-topology.md), [ADR 0003](../adr/0003-identity-access-and-policy.md), [ADR 0004](../adr/0004-credentials-and-keys.md), [ADR 0006](../adr/0006-release-and-delivery-tracking.md), [ADR 0008](../adr/0008-reaching-customer-systems.md), [ADR 0013](../adr/0013-one-agent-per-runtime.md), [delivery data model](delivery-data-model.md), [workload enrolment](workload-enrolment.md)

This document designs `ivaldi-k8s-agent` (`apps/k8s-agent`), the agent that runs inside a customer's Kubernetes cluster and serves one Target ([ADR 0013](../adr/0013-one-agent-per-runtime.md)). It has the two jobs [ADR 0006](../adr/0006-release-and-delivery-tracking.md) gives a runtime agent: **observe** what runs on its Target and report it, and, when allowed, **execute** Deployments the control plane assigns to it. It never touches the control-plane database and only ever connects out ([ADR 0008](../adr/0008-reaching-customer-systems.md)).

The control plane already serves most of what the agent needs: enrolment and renewal, assignment polling, Deployment reports, and heartbeats. The gaps are listed under [Control-plane changes](#control-plane-changes).

## Decisions this design is built on

- **One agent serves one Target, which is one namespace.** A Target's locator is a cluster and a namespace, and enrolment gives each Target its own workload principal. So one install of the agent is one Helm release in the Target's namespace, with its own ServiceAccount and a namespace-scoped Role. Nothing the agent does needs cluster-wide access. This answers ADR 0006's open question "one agent per cluster or per namespace" with "per namespace" for now. A cluster-wide agent serving many Targets is a later option (see [Open questions](#open-questions)).
- **The agent polls; the control plane never calls in.** It sends a heartbeat, polls for assignments, and reports, all over HTTPS to the control plane's `/v1` API. Polling is simple to run behind any egress proxy, and assignment latency of a few seconds is acceptable for deployments. A streaming connection can replace the poll later without changing the contracts.
- **Kubernetes identity first.** The default credential method is `kubernetes`: the agent presents a projected ServiceAccount token with audience `ivaldi`, which the kubelet rotates. There is no long-lived Ivaldi secret in the cluster. `bearer` and `mtls` stay available for control planes that cannot reach the cluster's OIDC issuer.
- **Observation is always on; execution is opt-in twice.** The control plane executes only on Environments with `execution_enabled`. The agent also has its own `execute` switch, off by default, which decides whether the chart grants it write access at all. A cluster operator can keep Ivaldi observe-only whatever the control plane says.
- **Helm is the first way to execute.** The assignment already carries `helm_release_name`, and a Release's deployment definition is its `helm_chart` artifact. Plain manifests and image-only Releases come later, as other ways the same agent applies a Release (ADR 0006: deploy tools are not separate agents).
- **Digests, never tags.** The agent reports the digest each container is actually running, read from the Pod's status, and fetches charts by digest. A tag is never what it reports or applies.
- **The agent decides nothing about policy.** It applies what it is assigned and reports what it sees. Lifecycle checks, approvals, findings, and Release matching stay in the control plane ([ADR 0003](../adr/0003-identity-access-and-policy.md)).

## How it runs

The agent is a single-replica Deployment in the Target's namespace, installed by `deploy/helm/ivaldi-agent`:

- **One active replica.** The Deployment uses the `Recreate` strategy, and the agent also takes a `coordination.k8s.io` Lease before it starts working, so two replicas never apply the same assignment during an upgrade or a node failure. The Lease is in its own namespace.
- **No Service, HPA, or HTTPRoute.** Nothing connects to the agent. The scaffold's Service, HPA, HTTPRoute, and test-connection hook are removed. Liveness and readiness are served on a local port (`:8080`, `/healthz` and `/readyz`) for the kubelet only.
- **Its namespace comes from the downward API** (`POD_NAMESPACE`), not from a flag, so it cannot be pointed at a namespace its Role does not cover.
- **It runs non-root with a read-only root filesystem.** It keeps nothing on disk; its state is in a Secret (below).

### Lifecycle of the process

1. Read configuration; load state from the state Secret.
2. If there is no state, enrol with the token from the enrolment Secret (below). Otherwise check the credential is still valid, and renew it if it is past half its life.
3. Take the Lease.
4. Start the informers ([Observing](#observing)) and wait for their caches to sync.
5. Fetch the Target's watch list, report the current state once, then run four loops until the Lease is lost or the process is stopped:
   - heartbeat every `heartbeat_interval` (30s, matching `runners.heartbeat_interval`);
   - poll assignments every `poll_interval` (5s);
   - refresh the watch list every `watch_list_interval` (60s);
   - renew the credential at 50% of its life, ±5% jitter, as [workload enrolment](workload-enrolment.md) asks.
6. On losing the Lease, stop applying, finish the current report, and exit, so Kubernetes restarts it as a follower.

## Enrolment and credentials

An operator creates an enrolment token for the Target (`POST /v1/targets/{target_id}/enrolments`, method `kubernetes`) and installs the chart with it:

```sh
helm install ivaldi-agent oci://ghcr.io/ivaldibuild/charts/ivaldi-agent \
  --namespace payments-prod \
  --set controlPlane.url=https://ivaldi.example.com \
  --set enrolment.token=iwe_…
```

The chart puts the token in a Secret the agent reads once.

- **Redeem.** The agent calls `POST /v1/agent/enrolments/redeem` with the token, the method, and, for `kubernetes`, its projected token. The response gives its workload principal, credential, expiry, and, for `bearer`, its workload token.
- **State Secret.** The enrolment token is single-use and Pods are replaced, so the agent saves what it learned in a Secret, `<release>-state`: Target ID, Organization ID, workload principal ID, credential ID and kind, expiry, and the `bearer` token if there is one. The Role grants `get` and `update` on that one Secret by `resourceNames`, and `get` on the enrolment Secret. Nothing else in the namespace's Secrets is readable unless execution is on.
- **Projected token.** The chart mounts a projected ServiceAccount token at `/var/run/secrets/ivaldi/token` with audience `ivaldi` (configurable to match `runners.kubernetes_audience`) and a one-hour expiry. The agent re-reads the file on every request, since the kubelet rotates it, and sends it with `X-Ivaldi-Credential: kubernetes`.
- **Renewal** calls `POST /v1/agent/credentials/renew`. For `kubernetes` it extends the credential record; for `bearer` it gets a new token and writes it to the state Secret before using it, so a crash between the two never loses the only valid token.
- **When the credential is gone.** If a call returns 401 because the credential expired or was revoked, or 403 because the Target now has another agent, the agent stops working, marks itself not ready, and logs that it needs a new enrolment token. Re-installing with a new token, or updating the enrolment Secret, re-enrols it. It never retries a redeemed token.

The current flags (`--authentication-method`, `--credentials-store-path`, `--client-*-file`, `--kubernetes-token-file`) stay, with the state store becoming a Secret name instead of a path.

## Observing

### What it watches

The agent asks the control plane which Applications it should look for on its Target (new endpoint, below). The answer lists each active Application in the Target's Workspace with its ID, workload selector (default `ivaldi.build/application=<application ID>`), its artifacts' `artifact_name`s, and, where there is a placement, its current Deployment ID. Applications in other Workspaces are never matched, because a Target belongs to one Environment in one Workspace.

It runs shared informers, limited to its namespace, on Deployments, StatefulSets, DaemonSets, ReplicaSets, and Pods. Argo Rollouts, Argo CD Applications, and Flux objects come later (see [Open questions](#open-questions)).

### What a workload's Release is

For each Application, the agent finds the workloads whose labels match its selector and reads the images their **ready Pods of the current revision** are running:

- The digest comes from each container's `status.containerStatuses[].imageID`, normalised to `<repository>@sha256:<digest>`, unless the Pod spec already names the image by digest.
- The image's repository is matched to the Application's artifacts by `artifact_name`, as the data model says. Unmatched images are reported too, so the control plane can create observed artifacts.
- Init containers count, because a `migrate` image is often one. Sidecars injected by a mesh or an admission controller are reported, and the control plane drops any whose repository matches no artifact and is on an ignore list (see [Open questions](#open-questions)).

The result for an Application is a set of `(artifact_name, digest)` pairs. The agent fingerprints it and reports an **observation** only when the fingerprint changes, or once after start-up. The control plane treats an observation that matches the placement's current Release as a no-op, so a restart creates no new Deployment.

An observation carries the Application ID, the images and digests, the workloads it came from (kind, name, revision), and an `external_ref` naming the deployer when the agent can tell, such as the Helm release and revision from the `meta.helm.sh/release-name` annotation, or the Argo CD instance label. The control plane runs the observed-Deployment flow from the [delivery data model](delivery-data-model.md) and returns the Deployment it created or matched, which the agent then reports health for.

### Health

The agent tracks the health of each placement's current Deployment, executed or observed, using [kstatus](https://github.com/kubernetes-sigs/cli-utils/tree/master/pkg/kstatus) on the Application's workloads. Kstatus reads each workload's conditions, observed generation, and replica counts, so the agent does not reimplement rollout logic per kind.

| kstatus result for the Application's workloads | Reported Deployment status |
|---|---|
| Any `InProgress` during a rollout | `progressing` |
| All `Current` | `healthy` |
| Any `Failed` (for example `ProgressDeadlineExceeded`) during a rollout | `failed` |
| Any `InProgress` or `Failed` after reaching `healthy` | `degraded` |
| All `Current` again | `healthy` |

A report is sent only when the status changes, as `kind: status_changed`, with the workloads' conditions in `detail`. A `degraded` report is debounced (default 60s), so a Pod rescheduling does not flap the Deployment. The agent checks each move against `release.CanMoveDeployment` before reporting, and never sends one the control plane would refuse.

## Executing

### Picking up an assignment

`GET /v1/targets/{target_id}/assignments` returns every Deployment on the Target that is `applying` or `progressing`, each with its Release, Builds by digest, `apply_config`, and `helm_release_name`. The agent works through new ones concurrently, at most one per Application (the control plane already allows only one in flight per Application and Target) and at most `max_concurrent_applies` (default 2) in total.

While it is applying a Deployment for an Application, the agent reports changes it sees for that Application against the Deployment, not as an observation. This is step 5 of the observed flow in the data model.

### Applying with Helm

1. **Fetch the chart.** The artifact named by `apply_config.definition_artifact` must be a `helm_chart`. The agent pulls it as `oci://<published_as>@<digest>` and checks the digest. Registry credentials come from a docker-config Secret named in the chart's values (`registries.pullSecret`), because a data-plane Registry's credentials stay in the customer's environment ([ADR 0004](../adr/0004-credentials-and-keys.md), [ADR 0006](../adr/0006-release-and-delivery-tracking.md)).
2. **Build the values.** In order, later wins:
   1. the chart's defaults;
   2. each of `apply_config.values_refs`, resolved in the namespace (see below);
   3. the Release's images, under `global.ivaldi.artifacts.<artifact name>`, as `repository`, `digest`, and `image` (`<repository>@<digest>`), so charts can pin images by digest without knowing about Ivaldi's IDs;
   4. `global.ivaldi.application`, `.release`, `.deployment`, and `.version`.
3. **Install or upgrade** the Helm release named `helm_release_name` with the Helm Go SDK, server-side apply, and no wait: the agent tracks the rollout itself. The Helm release is labelled `ivaldi.build/deployment=<deployment ID>`.
4. **Label what it applies.** A post-renderer adds `ivaldi.build/application=<application ID>` to every workload's metadata and Pod template, and `ivaldi.build/deployment` and `ivaldi.build/release` annotations to every object. That keeps the default workload selector true for any chart, and lets anyone with `kubectl` see where an object came from.
5. **Report.** `progress` events for fetching and applying, then `status_changed` to `progressing`, then the [health](#health) mapping until the rollout reaches `healthy`, `degraded`, or `failed`.

A failed fetch or upgrade reports `failed` with the error in `detail`. The agent does not roll back on its own: a rollback is a new Deployment of an earlier Release, requested through the control plane like any other.

### Values references

`values_refs` are references, never values. The agent resolves two forms in its own namespace:

- `secret:<name>/<key>`: a key of a Secret, holding a YAML values document;
- `configmap:<name>/<key>`: the same for a ConfigMap.

Customers keep Target-specific values, including secrets, in their own cluster, and Ivaldi only records which ones a placement uses. Resolving references through the credentials capability can be added later as a third form.

### Restarting mid-apply

Every report has a deterministic `report_id`, `<deployment ID>:<step>` (for example `dpl_…:status:progressing`), so a retry after a crash is stored once. On picking up an assignment, the agent reads the Helm release's history. If the latest revision is already labelled with this Deployment's ID, it skips straight to tracking the rollout; otherwise it applies. Applying twice would be harmless anyway, since the upgrade is the same chart with the same values.

## Access in the cluster

The chart creates one ServiceAccount and these bindings in the Target's namespace:

| Role | Rules | When |
|---|---|---|
| `<release>-observe` | `get`, `list`, `watch` on Deployments, StatefulSets, DaemonSets, ReplicaSets, Pods, Events; `get`/`update` on its state Secret and `get` on its enrolment Secret by name; `get`/`create`/`update` on its Lease | Always |
| RoleBinding to the built-in `edit` ClusterRole | Create and change most namespaced objects, including Secrets, but not Roles or RoleBindings | `execute: true` |
| RoleBinding to `admin` instead | Also Roles and RoleBindings, for charts that create them | `execute: true` and `execution.allowRBAC: true` |

`edit` cannot grant itself more access, which is why it is the default. Both are bound with a RoleBinding, never a ClusterRoleBinding, so execution reaches only the Target's namespace. Charts that create cluster-scoped objects, such as CRDs, cannot be executed by the agent; they are installed by the cluster's operators.

## Configuration

Flags, with environment variables of the same name prefixed `IVALDI_`, and chart values that set them:

| Flag | Default | Meaning |
|---|---|---|
| `--server-url` | none, required | The control plane's URL |
| `--authentication-method` | `kubernetes` | `kubernetes`, `bearer`, or `mtls` |
| `--enrolment-secret` | `<release>-enrolment` | Secret holding the one-time enrolment token |
| `--state-secret` | `<release>-state` | Secret the agent keeps its state in |
| `--kubernetes-token-file` | `/var/run/secrets/ivaldi/token` | The projected token |
| `--client-cert-file`, `--client-key-file`, `--client-ca-file` | none | For `mtls`, and a private CA for the control plane |
| `--execute` | `false` | Whether to apply assignments |
| `--registry-pull-secret` | none | Docker-config Secret for fetching charts |
| `--heartbeat-interval` | `30s` | |
| `--poll-interval` | `5s` | |
| `--max-concurrent-applies` | `2` | |
| `--health-address` | `:8080` | Liveness and readiness |

`--credentials-store-path` is replaced by `--state-secret`. Kubernetes access uses the in-cluster config; there is no kubeconfig flag outside development.

## Code layout

```
apps/k8s-agent/
  cmd/main.go              calls agent.New().Execute()
  internal/agent/          Cobra command, flags, the run loop, leader election
  internal/apiclient/      oapi-codegen client for the agent's operations only, as ivaldictl does
  internal/auth/           the credential transport: projected token, bearer, or mTLS; renewal
  internal/state/          the state Secret
  internal/observe/        informers, selector matching, image digests, observations
  internal/health/         kstatus to Deployment status, debouncing
  internal/execute/        assignments, chart fetching, values, Helm, post-renderer
```

Only the generated client and the status rules from `pkg/release` cross into the agent; it imports nothing from `apps/control-plane`. Its new dependencies are `client-go`, the Helm SDK, and `cli-utils` for kstatus. They stay in this module, which is the point of [ADR 0013](../adr/0013-one-agent-per-runtime.md). The client and the credential transport move to `pkg/` when a second agent needs them.

Tests use client-go's fake clientset for observation and health, a fake Helm action configuration for execution, and `httptest` for the control plane. An end-to-end test against `kind` runs in CI later.

## Control-plane changes

What the agent needs that the control plane does not have yet:

1. **The redeem response names the Target.** `Redeemed` gains `target_id` and `organization_id`, so the agent learns which Target it serves from the enrolment itself instead of from configuration that could disagree with it.
2. **A watch list.** `GET /v1/targets/{target_id}/watch-list`, for the Target's agent only: the active Applications of the Target's Workspace with their workload selectors, artifact names, and the current Deployment ID of each placement on this Target.
3. **Observations.** `POST /v1/targets/{target_id}/observations`, for the Target's agent only, with a `report_id`: an Application ID, the images and digests it runs, the workloads, and an optional `external_ref`. It runs the observed-Deployment flow and returns the Deployment it created or matched, or none when nothing changed.
4. **A heartbeat body.** `POST /v1/targets/{target_id}/reports` accepts the agent's version, the Kubernetes version, and whether execution is on, so the web app can show an agent that cannot execute before someone requests a Deployment, and the control plane can refuse to queue one.
5. **Values references are checked against the forms above** when a placement's `apply_config` is saved.

None of these change existing operations, so they are additive in the OpenAPI description that the agent's client is generated from. The separate runner listener (`runners.port`, 8661) is configured but not served; the agent uses the API listener until it is.

## Invariants

- **The agent only connects out**, to the control plane and to registries it fetches charts from.
- **The agent acts only in its own namespace**, enforced by RoleBindings rather than by its code.
- **The agent never applies without an assignment**, and never applies when `--execute` is off, whatever the control plane sends.
- **Every report is idempotent** through its `report_id`.
- **Everything the agent reports is a digest it saw running**, never a tag or a value from configuration.
- **No credential leaves the cluster**: registry credentials and values stay in the namespace, and the agent's own credential is the projected token or a token stored only in its state Secret.

## Open questions

- **Multi-arch digests.** CI usually reports an image index digest. Whether `imageID` shows the index or the platform manifest digest depends on the container runtime and how the image was pulled. Containerd and CRI-O need checking. If they report the platform digest, Builds need to record their platform digests too so observations still match.
- **Which deployers to read first.** Argo CD Applications and Flux HelmReleases give a better `external_ref` and status than inferring from workloads. Which comes first depends on customers.
- **Argo Rollouts and other workload kinds.** Kstatus handles built-in kinds and anything with standard conditions; Rollouts need a check.
- **Sidecars.** Whether the control plane keeps an ignore list for injected images (Istio, Linkerd, Vault agent), or the agent reports only containers named in the Pod template.
- **A cluster-wide agent.** One install serving several Targets in a cluster would need one credential per Target and a ClusterRole for observation. Worth it only if per-namespace installs become a burden.
- **Plain manifests and image-only Releases.** Applying an `archive` artifact of manifests with server-side apply is a small addition. Patching images on existing workloads for Releases without a chart is simpler for adopters but leaves the deployment definition outside Ivaldi.
- **Streaming instead of polling**, once the runner listener is served.
