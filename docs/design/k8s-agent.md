# Kubernetes agent

- Status: Control plane implemented; agent not yet
- Date: 2026-11-08; updated 2026-11-14
- Related: [ADR 0001](../adr/0001-deployment-topology.md), [ADR 0003](../adr/0003-identity-access-and-policy.md), [ADR 0004](../adr/0004-credentials-and-keys.md), [ADR 0006](../adr/0006-release-and-delivery-tracking.md), [ADR 0008](../adr/0008-reaching-customer-systems.md), [ADR 0013](../adr/0013-one-agent-per-runtime.md), [ADR 0014](../adr/0014-agents-serve-clusters.md), [delivery data model](delivery-data-model.md), [workload enrolment](workload-enrolment.md)

This document designs `ivaldi-k8s-agent` (`apps/k8s-agent`), the agent that runs inside a customer's Kubernetes cluster ([ADR 0013](../adr/0013-one-agent-per-runtime.md)). One agent serves a whole cluster, and through it every Target in that cluster. It has the two jobs [ADR 0006](../adr/0006-release-and-delivery-tracking.md) gives a runtime agent: **observe** what runs on its Targets and report it, and, when allowed, **execute** Deployments the control plane assigns to it. When it executes, it resolves the Application's secrets from the customer's secret manager and hands them to the chart, so secret values never reach Ivaldi ([ADR 0004](../adr/0004-credentials-and-keys.md)). It never touches the control-plane database and only ever connects out ([ADR 0008](../adr/0008-reaching-customer-systems.md)).

