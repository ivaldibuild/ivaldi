# ADR 0014: Runtime agents serve Clusters, not Targets

- Status: Accepted; control plane implemented, agent in progress
- Date: 2026-11-08; accepted 2026-11-14
- Related: [ADR 0003](0003-identity-access-and-policy.md), [ADR 0006](0006-release-and-delivery-tracking.md), [ADR 0008](0008-reaching-customer-systems.md), [ADR 0013](0013-one-agent-per-runtime.md), [Kubernetes agent](../design/k8s-agent.md), [workload enrolment](../design/workload-enrolment.md), [delivery data model](../design/delivery-data-model.md)

## Context

A Target is one namespace in one Kubernetes cluster. Today a runtime agent serves exactly one Target. Enrolment is per Target (`POST /v1/targets/{target_id}/enrolments`). Redeeming the token creates a workload principal and writes it to `targets.agent_principal_id`, and every agent operation checks the caller against that column: assignments, reports, and heartbeats. A ServiceAccount or certificate names one live workload, so one agent identity cannot be enrolled for a second Target.

[ADR 0006](0006-release-and-delivery-tracking.md) left open whether one agent covers a whole cluster or one namespace. With the agent tied to a Target, the answer has defaulted to one namespace: a cluster that hosts ten Targets needs ten installs, ten ServiceAccounts, ten enrolment tokens, and ten agents watching the same API server. Every new Target means installing another agent before anything can be observed or deployed there.

Customers running Kubernetes usually have a few clusters, each with many namespaces, so a per-namespace agent's cost grows with exactly the number they have most of. The [Kubernetes agent design](../design/k8s-agent.md) needs one agent to serve a whole cluster.

## Decision

**A runtime agent serves a Cluster, and through it every Target in the Cluster.**

- **Cluster is a delivery object.** It is a Kubernetes cluster an Organization deploys to (prefix `cls`, table `delivery.clusters`), with a slug unique in the Organization, a display name, a `runtime` (`kubernetes`), its agent's workload principal, the capabilities the agent last reported, and the time it last reported. It belongs to the Organization, not to a Workspace, because one cluster usually hosts several Workspaces' Environments.
- **Targets name their Cluster.** A Kubernetes Target's `locator.cluster` must be the slug of an active Cluster in the same Organization. The locator keeps its shape, and a Target is still what Environments, Deployments, placements, and authorization refer to.
- **A Target's agent is its Cluster's agent.** `targets.agent_principal_id` is dropped. Every check that a caller is "the Target's agent" becomes "the agent of the Target's Cluster", in the same places and with the same errors.
- **Enrolment moves to the Cluster.** An enrolment token is created for a Cluster (`POST /v1/clusters/{cluster_id}/enrolments`). Redeeming it sets the Cluster's agent and suspends the previous one, as Target enrolment does today, and the response names the Cluster and the Organization. One credential serves every Target in the Cluster.
- **Agent operations are scoped to the caller's Cluster.** The agent lists its Targets, polls assignments, sends one heartbeat, and reports observations through `/v1/agent/…` operations, instead of calling once per Target. Deployment event reports stay per Deployment.
- **The cluster's operators decide what the agent can reach.** Anyone with `environment.manage` can create a Target for any namespace of a Cluster. What the agent can observe and change there is set by the Kubernetes RBAC its chart grants: read-only observation, cluster-wide or in listed namespaces, and write access only in the namespaces the operators list. The heartbeat reports what the agent can do in each Target's namespace, and the control plane refuses an executed Deployment the agent has said it cannot carry out.
- **Permissions.** `cluster.read` and `cluster.manage` on the Organization. Creating, changing, and archiving a Cluster and creating its enrolment tokens need `cluster.manage`. Creating a Target still needs `environment.manage` on its Environment. The built-in roles pick both up by rule ([ADR 0003](0003-identity-access-and-policy.md)), so IAM does not change.
- **A Cluster cannot be archived while it has active Targets.**

This answers ADR 0006's open question: one agent covers a whole cluster.

## Consequences

- One install per cluster. A new Target is observed and deployable as soon as it is created, provided the agent's RBAC covers its namespace.
- One agent's credential now reaches every Target in its Cluster, across Environments and Workspaces. If it is stolen, everything the agent's RBAC and cloud identity allow is exposed, in staging and production namespaces alike. RBAC still limits writes to the listed namespaces, and a Secret Store's Environment limit and the customer's cloud policy still limit secrets ([deployment variables](../design/deployment-variables.md)). Customers who want production isolated from staging put them in different clusters, which is common practice anyway.
- Re-enrolling or losing a Cluster's agent affects all of its Targets at once. The web app shows the Cluster's agent status on each of its Targets.
- An Environment manager can point a Target at any namespace of a Cluster their Organization has, including one owned by another team. The agent only sees what its RBAC allows, and with cluster-wide read that includes that namespace's workloads and image digests, never its Secrets. Operators who need tighter control list the namespaces the agent may observe.
- Delivery gains an Organization-level object. Every other delivery object belongs to a Workspace or below, so Clusters sit outside the Workspace boundary checks; the Targets in them do not.
- The existing per-Target enrolment, assignment, heartbeat, and agent-assignment operations are replaced, and the [workload enrolment](../design/workload-enrolment.md) design is updated when they are. Only agents call them, and no agent has been released, so nothing deployed breaks.
- `runtime_agent` workload principals do not change; only what they are assigned to does.

## Alternatives considered

- **Keep one agent per Target.** Rejected: installs, credentials, and API-server watches grow with the number of namespaces, and every new Target needs a new install.
- **One agent enrolled for many Targets, with one credential per Target.** Rejected: the agent juggles a credential per Target, the rule that one ServiceAccount names one workload has to be broken or worked around, and the exposure if the agent is compromised is the same as with one credential.
- **Bind agents to clusters by `locator.cluster` alone, without a Cluster object.** Rejected: there would be nowhere to record the agent, its status, and its capabilities, and nothing to grant `cluster.manage` on. Typos in free-text cluster names would silently create Targets no agent serves.
- **Clusters inside a Workspace.** Rejected: clusters are usually shared across Workspaces, and a per-Workspace copy of the same cluster would need its own agent.

## Open questions

- Whether a large cluster needs more than one agent, for example sharded by namespace, and how Targets would be split between them.
- Whether one cluster shared by several Organizations, each with its own agent, needs anything beyond separate ServiceAccounts.
- Whether a VPS agent ([ADR 0013](0013-one-agent-per-runtime.md)) serves a host as a Cluster of one, or needs its own object.
