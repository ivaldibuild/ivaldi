# Kubernetes agent

- Status: Draft for review
- Date: 2026-11-08
- Related: [ADR 0001](../adr/0001-deployment-topology.md), [ADR 0003](../adr/0003-identity-access-and-policy.md), [ADR 0004](../adr/0004-credentials-and-keys.md), [ADR 0006](../adr/0006-release-and-delivery-tracking.md), [ADR 0008](../adr/0008-reaching-customer-systems.md), [ADR 0013](../adr/0013-one-agent-per-runtime.md), [delivery data model](delivery-data-model.md), [workload enrolment](workload-enrolment.md)

This document designs `ivaldi-k8s-agent` (`apps/k8s-agent`), the agent that runs inside a customer's Kubernetes cluster ([ADR 0013](../adr/0013-one-agent-per-runtime.md)). One agent serves a whole cluster, and through it every Target in that cluster. It has the two jobs [ADR 0006](../adr/0006-release-and-delivery-tracking.md) gives a runtime agent: **observe** what runs on its Targets and report it, and, when allowed, **execute** Deployments the control plane assigns to it. When it executes, it resolves the Application's secrets from the customer's secret manager and hands them to the chart, so secret values never reach Ivaldi ([ADR 0004](../adr/0004-credentials-and-keys.md)). It never touches the control-plane database and only ever connects out ([ADR 0008](../adr/0008-reaching-customer-systems.md)).