The control plane already serves part of what the agent needs: enrolment and renewal, Deployment reports, and per-Target assignment polling and heartbeats. What it lacks is listed under [Control-plane changes](#control-plane-changes).

## Decisions this design is built on

- **One agent per cluster, serving many Targets.** A Target is still a namespace in a cluster, and still what Environments, authorization, and Deployments refer to. What changes is who serves it: a new delivery object, the **Cluster**, records the agent for a cluster, and every Target in that Cluster is served by it. One install per cluster is easier to run than one per namespace, and Targets can be added without installing anything.
- **The cluster's operators decide what the agent may touch.** Ivaldi users with `environment.manage` can create a Target for any namespace of a Cluster, but the agent can observe only the namespaces its chart lets it read, and execute only in the namespaces its chart binds it to. Both are Kubernetes RBAC set at install time, not settings in Ivaldi.
- **The agent polls; the control plane never calls in.** It sends a heartbeat, polls for assignments across all its Targets, and reports, all over HTTPS to the control plane's `/v1` API. A streaming connection can replace polling later without changing the contracts.
- **Kubernetes identity first.** The default credential method is `kubernetes`: the agent presents a projected ServiceAccount token with audience `ivaldi`, which the kubelet rotates. `bearer` and `mtls` stay available for control planes that cannot reach the cluster's OIDC issuer.
- **Execution is opt-in twice.** The control plane executes only on Environments with `execution_enabled`, and the agent executes only in namespaces its chart grants write access to.
- **Helm first.** A Release's deployment definition is its `helm_chart` artifact, and the assignment carries the Helm release name in `apply.helm.release_name`. Plain manifests and image-only Releases come later, as other ways the same agent applies a Release.
- **Variables are resolved in two halves.** An Application's [deployment variables](deployment-variables.md) hold plain values and references to secrets in the customer's secret manager. The control plane picks the winning value of each for the Target; the agent fetches secrets with its own cloud identity, fills in bindings, and renders the chart. It records which secret versions it used, never their values.
- **Digests, never tags.** The agent reports the digest each container is actually running, read from the Pod's status, and fetches charts by digest.
- **The agent decides nothing about policy.** It applies what it is assigned and reports what it sees. Lifecycle checks, approvals, findings, and Release matching stay in the control plane ([ADR 0003](../adr/0003-identity-access-and-policy.md)).

## Clusters

A **Cluster** is a Kubernetes cluster an Organization deploys to ([ADR 0014](../adr/0014-agents-serve-clusters.md)). It belongs to the Organization rather than a Workspace, because one cluster usually hosts several Workspaces' Environments.

- **clusters** (delivery, prefix `cls`): `slug` unique in the Organization and immutable after creation, `display_name`, `runtime` (`kubernetes`), `agent_principal_id`, `agent_capabilities` (the cluster-wide facts from the last heartbeat), `agent_last_failure` (the last refused enrolment, for the operator), `last_reported_at`, and soft deletion.
- **Targets name their Cluster.** A Target stores `cluster_id`, and its `locator.cluster` is the Cluster's slug, so the API shape is unchanged. A Kubernetes Target's slug must name an active Cluster in the Organization.
- **One active Target per Cluster namespace.** A unique index on `(organization_id, cluster_id, namespace)` refuses a second; the error names the Target already there and its Environment.
- **A Target's agent is its Cluster's agent.** `targets.agent_principal_id` and `targets.last_reported_at` are gone. Every check that a caller is "the Target's agent" resolves Deployment → Target → Cluster. Capabilities are per Target (`can_observe`, `can_execute`, `capability_reason`); liveness is the Cluster's.
- **Permissions.** `cluster.read` and `cluster.manage` on the Organization. Creating a Cluster and minting its enrolment tokens need `cluster.manage`; creating a Target in it still needs `environment.manage` on the Target's Environment. The built-in roles pick these up by rule, so IAM does not change.
- **Archiving** a Cluster is refused while it has active Targets. Archiving one without Targets suspends its agent and revokes its credentials in the same transaction; restoring it leaves it agentless until a new enrolment token is redeemed.

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
   - heartbeat every `heartbeat_interval` (30s, `runners.heartbeat_interval`), carrying what it can do in each namespace;
   - poll assignments every `poll_interval` (5s, `runners.agent_poll_interval`);
   - refresh its Targets and watch lists every `watch_interval` (60s, `runners.agent_watch_interval`), starting and stopping informers as Targets come and go;
   - renew the credential at 50% of its life, ±5% jitter ([workload enrolment](workload-enrolment.md)).

   The control plane sends the three intervals in `GET /v1/agent/targets`; the agent has no interval flags of its own, so its pace always matches the staleness rule.
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

- **Install details.** `POST /v1/clusters/{cluster_id}/enrolments` also returns `install`: `{server_url, chart, chart_version, credential_kinds}`. `server_url` is the control plane's `external_url`; the chart and version come from `runners.agent_chart` and `runners.agent_chart_version`, which defaults to the control plane's own version. The web onboarding guide uses these so its commands cannot drift from the release.
- **Redeem.** The agent calls `POST /v1/agent/enrolments/redeem` with the token, the method, and its projected token. The control plane creates the `runtime_agent` workload principal, sets it as the Cluster's agent, and suspends the Cluster's previous agent. The response names the Cluster and the Organization, and returns the credential and, for `bearer`, its token.
- **One identity, one Cluster.** A certificate or Kubernetes service-account identity names one live workload. Redeeming it for a second active Cluster is refused with `409 identity_in_use`; the refusal is recorded on the Cluster being enrolled, in `AgentStatus.last_failure`, and audited. The previous Cluster keeps working.
- **State Secret.** The enrolment token is single-use and Pods are replaced, so the agent keeps what it learned in a Secret, `<release>-state`: Cluster ID, Organization ID, workload principal ID, credential ID, kind, and expiry, and the `bearer` token if there is one. Its Role allows `get`/`update` on that Secret and `get` on the enrolment Secret, by name.
- **Projected token.** The chart mounts a projected ServiceAccount token at `/var/run/secrets/ivaldi/token` with audience `ivaldi` (to match `runners.kubernetes_audience`) and a one-hour expiry. The agent re-reads it on every request and sends it with `X-Ivaldi-Credential: kubernetes`.
- **Renewal** calls `POST /v1/agent/credentials/renew`. For `bearer`, the new token is written to the state Secret before it is used.
- **When the credential is gone** (401, or 403 because the Cluster has another agent), the agent stops working, marks itself not ready, and logs that it needs a new enrolment token. It never retries a redeemed token.

## Observing

### What it watches

The agent calls `GET /v1/agent/targets` (below). It returns the Cluster, the intervals, and each Target with its namespace, its `can_observe`/`can_execute` flags, and the active Applications placed on it with their workload selectors (default `ivaldi.build/application=<application ID>`), artifact names, and the current Deployment of each placement.

It watches Deployments, StatefulSets, DaemonSets, ReplicaSets, and Pods in its Targets' namespaces, and nowhere else:

It runs one informer set per Target namespace it can read, whether the chart grants cluster-wide read (`observe.namespaces` left empty) or lists namespaces. A watch cannot be limited to a list of namespaces, so a cluster-wide informer would cache every Pod in the cluster to report on a few namespaces. The heartbeat's `watch_mode` says which grant the agent has (`cluster` when it may list and watch the observed kinds in every namespace), not how it watches. The informers follow the Targets at every refresh: a new Target's namespace starts its informers, a removed one stops them, and `/readyz` waits until they have synced.

A Target whose namespace the agent cannot read is reported as not observable in the heartbeat, and the web app shows it.

### What a workload's Release is

For each Application on each Target, the agent finds the workloads matching its selector and reads the images their ready Pods of the current revision are running:

- The digest comes from `status.containerStatuses[].imageID`, normalised to `<repository>@sha256:<digest>`, unless the Pod spec already names the image by digest.
- Images are matched to the Application's artifacts by `artifact_name`. Unmatched images are reported too, so the control plane can create observed artifacts.
- Init containers count, because a `migrate` image is often one.

The result is a set of `(artifact_name, digest)` pairs per Application and Target. The agent reports an **observation** (`POST /v1/agent/observations`) only when that set changes, or once after start-up. The request carries the Target, the Application, the images, the workloads they came from, an opaque `external_ref` naming the deployer when the agent can tell, a `report_id`, and the agent's `observed_at`.

An observation that matches the placement's current Release changes nothing and returns that Deployment. Otherwise the control plane records the artifacts and Builds it did not know, an observed published Release named from the digest set, and an observed healthy Deployment; it makes that Deployment current, supersedes the previous one, and records the observation's `report_id` on its first status event. Only container images are compared: a chart in an executed Release does not make the image set differ.

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

The agent polls `GET /v1/agent/assignments` for every Deployment on any of its Targets that is `applying` or `progressing`. Each assignment carries the Deployment, Release, Target, Environment, and a deployer-neutral `apply` block: the deployer (`helm`), namespace, definition artifact, overlay values files, images by digest, variables, secrets, the deployer's own settings (for Helm, the release name), reason, and for a rollback the Deployment it restores. It works at most one per Application and Target, and at most `max_concurrent_applies` (default 4) in total.

The `apply` block is the boundary that keeps Helm an implementation. Argo CD and Flux add a deployer value and their own sub-object beside `helm`, and nothing else changes. A field or store kind added later is additive; an agent that receives a deployer or store kind it does not support fails the Deployment before applying, naming it.

An assignment for a namespace the agent cannot write to is reported `failed` straight away with that reason; the control plane refuses such a Deployment at request time from the heartbeat's capabilities.

While it applies a Deployment, changes it sees for that Application on that Target are reported against the Deployment, not as an observation.

### Applying with Helm

1. **Fetch the chart.** `apply.definition` must be a `helm_chart`. The agent pulls `oci://<published_as>@<digest>` and checks the digest. Registry credentials come from a docker-config Secret named in the chart's values (`registry.pullSecret`).
2. **Resolve variables**, as described in [Variables and secrets](#variables-and-secrets). A secret that cannot be fetched, an undefined binding, or a cycle fails the Deployment before anything is applied, naming the variable but not any value.
3. **Build the values.** In order, later wins:
   1. the chart's defaults;
   2. each of `apply.overlays`, the chart-relative values files the Placement selected, applied in order (deep-merged maps, replaced lists, `null` deletes a key);
   3. each variable with a `values_path`, at that path: `text` variables, and `secret` variables delivered as values;
   4. the Release's images, under `global.ivaldi.artifacts.<artifact name>` as `repository`, `digest`, and `image` (`<repository>@<digest>`);
   5. `global.ivaldi.application`, `.release`, `.deployment`, `.version`, and `global.ivaldi.secrets.name` when secrets are delivered as a Kubernetes Secret.

   Every overlay and text value is free of bindings before the merge: `#{…}` in a scalar is replaced from the resolved variables and the system variables (`Ivaldi.Environment.Slug`, `Ivaldi.Cluster.Slug`, `Ivaldi.Target.Namespace`, `Ivaldi.Deployment.Id`, and so on). Undefined names and cycles fail the Deployment. `##{` writes a literal `#{`.
4. **Install or upgrade** the Helm release `apply.helm.release_name` in the Target's namespace with the Helm Go SDK, server-side apply, and no wait: the agent tracks the rollout itself. The Helm release is labelled `ivaldi.build/deployment=<deployment ID>`.
5. **Label what it applies.** A post-renderer adds `ivaldi.build/application=<application ID>` to every workload's metadata and Pod template, and `ivaldi.build/deployment` and `ivaldi.build/release` annotations to every object, so the default workload selector holds for any chart.
6. **Report** `progress` events for fetching, resolving, and applying, then `progressing`, then the [health](#health) mapping until the rollout is `healthy`, `degraded`, or `failed`.

The agent does not roll back on its own: a rollback is a new Deployment of an earlier Release, with `reason: rollback` and `restores` naming the Deployment it replays. Applying it is the same code path as a forward Deployment, which is why there is no `helm rollback` call anywhere in the agent. The control plane rebuilds the earlier Release's chart digest, values snapshot, and secret pins; the agent fails the Deployment before applying if a pinned secret's fingerprint no longer matches.

### Restarting mid-apply

Every report has a deterministic `report_id`, `<deployment ID>:<step>`, so a retry is stored once. On picking up an assignment, the agent reads the Helm release's history; if the latest revision already carries this Deployment's ID, it skips to tracking the rollout.

## Variables and secrets

What an Application's variables are, how scopes pick a value, and how a Release snapshots them is in [deployment variables](deployment-variables.md). This section is the agent's half: fetching secrets, filling in bindings, and giving the result to the chart.

### Secret Stores

A **Secret Store** is a secret manager the agent can reach. It belongs to the credentials capability ([ADR 0004](../adr/0004-credentials-and-keys.md)) and to a Workspace, like a Registry, and holds no credentials:

- `kind`: `kubernetes`, `aws_secrets_manager`, `vault`, or `doppler`.
- Non-secret settings for the kind: nothing for `kubernetes`, which reads Secrets in the Target's namespace; the AWS region and an optional assume-role ARN; the Vault address, KV v2 mount, Kubernetes auth role, and optional Enterprise namespace; the Doppler project/config, and the name and key of the Kubernetes Secret in the agent's namespace that holds the token.
- `environment_ids`: optional. When set, only Deployments to those Environments may use the store. This is what keeps a staging Deployment from naming a production store.
- A Doppler service token is scoped to one config, so a Doppler store serves one config and names one or more Environments. The alternative is the Doppler Kubernetes operator writing a Secret, which Ivaldi reads through the `kubernetes` store.

Behind it sits the Ivaldi-owned secret-store interface ADR 0004 asks for, in `pkg/` because the agent implements its providers and the control plane validates against the same kinds.

### How the agent resolves them

- **Bindings, then secrets.** Text values bind other text values and system variables; an undefined name or a cycle fails the Deployment. Text values do not bind secrets, so a secret never hides inside a text value; overlays may (below). A secret's path binds the same names, filled in before the secret is read, so one reference serves every Environment.
- **With its own identity.** The agent authenticates to the store as its Kubernetes ServiceAccount: EKS Pod Identity or IRSA for AWS, Vault's Kubernetes auth with the store's role, and its own RBAC for `kubernetes`. Ivaldi holds no secret-manager credentials anywhere. A Vault login sends a service-account token to the store's address, which comes from the control plane, so the agent logs in only to addresses its operator listed (`secrets.vault.addresses` in the chart), with a token projected for Vault's audience rather than the API token, and follows no redirect.
- **At render time, in memory.** Values are fetched for one apply, used, and dropped. They are never logged, never put in a report's `detail`, and never sent to the control plane.
- **As a Kubernetes Secret by default.** For `deliver_as: secret`, the agent writes one immutable Secret per Deployment, `ivaldi-<application>-<deployment>`, labelled `ivaldi.build/application` and `ivaldi.build/deployment`, in the Target's namespace, with one key per secret variable (the variable's name, which is why names are limited to what a Secret key allows), and passes its name to the chart as `global.ivaldi.secrets.name`. Each Deployment's Secret has a new name, so the Pod template changes and the workloads roll. Once a Deployment is healthy, the agent deletes the Application's Secrets in the namespace except its own and the newest other one, whose Pods may still be draining. A rollback recreates its own Secret from its pinned references, so it never needs an old one.
- **As values only when asked.** `deliver_as: value` puts the secret into the chart's values at `values_path`, for charts that cannot read a Secret. Helm stores values in its release records, so the secret then sits in every revision's history in the namespace and shows in `helm get values`. The web app says so where the option is chosen.
- **Pinned versions are fetched exactly.** When the assignment pins a secret's version (because the value pins it, or because the Deployment is a rollback), the agent fetches that version and fails the Deployment if the store no longer has it. It never falls back to the latest on its own. AWS uses a VersionId, Vault a KV v2 version, and Kubernetes a `resourceVersion`.
- **Every secret also gets a keyed fingerprint.** The agent computes an HMAC-SHA-256 of each resolved value with a 32-byte key it keeps in a Secret in its own namespace (`fingerprint.secretName`), and reports each secret's name, version, fingerprint, and delivery in the `detail` of the report that moves the Deployment to `progressing` (`secrets`), with `secrets_in_values` naming those that went into the values. The key never leaves the cluster, so a stored fingerprint cannot be brute-forced back into a short secret. A rollback pins the fingerprint and the provider version; it fails before applying when the value changed. Doppler's secrets API returns a config's current values rather than a per-secret version, so a Doppler rollback pins the fingerprint alone and refuses when it differs.
- **Fingerprint keys are versioned.** New fingerprints carry the newest key version (`hmac-sha256:v2:…`), and a pinned fingerprint is checked with the key version it names. Removing an old key makes rollbacks pinned with it fail with a named reason. Losing the key Secret has the same effect for every earlier Deployment; the chart marks it `helm.sh/resource-policy: keep`. Fingerprints are comparable only within one Cluster, which is where a rollback runs.
- **Overlays can bind secrets too.** A `#{…}` binding in an overlay value is resolved before the merge. If it binds a secret, that Deployment is treated as delivering the secret as a value, because the chart receives a literal; the deploy sheet warns that Helm's release history in the namespace will hold it. A variable with `deliver_as: secret` keeps the value in the immutable Secret instead.
- **Masked wherever it reports.** Before sending any `detail` or log line, the agent replaces every resolved secret value longer than three characters with `****`, including inside Helm's own error messages, through a `slog` handler every log line passes, so a new log line cannot forget, which can echo values. As Octopus warns, literal masking is not foolproof, so the agent also never puts rendered values or manifests in a report.

### Who can read what

One cluster-wide agent with one cloud identity can read every secret that identity is granted, for every Target in the cluster. Two controls keep that from becoming a path from staging to production:

- **In Ivaldi:** a store's `environment_ids`, checked when a value is saved and again when a Deployment is requested.
- **In the customer's cloud:** the IAM policy or Vault role the agent's identity holds, which the customer scopes as they like.

A stronger option is for the agent to resolve each Target's secrets as a ServiceAccount in that Target's namespace, using the TokenRequest API, so a cloud policy can be written per namespace. It needs `create` on `serviceaccounts/token` for one ServiceAccount in each execute namespace, and one more identity per namespace for the customer to set up. It is left as an open question rather than built in.

## Access in the cluster

The chart creates one ServiceAccount in the agent's namespace, and:

| Binding | Grants | Where |
|---|---|---|
| Role in its own namespace | `get`/`update` on its state Secret, `get`/`update` on its fingerprint-key Secret, `get` on its enrolment Secret, `get`/`create`/`update` on its Lease | Always |
| ClusterRole `ivaldi-agent-observe` | `get`, `list`, `watch` on Deployments, StatefulSets, DaemonSets, ReplicaSets, ControllerRevisions (a DaemonSet's current revision), Pods, Events. Not Secrets. | Bound cluster-wide with a ClusterRoleBinding when `observe.namespaces` is empty; otherwise with a RoleBinding in each listed namespace |
| RoleBinding to the built-in `edit` ClusterRole | Create and change most namespaced objects, including Secrets, but not Roles or RoleBindings | Each namespace in `execute.namespaces` |
| RoleBinding to `admin` instead | Also Roles and RoleBindings, for charts that create them | Each namespace in `execute.namespaces` when `execute.allowRBAC` is true |
| RoleBinding to a `secrets-read` Role | `get` on Secrets | Each namespace with a `kubernetes` Secret Store in use, listed in `secrets.kubernetesNamespaces` |

Execution never gets a ClusterRoleBinding, so the agent can change only namespaces the cluster's operators listed. Adding an execute namespace is a `helm upgrade` of the agent, which is deliberate: it is the cluster operator's consent. Charts that create cluster-scoped objects, such as CRDs, cannot be executed by the agent.

The agent reports in each heartbeat which of its Targets' namespaces it can observe and execute in, found with one `SelfSubjectRulesReview` per namespace at each Target refresh (a `SelfSubjectAccessReview` confirms any denial when the rules review is incomplete), so the control plane and the web app know before anyone requests a Deployment. Observing needs `get`, `list`, and `watch` on the observed kinds; executing is judged by a sample of what applying a chart needs, `get`, `list`, `create`, `update`, `patch`, and `delete` on Deployments, Services, ConfigMaps, and Secrets (where Helm keeps its release history), which `edit` and `admin` both grant. A capability that is false carries the first permission missing as its reason, such as `RBAC denies create on deployments.apps`.

## Configuration

Flags, with environment variables of the same name prefixed `IVALDI_`, and chart values that set them:

| Flag | Default | Meaning |
|---|---|---|
| `--server-url` | none, required | The control plane's URL |
| `--authentication-method` | `kubernetes` | `kubernetes`, `bearer`, or `mtls` |
| `--enrolment-secret` | `<release>-enrolment` | Secret holding the one-time enrolment token |
| `--state-secret` | `<release>-state` | Secret the agent keeps its state in |
| `--fingerprint-secret` | `<release>-fingerprint` | Secret holding the per-Cluster fingerprint key |
| `--kubernetes-token-file` | `/var/run/secrets/ivaldi/token` | The projected token |
| `--client-cert-file`, `--client-key-file`, `--client-ca-file` | none | For `mtls`, and a private CA for the control plane |
| `--registry-pull-secret` | none | Docker-config Secret for fetching charts |
| `--plain-http-registries` | none | Registry hosts, such as `registry:5000`, to fetch charts from over plain HTTP; for local development (`registry.plainHTTPHosts`) |
| `--max-concurrent-applies` | `4` | |
| `--health-address` | `:8080` | Liveness and readiness |
| `--lease-name` | `ivaldi-agent` | The Lease in the agent's namespace |
| `--namespace` | the Pod's | The agent's namespace, holding its Secrets and Lease |
| `--pod-name` | the host name | This replica's identity in the Lease |
| `--kubeconfig` | in-cluster | For running outside a cluster |
| `--log-level` | `info` | `debug`, `info`, `warn`, or `error` |

The enrolment Secret holds the token under the key `token`; the state Secret holds the state as JSON under `state.json`, including the SHA-256 of the last enrolment token the agent tried, so it never redeems one twice. Re-enrolling is replacing the token in the enrolment Secret: the agent redeems a token it has not tried at its next start, or within 30 seconds while it waits without a credential. A credential the control plane refuses (401, or a refused renewal) is removed from the state, and the agent releases the Lease and waits, not ready, for a new token; it does not exit, so the Pod does not crash-loop.

There is no `--execute` flag: whether the agent can execute in a namespace is whatever RBAC grants it there, which the chart's `execute.namespaces` sets. There are no heartbeat, poll, or watch flags either: the control plane sends the three intervals in `GET /v1/agent/targets`, so the agent's pace always matches the staleness rule. `--credentials-store-path` is replaced by `--state-secret`.

## Code layout

```
apps/k8s-agent/
  cmd/main.go              calls agent.New().Execute()
  internal/agent/          Cobra command, flags, the run loop, leader election
  internal/apiclient/      oapi-codegen client for the agent's operations only, as ivaldictl does
  internal/auth/           the credential transport: projected token, bearer, or mTLS; renewal
  internal/state/          the state Secret
  internal/targets/        the Target list, namespace access checks, informer lifecycle
  internal/values/         overlays, bindings, merge, Ivaldi values
  internal/deploy/         the Deployer interface, Assignment, State
  internal/deploy/helm/    Helm SDK: fetch, render, apply, history, status
  internal/deploy/status/  kstatus helper the Helm deployer uses
  internal/observe/        selector matching, image digests, observations
  internal/secrets/        binding resolution, masking, fingerprinting, the Kubernetes Secret
  internal/report/         report IDs, retries, dedupe
pkg/secretstore/           the secret-store interface and its providers: Kubernetes, AWS Secrets Manager, Vault, Doppler
```

The agent imports nothing from `apps/control-plane`. Its new dependencies are `client-go`, the Helm SDK, `cli-utils` for kstatus, and the AWS and Vault SDKs through `pkg/secretstore`. The providers live in `pkg/` because ADR 0004 says the broker and secret-store interfaces are shared by both planes; the control plane uses the kinds and settings to validate stores, never to fetch.

Tests use client-go's fake clientset, a fake Helm action configuration, a fake secret store, and `httptest` for the control plane. An end-to-end test against `kind` runs in CI later.

## What the control plane provides

Implemented, and what the agent is built against:

1. **Clusters.** `delivery.clusters`, `/v1/clusters` with `cluster.read`/`cluster.manage` on the Organization, Targets linked by `cluster_id` and `locator.cluster`, per-Target capabilities, and one active Target per Cluster namespace.
2. **Cluster enrolment.** `POST /v1/clusters/{cluster_id}/enrolments`; the token names a Cluster; redeem sets the Cluster's agent and returns the Cluster and Organization IDs, and the `install` details. One identity serves one active Cluster; a refused identity is recorded on the Cluster being enrolled.
3. **Agent-scoped operations**, for the caller's Cluster only:
   - `GET /v1/agent/targets`: the Cluster, the intervals, and its Targets with namespaces, capabilities, and the placed Applications;
   - `GET /v1/agent/assignments`: in-flight Deployments across its Targets, with the `apply` block;
   - `POST /v1/agent/heartbeat`: the agent's version, the Kubernetes version, and what it can do in each Target's namespace;
   - `POST /v1/agent/observations`: the observed-Deployment flow.
   `POST /v1/deployments/{id}/events` stays per Deployment, with the agent check moved to the Cluster.
4. **Refuse what cannot run.** A Deployment request is refused when the Target's Cluster has no agent, when the agent last reported longer ago than three heartbeat intervals, or when it reported it cannot execute in the Target's namespace.
5. **The web onboarding guide.** It names the Cluster, mints a token, shows the values and install commands from the `install` details, polls the agent's status, and adds Targets.

Still to build, and designed elsewhere:

- Approval of a refresh of a Release's variables that changes production ([deployment variables](deployment-variables.md#refreshes-that-change-production-need-approval)).

Secret Stores (`credentials.secret_stores`, `/v1/workspaces/{id}/secret-stores` and `/v1/secret-stores/{id}`, with `secret_store.read`/`secret_store.manage` on the Workspace) and deployment variables, with rollbacks that replay a Deployment's revision, secret versions, and fingerprints, are implemented; setting stores up is in [the self-hosting guide](../self-hosting/secret-stores.md).

The separate runner listener (`runners.port`, 8661) is configured but not served; the agent uses the API listener until it is.

## Invariants

- **The agent only connects out**: to the control plane, its registries, and its secret stores.
- **The agent acts only where the cluster's operators let it**, enforced by RBAC rather than by its code.
- **The agent never applies without an assignment.**
- **A secret value never leaves the cluster.** The control plane holds references, provider versions, and keyed fingerprints whose key stays in the cluster; values exist only in the agent's memory during an apply and in the namespace's Secret (or Helm values, when chosen).
- **One active Target per Cluster namespace**, enforced by a unique index.
- **A stale capability is not a capability.** An agent that has not heartbeated recently cannot be assigned new work.
- **A Deployment uses only Secret Stores allowed for its Environment.**
- **Every report is idempotent** through its `report_id`.
- **Everything the agent reports is a digest it saw running**, never a tag.

## Open questions

- **GCP Secret Manager and Azure Key Vault.** The first stores are Kubernetes Secrets, AWS Secrets Manager, Vault, and Doppler; the other two depend on customers.
- **Doppler's history API.** Whether a past config version can be read through the secrets API. If it can, Doppler gets true per-secret pinning and the fingerprint becomes an extra check rather than the only one.
- **Secret identity per namespace.** Whether the agent resolves secrets as a per-namespace ServiceAccount, so cloud policies can be scoped per Target, or keeps one identity with `environment_ids` and the customer's own policy as the controls.
- **Secrets that change between Deployments.** Today a rotated secret reaches workloads at the next Deployment. Re-resolving on a schedule and rolling the workloads when a version changes would be a new kind of Deployment, `reason: secret_rotation`, and needs its own design.
- **Multi-arch digests.** CI usually reports an image index digest; whether `imageID` shows the index or a platform digest depends on the container runtime. If it is the platform digest, Builds need to record their platform digests too.
- **Argo CD and Flux as deployers.** They are driven through their CRs, which also give a better `external_ref` and status than inferring from workloads. The deployer interface is ready for them; an ADR should record the choice before the first one is built.
- **Argo Rollouts and other workload kinds**, which kstatus may not handle without help.
- **Sidecars.** Whether injected images (Istio, Linkerd, Vault agent) are ignored by a list in the control plane or by reporting only containers named in the Pod template.
- **Plain manifests and image-only Releases**, after Helm.
- **Streaming instead of polling**, once the runner listener is served.
