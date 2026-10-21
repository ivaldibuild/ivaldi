# Architecture Decision Records

Consequential architecture decisions, one per file. Each record states its context, the decision, its consequences, and the alternatives considered. A record is `Proposed` until the team accepts it; superseded records stay in place and link to their replacement.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-deployment-topology.md) | Deployment topology: control plane and data plane | Proposed |
| [0002](0002-gateway-integration-ingress.md) | Gateway as the integration ingress | Proposed |
| [0003](0003-identity-access-and-policy.md) | Identity and access management, and its boundary with policy | Proposed |
| [0004](0004-credentials-and-keys.md) | Credentials and customer-managed keys | Proposed |
| [0005](0005-data-model-ownership.md) | Data model ownership and placement | Proposed |
| [0006](0006-release-and-delivery-tracking.md) | Release and delivery tracking | Proposed |
| [0007](0007-teams-and-agent-identity.md) | Teams and durable Agent identity | Proposed |
| [0008](0008-reaching-customer-systems.md) | Reaching systems inside customer networks | Proposed |
| [0009](0009-gateway-event-transport.md) | Redis Streams carry events from the gateway to the control plane | Proposed |
| [0010](0010-one-binary-for-the-components.md) | One binary runs the control plane, gateway, worker, and indexer | Proposed |

New records take the next number and use the same section headings.
