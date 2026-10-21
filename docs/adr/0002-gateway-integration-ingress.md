# ADR 0002: Gateway as the integration ingress

- Status: Proposed
- Date: 2026-10-01

## Context

Two kinds of external traffic reach Ivaldi apart from users of its own API:

- **Events from providers:** webhooks from VCS and CI systems and other provider callbacks.
- **Tool calls from agents:** Model Context Protocol (MCP) requests to Ivaldi's code-intelligence server from clients such as Claude Code, Codex, OpenCode, IDE plugins, and agent Sessions that Ivaldi itself orchestrates.

Both have authentication needs that differ from human login. Under bring-your-own-compute ([ADR 0001](0001-deployment-topology.md)), a customer's VCS may sit inside their network, and code-intelligence indexes, which contain source code, live in the customer's data plane.

## Decision

The gateway (`ivaldi gateway`, code in `apps/control-plane/internal/gateway`; [ADR 0010](0010-one-binary-for-the-components.md)) is Ivaldi's **integration ingress** for inbound provider events and inbound MCP tool calls. It is not the entry point for the control plane's own user-facing API.

**It verifies identities, never issues them.** The gateway checks credentials and signatures. It does not store identity data, issue tokens, or decide access on its own; it calls the single authorizer described in [ADR 0003](0003-identity-access-and-policy.md).

**It can run in either plane.** The gateway works in the control plane or in a customer's data plane. It verifies tokens offline against the control plane's published signing keys (JWKS) through `pkg/identity`, so it does not call the control plane on every request.

**Webhooks and MCP are separate internal modules** (`internal/webhook` and `internal/mcp`), each with its own authentication. They share a deployable for now, which follows the "capability before service" principle. They can be split apart or scaled independently later.

### Webhooks (`internal/webhook`)

- **Verify the provider signature** using that installation's secret, e.g. GitHub's `X-Hub-Signature-256`, GitLab's token header, or Bitbucket's signatures. Webhook secrets come from the credentials capability ([ADR 0004](0004-credentials-and-keys.md)).
- **Map the event to a tenant.** Resolve the provider's installation, app, or organization ID to an Ivaldi tenant and Source. Reject events that map to no tenant.
- **Protect against replays and duplicates** using delivery IDs and timestamp windows, because providers retry.
- **Normalize and hand off.** Convert provider payloads into Ivaldi events and enqueue them, on a Redis stream the control plane consumes ([ADR 0009](0009-gateway-event-transport.md)). The gateway acknowledges quickly and does not call domain logic synchronously.
- **Private VCS under BYOC:** when the VCS cannot reach Ivaldi's cloud, a gateway running in the customer's data plane receives webhooks and relays them over the runner's outbound connection. Polling is the fallback.

### MCP (`internal/mcp`)

- **The MCP endpoint is an OAuth 2.1 resource server**, following the MCP authorization specification. It publishes protected-resource metadata (RFC 9728) that points clients to the control plane's authorization server.
- **Check the token's audience** using resource indicators (RFC 8707).
- **Never forward the client's token** to the indexer or any other service. The gateway calls internal services with its own workload identity, as the MCP specification requires.
- **Limit every query and result to the Sources the caller may read**, as decided by the authorizer. Agent Sessions see only their Session's repositories.
- **Allow or deny individual tools per caller** through policy (for example, cross-repository search).
- **Include provenance in responses:** every result identifies the repository and the commit or index generation it came from.
- **Record each tool call as an audit event** linked to its Session or caller.
- **Under BYOC, serve MCP inside the customer's data plane** so code does not pass through Ivaldi's cloud.

## Consequences

- Human, API, and CLI authentication is handled by the control plane, not the gateway.
- Because the gateway runs in either plane, it must not depend on control-plane databases or on synchronous calls to the control plane for each request.
- Webhooks are short requests that are acknowledged and queued; MCP uses long-lived streaming connections with synchronous answers and needs rate limits per caller. Keeping them as separate modules allows separate scaling later.

## Alternatives considered

- **Gateway as the front door for all traffic, including human login.** Rejected: it mixes three unrelated authentication models in one place and prevents the gateway from running in the data plane without identity data.
- **MCP served only from Ivaldi's cloud and relayed to customer indexes.** Rejected as the default for BYOC: code would pass through Ivaldi's cloud, removing the guarantee BYOC customers want. It remains possible for managed-cloud tenants.
- **Webhooks delivered straight to the control plane.** Rejected: it does not work for private VCS under BYOC and couples provider-specific handling to domain logic.
