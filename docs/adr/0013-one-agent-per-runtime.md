# ADR 0013: One agent per runtime, Kubernetes first

- Status: Proposed; agents serve Clusters rather than single Targets since [ADR 0014](0014-agents-serve-clusters.md); agents get access to Secret Stores from the control plane since [ADR 0017](0017-control-plane-grants-secret-store-access.md)
- Date: 2026-11-08
- Related: [ADR 0001](0001-deployment-topology.md), [ADR 0003](0003-identity-access-and-policy.md), [ADR 0006](0006-release-and-delivery-tracking.md), [ADR 0008](0008-reaching-customer-systems.md), [ADR 0010](0010-one-binary-for-the-components.md), [workload enrolment](../design/workload-enrolment.md)

## Context

[ADR 0006](0006-release-and-delivery-tracking.md) planned one runtime agent, `apps/runtime-agent`, with runtimes behind an Ivaldi-owned interface inside it: Kubernetes as the first provider, and each future runtime as another provider in the same binary. [ADR 0010](0010-one-binary-for-the-components.md) kept that agent a binary of its own.

This version of Ivaldi targets Kubernetes. The agent is being built for it: `apps/k8s-agent` builds `ivaldi-k8s-agent`, and `deploy/helm/ivaldi-agent` installs it into a cluster. It authenticates with a projected service-account token, reads and changes workloads through the Kubernetes API, and is scoped by RBAC.

A VPS, meaning a plain virtual or physical host with no orchestrator, shares almost none of that. Its agent runs as a system service rather than as a Pod. It applies a Release through the host's container engine or service manager rather than the Kubernetes API, it is limited by operating-system permissions rather than RBAC, and it authenticates with an Ivaldi workload token or a client certificate rather than a service-account token. One binary covering both would bring the Kubernetes client libraries onto every host, and host-management code into every cluster, where the agent already has the widest access of any Ivaldi component.

## Decision

**Each runtime gets its own agent binary.** The Kubernetes agent is `apps/k8s-agent` (`ivaldi-k8s-agent`), and it supports only Kubernetes. Deploying to a VPS needs a new agent, such as `apps/vps-agent`, rather than a provider added to the Kubernetes agent. That agent is out of scope for this version.

This replaces ADR 0006's single agent with runtime providers, and ADR 0010's `apps/runtime-agent`. The rest of those records still holds:

- **The domain stays runtime-neutral.** A Target's `runtime` names its runtime and `locator` holds that runtime's address; `kubernetes` is the only runtime the control plane accepts for now. A VPS runtime adds a `Runtime` value and its locator keys, not a new model.
- **Every agent is a `runtime_agent` workload.** All of them enrol for one Target, connect out to the control plane ([ADR 0008](0008-reaching-customer-systems.md)), and report only on their own Target. The Target's `runtime` says which agent serves it. The enrolment methods stay as they are: `kubernetes` for in-cluster agents, and `bearer` and `mtls` for agents without a cluster identity.
- **The agents share one protocol.** Enrolment, renewal, heartbeats, assignments, secret-store grants ([ADR 0017](0017-control-plane-grants-secret-store-access.md)), and reports are the same for every runtime, and so are the contracts in `schemas/` and the status rules in `pkg/release`. Client code for that protocol goes in `pkg/` when a second agent needs it, so a runtime's agent holds only what differs: how it observes and applies.
- **Inside the Kubernetes agent, deploy tools are still not separate agents.** Helm, plain manifests, and observing external deployers such as Argo CD or Flux are ways the Kubernetes agent works, as ADR 0006 decided.

## Consequences

- Each agent carries only the code and dependencies of its runtime, so what runs with wide access on a customer's cluster or host stays small.
- Each runtime is another binary to build, scan, document, and release. `make build` builds `ivaldi`, `ivaldictl`, and `ivaldi-k8s-agent`.
- A customer who deploys to both Kubernetes and VPS hosts runs two kinds of agent. Each enrols for its own Target, as it would have anyway.
- A change to the shared protocol has to reach every agent. Versioned contracts in `schemas/` already allow agents of different versions to coexist ([ADR 0006](0006-release-and-delivery-tracking.md)).
- Until a VPS agent exists, Ivaldi neither observes nor deploys software running outside Kubernetes.

## Alternatives considered

- **One agent with runtime providers, as ADR 0006 planned.** Rejected: the runtimes share the protocol but not how they are installed, how they authenticate, what they are allowed to do, or what libraries they need. Splitting by runtime keeps each agent's footprint and access matched to where it runs.
- **Deploy to a VPS from the control plane over SSH.** Rejected: [ADR 0008](0008-reaching-customer-systems.md) has the control plane never reach into a customer's systems, and it would put host credentials in the control plane.
- **Require VPS users to run a small Kubernetes distribution, such as k3s, and use the Kubernetes agent.** Not adopted as the answer, but it works today for anyone who accepts it.

## Open questions

- What a VPS Target's locator holds, such as a host name or a host group.
- How a VPS agent applies a Release: containers through Docker or Podman, Compose files, systemd units, archives, or several of these.
- Whether one VPS agent serves one host, or one agent serves a group of hosts as one Target.
- When the protocol client moves into `pkg/`: now, from the Kubernetes agent, or when a second agent needs it.