The control plane already serves part of what the agent needs: enrolment and renewal, Deployment reports, and per-Target assignment polling and heartbeats. What it lacks is listed under [Control-plane changes](#control-plane-changes).

## Decisions this design is built on

- **One agent per cluster, serving many Targets.** A Target is still a namespace in a cluster, and still what Environments, authorization, and Deployments refer to. What changes is who serves it: a new delivery object, the **Cluster**, records the agent for a cluster, and every Target in that Cluster is served by it. One install per cluster is easier to run than one per namespace, and Targets can be added without installing anything.
- **The cluster's operators decide what the agent may touch.** Ivaldi users with `environment.manage` can create a Target for any namespace of a Cluster, but the agent can observe only the namespaces its chart lets it read, and execute only in the namespaces its chart binds it to. Both are Kubernetes RBAC set at install time, not settings in Ivaldi.
- **The agent polls; the control plane never calls in.** It sends a heartbeat, polls for assignments across all its Targets, and reports, all over HTTPS to the control plane's `/v1` API. A streaming connection can replace polling later without changing the contracts.
- **Kubernetes identity first.** The default credential method is `kubernetes`: the agent presents a projected ServiceAccount token with audience `ivaldi`, which the kubelet rotates. `bearer` and `mtls` stay available for control planes that cannot reach the cluster's OIDC issuer.
- **Execution is opt-in twice.** The control plane executes only on Environments with `execution_enabled`, and the agent executes only in namespaces its chart grants write access to.
- **Helm first.** A Release's deployment definition is its `helm_chart` artifact, and the assignment carries `helm_release_name`. Plain manifests and image-only Releases come later, as other ways the same agent applies a Release.
- **Variables are resolved in two halves.** An Application's [deployment variables](deployment-variables.md) hold plain values and references to secrets in the customer's secret manager. The control plane picks the winning value of each for the Target; the agent fetches secrets with its own cloud identity, fills in bindings, and renders the chart. It records which secret versions it used, never their values.
- **Digests, never tags.** The agent reports the digest each container is actually running, read from the Pod's status, and fetches charts by digest.
- **The agent decides nothing about policy.** It applies what it is assigned and reports what it sees. Lifecycle checks, approvals, findings, and Release matching stay in the control plane ([ADR 0003](../adr/0003-identity-access-and-policy.md)).

## Clusters

A **Cluster** is a Kubernetes cluster an Organization deploys to. It belongs to the Organization rather than a Workspace, because one cluster usually hosts several Workspaces' Environments.

- **clusters** (delivery, prefix `cls`): `slug` unique in the Organization, `display_name`, `runtime` (`kubernetes`), `agent_principal_id`, `agent_capabilities` (from the last heartbeat, below), `last_reported_at`, and soft deletion.
- **Targets name their Cluster.** A Kubernetes Target's `locator.cluster` must be the slug of an active Cluster in the Organization. The locator keeps its shape, so nothing else about Targets changes.
- **A Target's agent is its Cluster's agent.** `targets.agent_principal_id` is dropped. Every check that today compares the caller with the Target's agent compares it with the Target's Cluster's agent instead. Assigning an agent moves from `PUT /v1/targets/{id}/agent` to the Cluster.
- **Permissions.** `cluster.read` and `cluster.manage` on the Organization. Creating a Cluster and creating its enrolment tokens need `cluster.manage`; creating a Target in it still needs `environment.manage` on the Target's Environment. The built-in roles pick these up by rule, so IAM does not change.
- **Archiving** a Cluster is refused while it has active Targets.

A Cluster is Kubernetes-specific. A future VPS agent ([ADR 0013](../adr/0013-one-agent-per-runtime.md)) may need a similar "where an agent runs" object, but that is for its own design.

## How it runs

The agent is a single-replica Deployment in its own namespace (`ivaldi-system` by default), installed by `deploy/helm/ivaldi-agent`:

- **One active replica.** The Deployment uses the `Recreate` strategy, and the agent also takes a `coordination.k8s.io` Lease in its own namespace before it starts working, so two replicas never apply the same assignment.
- **No Service, HPA, or HTTPRoute.** Nothing connects to the agent. The scaffold's Service, HPA, HTTPRoute, and test-connection hook are removed. Liveness and readiness are served on a local port (`:8080`) for the kubelet only.
- **It runs non-root with a read-only root filesystem**, and keeps its state in a Secret in its own namespace.

### Lifecycle of the process

1. Read configuration; load state from the state Secret.
2. If there is no state, enrol with the token from the enrolment Secret. Otherwise check the credential, and renew it if it is past half its life.
3. Take the Lease.
4. Fetch its Targets and their watch lists ([Observing](#observing)), start informers for the namespaces it can read, and wait for their caches to sync.
5. Report the current state of every Target once, then run these loops until the Lease is lost or the process stops:
   - heartbeat every `heartbeat_interval` (30s, matching `runners.heartbeat_interval`), carrying what it can do in each namespace;
   - poll assignments every `poll_interval` (5s);
   - refresh its Targets and watch lists every `watch_list_interval` (60s), starting and stopping informers as Targets come and go;
   - renew the credential at 50% of its life, ±5% jitter ([workload enrolment](workload-enrolment.md)).
6. On losing the Lease, stop applying, finish the current report, and exit.

## Enrolment and credentials

An operator creates the Cluster, then an enrolment token for it (`POST /v1/clusters/{cluster_id}/enrolments`, method `kubernetes`), and installs the chart with it:

```sh
helm install ivaldi-agent oci://ghcr.io/ivaldibuild/charts/ivaldi-agent \
  --namespace ivaldi-system --create-namespace \
  --set controlPlane.url=https://ivaldi.example.com \
  --set enrolment.token=iwe_… \
  --set execute.namespaces='{payments-staging,payments-prod}'
```

- **Redeem.** The agent calls `POST /v1/agent/enrolments/redeem` with the token, the method, and its projected token. The control plane creates the `runtime_agent` workload principal, sets it as the Cluster's agent, and suspends the Cluster's previous agent, as Target enrolment does today. The response names the Cluster and the Organization.
- **State Secret.** The enrolment token is single-use and Pods are replaced, so the agent keeps what it learned in a Secret, `<release>-state`: Cluster ID, Organization ID, workload principal ID, credential ID, kind, and expiry, and the `bearer` token if there is one. Its Role allows `get`/`update` on that Secret and `get` on the enrolment Secret, by name.
- **Projected token.** The chart mounts a projected ServiceAccount token at `/var/run/secrets/ivaldi/token` with audience `ivaldi` (to match `runners.kubernetes_audience`) and a one-hour expiry. The agent re-reads it on every request and sends it with `X-Ivaldi-Credential: kubernetes`.
- **Renewal** calls `POST /v1/agent/credentials/renew`. For `bearer`, the new token is written to the state Secret before it is used.
- **When the credential is gone** (401, or 403 because the Cluster has another agent), the agent stops working, marks itself not ready, and logs that it needs a new enrolment token. It never retries a redeemed token.

## Observing

### What it watches

The agent asks the control plane for its Targets (new endpoint, below). For each Target, the answer gives the namespace and the active Applications of the Target's Workspace with their workload selectors (default `ivaldi.build/application=<application ID>`), artifact names, and the current Deployment of each placement on that Target.

It watches Deployments, StatefulSets, DaemonSets, ReplicaSets, and Pods in its Targets' namespaces, and nowhere else:

- When the chart grants cluster-wide read (`observe.namespaces` left empty), it uses one cluster-wide informer per kind, filtered to its Targets' namespaces.
- When the chart lists namespaces, it has read access only there, so it runs one informer set per namespace, for those that have a Target.

A Target whose namespace the agent cannot read is reported as not observable in the heartbeat, and the web app shows it.

### What a workload's Release is

For each Application on each Target, the agent finds the workloads matching its selector and reads the images their ready Pods of the current revision are running:

- The digest comes from `status.containerStatuses[].imageID`, normalised to `<repository>@sha256:<digest>`, unless the Pod spec already names the image by digest.
- Images are matched to the Application's artifacts by `artifact_name`. Unmatched images are reported too, so the control plane can create observed artifacts.
- Init containers count, because a `migrate` image is often one.

The result is a set of `(artifact_name, digest)` pairs per Application and Target. The agent reports an **observation** only when that set changes, or once after start-up; the control plane ignores one that matches the placement's current Release. An observation also carries the workloads it came from and an `external_ref` naming the deployer when the agent can tell, such as the Helm release and revision. The control plane runs the observed-Deployment flow from the [delivery data model](delivery-data-model.md) and returns the Deployment it created or matched.

### Health

The agent tracks the health of each placement's current Deployment, executed or observed, using [kstatus](https://github.com/kubernetes-sigs/cli-utils/tree/master/pkg/kstatus) on the Application's workloads:

| kstatus result for the Application's workloads | Reported Deployment status |
|---|---|
| Any `InProgress` during a rollout | `progressing` |
| All `Current` | `healthy` |
| Any `Failed` (for example `ProgressDeadlineExceeded`) during a rollout | `failed` |
| Any `InProgress` or `Failed` after reaching `healthy` | `degraded` |
| All `Current` again | `healthy` |

A report is sent only when the status changes, with the workloads' conditions in `detail`. `degraded` is debounced (60s by default). The agent checks each move with `release.CanMoveDeployment` before reporting it.

## Executing

### Picking up an assignment

The agent polls one endpoint for every Deployment on any of its Targets that is `applying` or `progressing`, each with its Release, Builds by digest, `apply_config`, `helm_release_name`, the snapshot revision of its variables, and the winning value of each variable for that Target ([deployment variables](deployment-variables.md#resolving-a-deployments-variables)). It works at most one per Application and Target, and at most `max_concurrent_applies` (default 4) in total. An assignment for a namespace the agent cannot write to is reported `failed` straight away with that reason; the control plane should refuse it before that (see [Control-plane changes](#control-plane-changes)).

While it applies a Deployment, changes it sees for that Application on that Target are reported against the Deployment, not as an observation.

### Applying with Helm

1. **Fetch the chart.** The artifact named by `apply_config.definition_artifact` must be a `helm_chart`. The agent pulls `oci://<published_as>@<digest>` and checks the digest. Registry credentials come from a docker-config Secret named in the chart's values (`registries.pullSecret`).
2. **Resolve variables**, as described in [Variables and secrets](#variables-and-secrets). A secret that cannot be fetched, an undefined binding, or a cycle fails the Deployment before anything is applied, naming the variable but not any value.
3. **Build the values.** In order, later wins:
   1. the chart's defaults;
   2. each of `apply_config.values_refs` (whole values documents in the namespace, such as a platform team's ConfigMap);
   3. each variable with a `values_path`, at that path: `text` variables, and `secret` variables delivered as values;
   4. the Release's images, under `global.ivaldi.artifacts.<artifact name>` as `repository`, `digest`, and `image` (`<repository>@<digest>`);
   5. `global.ivaldi.application`, `.release`, `.deployment`, `.version`, and `global.ivaldi.secrets.name` when secrets are delivered as a Kubernetes Secret.
4. **Install or upgrade** the Helm release `helm_release_name` in the Target's namespace with the Helm Go SDK, server-side apply, and no wait: the agent tracks the rollout itself. The Helm release is labelled `ivaldi.build/deployment=<deployment ID>`.
5. **Label what it applies.** A post-renderer adds `ivaldi.build/application=<application ID>` to every workload's metadata and Pod template, and `ivaldi.build/deployment` and `ivaldi.build/release` annotations to every object, so the default workload selector holds for any chart.
6. **Report** `progress` events for fetching, resolving, and applying, then `progressing`, then the [health](#health) mapping until the rollout is `healthy`, `degraded`, or `failed`.

The agent does not roll back on its own: a rollback is a new Deployment of an earlier Release.

### Restarting mid-apply

Every report has a deterministic `report_id`, `<deployment ID>:<step>`, so a retry is stored once. On picking up an assignment, the agent reads the Helm release's history; if the latest revision already carries this Deployment's ID, it skips to tracking the rollout.

## Variables and secrets

What an Application's variables are, how scopes pick a value, and how a Release snapshots them is in [deployment variables](deployment-variables.md). This section is the agent's half: fetching secrets, filling in bindings, and giving the result to the chart.

### Secret Stores

A **Secret Store** is a secret manager the agent can reach. It belongs to the credentials capability ([ADR 0004](../adr/0004-credentials-and-keys.md)) and to a Workspace, like a Registry, and holds no credentials:

- `kind`: `aws_secrets_manager`, `vault`, or `kubernetes` first; `gcp_secret_manager` and `azure_key_vault` later.
- Non-secret settings for the kind: the AWS region; the Vault address, mount, and auth role; nothing for `kubernetes`, which reads Secrets in the Target's namespace.
- `environment_ids`: optional. When set, only Deployments to those Environments may use the store. This is what keeps a staging Deployment from naming a production store.

Behind it sits the Ivaldi-owned secret-store interface ADR 0004 asks for, in `pkg/` because the agent implements its providers and the control plane validates against the same kinds.

### How the agent resolves them

- **Bindings first, then secrets.** The agent fills in `#{…}` bindings in secret paths, fetches the secrets, then fills in bindings in `text` values, which may bind those secrets. Any value that binds a secret is treated as a secret from then on. An undefined name or a cycle fails the Deployment.
- **With its own identity.** The agent authenticates to the store as its Kubernetes ServiceAccount: EKS Pod Identity or IRSA for AWS, Vault's Kubernetes auth with the store's role, and its own RBAC for `kubernetes`. Ivaldi holds no secret-manager credentials anywhere.
- **At render time, in memory.** Values are fetched for one apply, used, and dropped. They are never logged, never put in a report's `detail`, and never sent to the control plane.
- **As a Kubernetes Secret by default.** For `deliver_as: secret`, the agent writes an immutable Secret, `<helm release>-secrets-<hash>`, in the Target's namespace, with one key per secret variable (the variable's name, which is why names are limited to what a Secret key allows), and passes its name to the chart as `global.ivaldi.secrets.name`. The hash covers the resolved versions, so a rotated secret gets a new name, which changes the Pod template and rolls the workloads. The agent keeps the Secrets of the last three Deployments, for rollbacks, and deletes older ones.
- **As values only when asked.** `deliver_as: value` puts the secret into the chart's values at `values_path`, for charts that cannot read a Secret. Helm stores values in its release records, so the secret then sits in every revision's history in the namespace and shows in `helm get values`. The web app says so where the option is chosen.
- **Versions are recorded.** The agent reports, for each reference, the store's version ID it resolved (an AWS version ID, a Vault KV version, a Kubernetes `resourceVersion`) in a `progress` event. With the snapshot revision the Deployment records, its history says exactly what configuration it shipped.
- **Masked wherever it reports.** Before sending any `detail` or log line, the agent replaces every resolved secret value longer than three characters with `***`, including inside Helm's own error messages, which can echo values. As Octopus warns, literal masking is not foolproof, so the agent also never puts rendered values or manifests in a report.

### Who can read what

One cluster-wide agent with one cloud identity can read every secret that identity is granted, for every Target in the cluster. Two controls keep that from becoming a path from staging to production:

- **In Ivaldi:** a store's `environment_ids`, checked when a value is saved and again when a Deployment is requested.
- **In the customer's cloud:** the IAM policy or Vault role the agent's identity holds, which the customer scopes as they like.

A stronger option is for the agent to resolve each Target's secrets as a ServiceAccount in that Target's namespace, using the TokenRequest API, so a cloud policy can be written per namespace. It needs `create` on `serviceaccounts/token` for one ServiceAccount in each execute namespace, and one more identity per namespace for the customer to set up. It is left as an open question rather than built in.

## Access in the cluster

The chart creates one ServiceAccount in the agent's namespace, and:

| Binding | Grants | Where |
|---|---|---|
| Role in its own namespace | `get`/`update` on its state Secret, `get` on its enrolment Secret, `get`/`create`/`update` on its Lease | Always |
| ClusterRole `ivaldi-agent-observe` | `get`, `list`, `watch` on Deployments, StatefulSets, DaemonSets, ReplicaSets, Pods, Events. Not Secrets. | Bound cluster-wide with a ClusterRoleBinding when `observe.namespaces` is empty; otherwise with a RoleBinding in each listed namespace |
| RoleBinding to the built-in `edit` ClusterRole | Create and change most namespaced objects, including Secrets, but not Roles or RoleBindings | Each namespace in `execute.namespaces` |
| RoleBinding to `admin` instead | Also Roles and RoleBindings, for charts that create them | Each namespace in `execute.namespaces` when `execute.allowRBAC` is true |
| RoleBinding to a `secrets-read` Role | `get` on Secrets | Each namespace with a `kubernetes` Secret Store in use, listed in `secrets.kubernetesNamespaces` |

Execution never gets a ClusterRoleBinding, so the agent can change only namespaces the cluster's operators listed. Adding an execute namespace is a `helm upgrade` of the agent, which is deliberate: it is the cluster operator's consent. Charts that create cluster-scoped objects, such as CRDs, cannot be executed by the agent.

The agent reports in each heartbeat which of its Targets' namespaces it can observe and execute in, found with `SelfSubjectAccessReview`, so the control plane and the web app know before anyone requests a Deployment.

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
| `--registry-pull-secret` | none | Docker-config Secret for fetching charts |
| `--heartbeat-interval` | `30s` | |
| `--poll-interval` | `5s` | |
| `--max-concurrent-applies` | `4` | |
| `--health-address` | `:8080` | Liveness and readiness |

There is no `--execute` flag: whether the agent can execute in a namespace is whatever RBAC grants it there, which the chart's `execute.namespaces` sets. `--credentials-store-path` is replaced by `--state-secret`.

## Code layout

```
apps/k8s-agent/
  cmd/main.go              calls agent.New().Execute()
  internal/agent/          Cobra command, flags, the run loop, leader election
  internal/apiclient/      oapi-codegen client for the agent's operations only, as ivaldictl does
  internal/auth/           the credential transport: projected token, bearer, or mTLS; renewal
  internal/state/          the state Secret
  internal/targets/        the Target list, namespace access checks, informer lifecycle
  internal/observe/        selector matching, image digests, observations
  internal/health/         kstatus to Deployment status, debouncing
  internal/execute/        assignments, chart fetching, values, Helm, post-renderer
  internal/variables/      bindings, fetching secrets, masking, writing the Kubernetes Secret
pkg/secretstore/           the secret-store interface and its providers: AWS Secrets Manager, Vault, Kubernetes
```

The agent imports nothing from `apps/control-plane`. Its new dependencies are `client-go`, the Helm SDK, `cli-utils` for kstatus, and the AWS and Vault SDKs through `pkg/secretstore`. The providers live in `pkg/` because ADR 0004 says the broker and secret-store interfaces are shared by both planes; the control plane uses the kinds and settings to validate stores, never to fetch.

Tests use client-go's fake clientset, a fake Helm action configuration, a fake secret store, and `httptest` for the control plane. An end-to-end test against `kind` runs in CI later.

## Control-plane changes

1. **Clusters.** The `clusters` table, `/v1/clusters` with `cluster.read`/`cluster.manage` on the Organization, `locator.cluster` validated against it, and `targets.agent_principal_id` replaced by the Cluster's agent in every agent check.
2. **Cluster enrolment.** `POST /v1/clusters/{cluster_id}/enrolments` replaces the Target one; the enrolment token names a Cluster instead of a Target; redeem sets the Cluster's agent and returns the Cluster and Organization IDs.
3. **Agent-scoped operations**, for the caller's Cluster only:
   - `GET /v1/agent/targets`: its Targets, each with its namespace and watch list;
   - `GET /v1/agent/assignments`: in-flight Deployments across its Targets, replacing `GET /v1/targets/{id}/assignments`;
   - `POST /v1/agent/heartbeat`: the agent's version, the Kubernetes version, and what it can do in each Target's namespace, replacing `POST /v1/targets/{id}/reports`;
   - `POST /v1/agent/observations`: a Target ID, an Application ID, the images and digests, the workloads, an optional `external_ref`, and a `report_id`, running the observed-Deployment flow.
   `POST /v1/deployments/{id}/events` stays as it is, with the agent check moved to the Cluster.
4. **Refuse what cannot run.** A Deployment request to a Target whose namespace the agent last reported it cannot execute in is refused with that reason, instead of waiting for the agent to fail it.
5. **Secret Stores.** A first slice of the credentials capability: `credentials.secret_stores` and `/v1/secret-stores`, Workspace-scoped, with their own read and manage permissions.
6. **Deployment variables.** Variables, Variable Sets, and Release snapshots, as designed in [deployment variables](deployment-variables.md), with each assignment carrying the winning value of every variable for its Target.

Changes 1 to 3 replace operations that only an agent calls, and no agent exists yet, so nothing deployed depends on the old ones. The separate runner listener (`runners.port`, 8661) is configured but not served; the agent uses the API listener until it is.

## Invariants

- **The agent only connects out**: to the control plane, its registries, and its secret stores.
- **The agent acts only where the cluster's operators let it**, enforced by RBAC rather than by its code.
- **The agent never applies without an assignment.**
- **A secret value never leaves the cluster.** The control plane holds references and version IDs; values exist only in the agent's memory during an apply and in the namespace's Secret (or Helm values, when chosen).
- **A Deployment uses only Secret Stores allowed for its Environment.**
- **Every report is idempotent** through its `report_id`.
- **Everything the agent reports is a digest it saw running**, never a tag.

## Open questions

- **Secret stores to support first.** This draft proposes AWS Secrets Manager, Vault, and Kubernetes Secrets. GCP Secret Manager and Azure Key Vault depend on customers.
- **Secret identity per namespace.** Whether the agent resolves secrets as a per-namespace ServiceAccount, so cloud policies can be scoped per Target, or keeps one identity with `environment_ids` and the customer's own policy as the controls.
- **Secrets that change between Deployments.** Today a rotated secret reaches workloads at the next Deployment. Re-resolving on a schedule and rolling the workloads when a version changes would be a new kind of Deployment, `reason: secret_rotation`, and needs its own design.
- **Multi-arch digests.** CI usually reports an image index digest; whether `imageID` shows the index or a platform digest depends on the container runtime. If it is the platform digest, Builds need to record their platform digests too.
- **Which deployers to read first.** Argo CD Applications and Flux HelmReleases give a better `external_ref` and status than inferring from workloads.
- **Argo Rollouts and other workload kinds**, which kstatus may not handle without help.
- **Sidecars.** Whether injected images (Istio, Linkerd, Vault agent) are ignored by a list in the control plane or by reporting only containers named in the Pod template.
- **Plain manifests and image-only Releases**, after Helm.
- **Streaming instead of polling**, once the runner listener is served.
