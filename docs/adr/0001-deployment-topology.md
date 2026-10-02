# ADR 0001: Deployment topology: control plane and data plane

- Status: Proposed
- Date: 2026-10-01

## Context

Ivaldi will ship in three ways:

1. **Self-hosted:** an organization runs all of Ivaldi in its own environment.
2. **Managed cloud:** Ivaldi runs everything for the customer.
3. **Managed cloud with bring-your-own-compute (BYOC):** Ivaldi runs orchestration, while execution, indexing, and access to source code happen on the customer's infrastructure.

Customers who choose self-hosting or BYOC usually do so because source code, secrets, and network access must stay inside their environment. Customers generally will not open inbound firewall ports to a vendor, and a vendor should not hold standing access to customer networks.

Maintaining separate editions for each mode would split the codebase and let the modes drift apart.

## Decision

Split Ivaldi into two planes, and ship one codebase with deployment profiles instead of separate editions.

**Control plane:** `apps/control-plane`. It owns orchestration state, Work Items, Sessions, identity and access, policy, credential references, and provenance records. In the managed cloud, Ivaldi operates it.

**Data plane:** `apps/worker`, `apps/indexer`, sandboxes, and `apps/helm-agent`. It runs where the code and compute live. It holds source checkouts, code-intelligence indexes, and execution environments.

`apps/gateway` can run in either plane (see [ADR 0002](0002-gateway-integration-ingress.md)).

| Mode | Control plane | Data plane |
|---|---|---|
| Self-hosted | Customer | Customer |
| Managed cloud | Ivaldi | Ivaldi |
| Managed cloud + BYOC | Ivaldi | Customer |

Rules that follow from the split:

- **Connections go outward from the data plane.** Data-plane runners open the connection to the control plane and receive work over it. The control plane never needs inbound access to a customer network.
- **Runners enroll, then use workload identity.** A runner registers with a one-time enrollment token. After that it authenticates with a rotating workload credential (mTLS, or a projected service-account token on Kubernetes). Shared static secrets are not used. See [ADR 0003](0003-identity-access-and-policy.md).
- **Tenancy is built in from the start.** The managed cloud is multi-tenant, so every tenant-scoped record, query, and policy decision carries a tenant identifier. A self-hosted installation is the same system with a single tenant.
- **The deployment mode is configuration.** Mode differences are expressed through configuration and provider implementations behind Ivaldi-owned interfaces, never through `if cloud` branches in domain code.
- **The data plane has no control-plane database access.** Runners talk to the control plane through its API only.

## Consequences

- Code and secrets can stay in the customer's environment under BYOC; the control plane stores metadata, references, and provenance.
- Every control-plane-to-runner interaction must work over a connection the runner opened, which rules out designs where the control plane calls into runners directly.
- Self-hosted and cloud share one code path, so self-hosted users get the same features and the modes do not drift.
- Multi-tenancy adds a cost to every table and query from day one; this is accepted because retrofitting tenancy later costs far more.
- Contracts between the planes (work assignments, events, results) must be versioned, because the customer may run a different data-plane version than the cloud control plane.

## Alternatives considered

- **Separate self-hosted and cloud editions.** Rejected: the editions would diverge and every feature would be built twice.
- **Control plane connects inbound to customer runners.** Rejected: it requires customers to open firewall ports and gives the vendor standing network access.
- **Single-tenant architecture, with multi-tenancy added later.** Rejected: retrofitting tenant scoping across all data and authorization is expensive and error-prone.

## Open questions

- Which existing domain term is the tenant boundary: is a Workspace the tenant, or is a new Organization introduced above Workspace?
- Which transport carries the runner channel (gRPC streams, WebSocket, or something else)?
