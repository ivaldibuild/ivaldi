# ADR 0006: Release and delivery tracking

- Status: Proposed
- Date: 2026-10-18

## Context

Ivaldi is both an agentic software craft shop and a continuous delivery platform. The Ship and Observe stages of its lifecycle need a record of what was built, what was released, and where it is running. That record closes the provenance chain from Work Item through Session, Change, commit, pull request, and CI to Build, Deployment, and Release.

Kubernetes is the first runtime. Others will follow, so the domain cannot be shaped around Kubernetes or around any one way of deploying to it, such as Helm.

Teams adopting Ivaldi usually already have a way to deploy: Argo CD, Flux, a CI pipeline, or scripts. Some will want Ivaldi to deploy for them. Others will only want Ivaldi to see what their existing tools did.

Clusters sit inside customer networks. Under [ADR 0001](0001-deployment-topology.md), the control plane never reaches into them; data-plane components connect out to it.

The repository reserves `pkg/release` and `apps/helm-agent`, both empty. [ADR 0005](0005-data-model-ownership.md) keeps control-plane database models out of `pkg/`, but it does not say what a capability may put in `pkg/` when both planes need part of it. [ADR 0003](0003-identity-access-and-policy.md) already follows such a split: IAM lives in the control plane while `pkg/identity` holds offline verification, and policy storage lives in the control plane while `pkg/policy` holds the evaluation engine.

## Decision

### Vocabulary

- **Application:** a deployable unit inside a Project, such as an API, a web app, or a background worker. A Project can have several Applications, and each is released and deployed on its own. An Application declares the artifacts it is made of, such as container images and a Helm chart.
- **Build:** one version of one artifact, identified by its content digest. A Build reported by CI is linked to the commit it was built from.
- **Release:** a versioned set of Builds, one for each of an Application's artifacts, that make up one version of the Application. Every Release belongs to one Channel.
- **Channel:** one release strategy for an Application, such as mainline, hotfix, or early access. Its rules decide which Releases may join it, and its Lifecycle decides where they may be deployed.
- **Lifecycle:** an ordered list of phases, each naming one or more Environments, that a Release moves through. A Lifecycle belongs to a Workspace and can be used by many Channels.
- **Environment:** a named stage that Releases move through, such as staging or production. An Environment belongs to a Workspace.
- **Target:** a concrete place an Environment runs, such as a cluster and namespace. A Target belongs to one Environment, and one Environment can have several Targets.
- **Deployment:** one Release of an Application applied to one Target at one point in time, with its status, its trigger, and the policy decision that allowed it.
- **Registry:** a place artifacts are stored, such as an OCI registry. A Registry belongs to a Workspace.

Application, Channel, Lifecycle, Environment, Target, and Registry are new terms and are added to the domain vocabulary in `AGENTS.md`.

### Environments belong to a Workspace

Environments are defined once per Workspace, and every Project in that Workspace deploys into the same Environments. A Workspace's production is one place with one set of Targets and one set of approval rules, not a separate copy in each Project. A Deployment therefore joins two scopes: the Release comes from an Application in a Project, and the Target comes from the Workspace's Environment.

Environments are registered with IAM as resources under their Workspace, alongside Projects, so access can be granted per Environment. For example, someone can be allowed to deploy to staging but not to production. This adds Environment to the resource hierarchy in the [IAM data model](../design/iam-data-model.md): Organization → Workspace → Project or Environment.

### Channels and Lifecycles shape how Releases move

