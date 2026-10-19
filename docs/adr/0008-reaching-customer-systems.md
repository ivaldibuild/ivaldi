# ADR 0008: Reaching systems inside customer networks

- Status: Proposed
- Date: 2026-10-18

## Context

Ivaldi needs to query systems its customers run: version control (to list commits between two Releases), artifact registries (to resolve a tag to a digest), and later CI, issue trackers, and observability tools. Under bring-your-own-compute and self-hosting ([ADR 0001](0001-deployment-topology.md)), many of these sit inside the customer's network. The control plane, running in Ivaldi's cloud, cannot reach them, and customers will not open inbound ports for it. Their credentials should also stay in the customer's environment ([ADR 0004](0004-credentials-and-keys.md)).

[ADR 0006](0006-release-and-delivery-tracking.md) already solved this for Registries. VCS Connections ([version-control data model](../design/version-control-data-model.md)) have the same problem, and more integrations will follow. Solving it separately for each one would produce several slightly different mechanisms.

## Decision

**Every integration record that Ivaldi queries says where its lookups run.** The record carries:

- `lookup_from`: `control_plane` or `data_plane`;
- `lookup_pool`: when `lookup_from` is `data_plane`, the pool of data-plane workloads that run its lookups.

**Data-plane lookups run as small jobs on a data-plane workload.** The control plane sends the request over the workload's existing outbound connection, as it sends any other work. The workload resolves the credential from the customer's own secret store, calls the system, and returns the result. The raw credential never leaves the customer's environment.

**The control plane never contacts a system marked `data_plane`.** If no workload in the pool is online, the lookup fails and says so. It does not fall back to a direct call.

**Lookups return only what the caller needs.** For example, a commit comparison returns commit IDs, authors, and messages, not file contents. Anything that would move source code into Ivaldi's cloud needs its own decision and policy.

**Each capability owns its integration records.** This ADR sets the rule, not a shared table. The delivery capability keeps `lookup_from` on Registries, and the version-control capability keeps it on Connections. The lookup job itself is defined once, as a contract in `schemas/`, so every capability sends lookups the same way.

## Consequences

- One mechanism covers every customer system Ivaldi queries. New integrations add a provider, not a new transport.
- Data-plane lookups are slower than direct calls, and they depend on a workload being online. Results that do not change, such as the commits between two fixed SHAs, can be cached by the capability that asked for them.
- Managed-cloud customers whose systems are public can use `control_plane` lookups and run no extra workload.
- Lookup jobs must be authorized like any other work assignment, and every lookup is recorded as an audit event linked to whoever asked.

## Alternatives considered

- **Require customers to allow inbound access from Ivaldi's cloud.** Rejected: customers generally refuse, and it breaks the outward-only rule in [ADR 0001](0001-deployment-topology.md).
- **A general network tunnel from the control plane into the customer's network.** Rejected: it gives the control plane broad network reach into the customer's environment, against least privilege. Narrow, typed lookups expose only what each one needs.
- **Decide per integration.** Rejected: each integration would grow its own mechanism, and the security review would have to be repeated for each.

## Open questions

- Whether lookup workloads are a dedicated pool, or whether any enrolled worker can run them.
- Which lookups may be cached in the control plane, and for how long, given that their results may include customer data such as commit messages.