An Application can follow several release strategies at once, through Channels, a concept taken from [Octopus Deploy](https://octopus.com/docs/releases/channels). A Release joins the one Channel whose rules it meets, such as a version range, a pre-release tag pattern, or the Git branch its Builds came from. Each Channel follows a Lifecycle: mainline might go development → staging → production, while hotfixes go straight to production. A phase of the Lifecycle opens for a Release only once the earlier required phases have successful Deployments of it.

Lifecycles belong to the Workspace, like Environments, so a standard path is defined once and shared by many Applications. An Application deploys only to the Environments in its Channels' Lifecycles. Ivaldi refuses to execute a Deployment that breaks its Lifecycle, and records a finding on an observed Deployment that does.

### Ivaldi tracks every deployment and executes some of them

A Deployment has a **source**:

- **Executed:** Ivaldi applied it, after a policy decision.
- **Observed:** another system applied it, and Ivaldi saw the result.

Both produce the same Deployment record, so provenance, history, and observability work the same way whichever system did the deploying. Tracking ships first. Executing is an optional capability that a team can turn on per Environment.

### The control plane owns the records

The delivery capability, `apps/control-plane/internal/delivery`, owns Applications and their artifacts, Channels, Lifecycles, Registries, Builds, Releases, Environments, Targets, and Deployments. Its tables are described in the [delivery data model](../design/delivery-data-model.md). Following [ADR 0005](0005-data-model-ownership.md), it owns its tables, keeps its PostgreSQL code in `internal/delivery/postgres`, and keeps its migrations in `internal/delivery/migrations`. It is the only place these records are written.

It also:

- links each Build to its commit, and through it to Changes, Sessions, and Work Items;
- asks the single authorizer ([ADR 0003](0003-identity-access-and-policy.md)) before it hands an executed Deployment to an agent, so approval gates such as "production deployments need a human approval" are policy, not release code;
- serves the API that runtime agents report to.

### `pkg/release` holds the shared core, without storage

`pkg/release` is the shared core of the delivery capability. It contains what the control plane and the runtime agent must agree on:

- identifiers and status values for Builds, Releases, and Deployments;
- the Deployment status transitions, so both planes accept and reject the same changes;
- version parsing and comparison, and the description of what a Release contains;
- Channel rule matching and the Lifecycle phase check, so the CLI and CI integrations can check a Release before they submit it, using the same rules the control plane enforces.

It contains no database code, no PostgreSQL types, and no vendor clients.

This generalizes the existing pattern into a rule for every capability: **the storage-free part of a capability that more than one deployable needs lives in `pkg/`, and its records live in `apps/control-plane/internal/<capability>`.** The two packages are named for what they hold, so they need not share a name: the delivery capability keeps its records in `internal/delivery` and its shared core in `pkg/release`. `pkg/policy`, `pkg/orchestration`, `pkg/vcs`, and `pkg/events` follow the same rule.

### A runtime agent applies and observes

`apps/helm-agent` is renamed `apps/runtime-agent`. It runs inside the customer's environment, enrolls and authenticates as a workload ([ADR 0003](0003-identity-access-and-policy.md)), and connects out to the control plane. It never touches the control-plane database.

It does two jobs:

- **Observe:** watch Targets for workload changes, rollout progress, and health, and report them as Deployments with the observed source. Where an external deployer exposes its own state, such as an Argo CD Application or a Flux Kustomization, the agent can read it for richer status.
- **Execute:** when enabled, receive a Deployment assignment, apply it, and report its progress.

Runtimes sit behind an Ivaldi-owned interface. Kubernetes is the first provider. Helm and plain manifests are ways that provider applies a Release, not separate providers. A future runtime is a new provider, not a change to the domain.

### Deployments link to Builds by digest

A running workload is matched to a Build by its image digest, never by tag, because tags can be moved. A workload whose digest matches no known Build is still recorded, flagged as unknown provenance.

### Registry lookups run where the registry can be reached

Ivaldi contacts a Registry only to look artifacts up: to resolve a tag to a digest when a Release is created, and to fetch a deployment definition such as a Helm chart when executing. Clusters keep pulling images themselves. A customer's private registry is usually unreachable from Ivaldi's cloud, and its credentials should stay in the customer's environment ([ADR 0004](0004-credentials-and-keys.md)). So each Registry records whether lookups run from the control plane or from a data-plane workload, and a Registry marked for the data plane is never contacted from the control plane.

### What changed is derived from commits

Each Build records only the commit and ref it was built from. The commits, Changes, Sessions, and Work Items between two Releases are derived through the VCS and Change capabilities when they are needed, rather than copied from CI as Octopus build information does. Ivaldi already owns those records, so deriving them keeps one source of truth.

### Messages between the planes are contracts in `schemas/`

Deployment assignments and status reports are versioned contracts in `schemas/` ([ADR 0005](0005-data-model-ownership.md)). The control plane and a customer's agent can run different versions, so both must accept older contract versions.

### Builds and external deployments come in through the gateway

CI systems report Builds through the gateway's webhook module ([ADR 0002](0002-gateway-integration-ingress.md)), which normalizes them into Ivaldi events. Deployer webhooks, such as Argo CD notifications or VCS deployment events, take the same path and supplement what the agent observes.

## Consequences

- The provenance chain reaches running software, whichever tool deployed it.
- Teams can adopt Ivaldi without replacing their deploy tooling, and turn on execution later.
- Policy can gate only executed Deployments. An observed Deployment that skipped a required approval cannot be blocked, only recorded and flagged. The documentation must say this clearly.
- Observation means the agent needs read access to every Target it watches, and execution needs write access. Both are scoped per Target and granted to the agent's workload identity.
- The agent must handle several external deployers' status models. Each one it supports is another provider to maintain.
- Because Environments are shared across a Workspace's Projects, changing one, such as adding a Target or tightening production approvals, affects every Project in that Workspace. Teams that need different production setups need separate Workspaces.
- Renaming `apps/helm-agent` changes its module path and its references in docs and the IAM design. It has no code, so this is the cheapest time to rename it.

## Alternatives considered

- **Execute only: Ivaldi is the only way to deploy.** Rejected: it forces teams to replace working deploy tooling before they get any value, and anything deployed outside Ivaldi would be invisible.
- **Track only, never execute.** Rejected: policy-gated, agent-driven delivery is part of the product, and it needs an executor.
- **Put release records and their database models in `pkg/release`.** Rejected: it breaks [ADR 0005](0005-data-model-ownership.md) and couples the data plane to the control-plane schema.
- **Drop `pkg/release` and keep everything in the control plane.** Rejected: the agent and the control plane must share status values and transition rules, and duplicating them would let the planes drift.
- **One agent per deploy tool (a Helm agent, an Argo CD agent, and so on).** Rejected: it multiplies deployables and enrollments per cluster. One agent with providers covers the same ground.
- **Environments per Project.** Rejected: production is usually shared infrastructure with shared approval rules, and per-Project copies would drift apart and multiply the policy to maintain.
- **Lifecycle phases defined on each Channel.** Rejected: every Application would repeat the same path, and changing the standard path would mean editing every Channel in the Workspace.
- **A separate delivery service.** Rejected for now: capability before service. The delivery capability can be extracted later because it owns its tables.

## Open questions

- Whether generated code for the `schemas/` contracts lives in each capability's `pkg/` package or in one generated package.
- Whether promotion needs more than Lifecycle order, such as a waiting period before the next phase opens, and whether that belongs to Lifecycles or to policy.
- Which external deployers the agent observes first.
- Whether one agent covers a whole cluster or is scoped per namespace.
- How far Observe goes beyond rollout status and health, for example into metrics, logs, or error rates from the customer's observability tools.
