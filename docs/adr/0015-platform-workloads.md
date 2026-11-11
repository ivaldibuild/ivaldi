# ADR 0015: Platform workloads

- Status: Proposed
- Date: 2026-11-10
- Related: [ADR 0001](0001-deployment-topology.md), [ADR 0002](0002-gateway-integration-ingress.md), [ADR 0003](0003-identity-access-and-policy.md), [ADR 0010](0010-one-binary-for-the-components.md), [ADR 0012](0012-token-issuer-and-signing-keys.md), [IAM data model](../design/iam-data-model.md), [workload enrolment](../design/workload-enrolment.md)

## Context

Some Ivaldi components serve every Organization at once. The gateway is the first: one GitHub App delivers every Organization's webhooks to one URL, and the gateway resolves each delivery's platform-wide route key through the control plane before it knows which Organization the delivery belongs to ([ADR 0002](0002-gateway-integration-ingress.md)). The gateway will also front MCP for Ivaldi's own agent Sessions and for coding agents outside Ivaldi, and ADR 0002 says it calls internal services with its own workload identity. Shared runners in the managed cloud, an open question in the IAM data model, are the next case.

Today the gateway authenticates to the control plane with a token both sides read from a file. Nothing in IAM knows it: the token names no principal, cannot be suspended or audited as an actor, and is a shared secret that both sides must hold.

IAM's workload identity does not fit either. Every principal belongs to one Organization (`principals.organization_id` is required), and workloads enrol through an Organization's Target. A component that serves all Organizations has no Organization to belong to.

## Decision

**A platform workload is a workload principal that belongs to no Organization.** It is a `workload` principal like a runtime agent, with a component (`gateway` first) and the same statuses, but its `organization_id` is NULL. Only workloads may be platform principals: `principals` gets a check that `organization_id` is set unless the kind is `workload`, and `workloads` and `workload_credentials` allow NULL too. The domain makes the scope explicit, with a platform flag rather than an empty Organization ID, so a missing Organization is never mistaken for a platform one.

Tenant isolation holds by construction: every Organization-scoped query filters on `organization_id = ?`, which never matches NULL, so a platform workload never appears in an Organization's lists, lookups, or access checks.

**Platform workloads are declared in the control plane's configuration and applied at start.** `platform.workloads` lists each one: a name, its component, and its credentials. Bootstrap applies them under its advisory lock, the way it applies the first-party OAuth clients: it creates a missing workload, adds new credentials, revokes credentials no longer listed, and suspends a workload removed from the configuration. The configuration is the source of truth; there is no API or enrolment token for platform workloads, because only the operator who runs the control plane runs them. Changes record platform-scoped events in the outbox (its `organization_id` is already nullable) and no audit record, as for OAuth clients, because audit records belong to an Organization.

**They authenticate with a key pair: a new `public_key` credential kind.** The workload keeps a private key (Ed25519 or ECDSA P-256) and the control plane keeps only the public key, named by its JWK thumbprint (RFC 7638). For each request, the workload signs a short JWT (RFC 7523 style): `kid` is the thumbprint, `aud` names the service it calls (`urn:ivaldi:control-plane` for the control plane), and it expires within five minutes. The audience is a fixed name rather than the control plane's external URL, because a component usually reaches the control plane at an internal address; keys are per installation, so an assertion cannot be replayed at another one. It sends it as a bearer token with `X-Ivaldi-Credential: workload-key`, as Kubernetes tokens are sent today. The workload verifier finds the active credential by thumbprint, checks the signature, audience, and lifetime, and yields a workload caller with no Organization. No secret is shared: rotating a key means listing the new public key, switching the workload to the new private key, and removing the old one. A Kubernetes service-account credential, which already exists for Organization workloads, can be added for platform workloads later without changing this model.

Signed requests are not tracked by `jti`: a captured request could be replayed for at most five minutes, over connections that are TLS between Ivaldi's own components in production. This is accepted rather than adding a replay store.

**IAM decides what a platform workload may do from its component, through the single authorizer.** Platform workloads hold no grants, since grants are on an Organization's resources. IAM keeps a fixed list of the permissions each component holds platform-wide; the gateway holds `version_control.webhook_route.read`. A platform request is an authorization request with no Organization, on the resource `platform`; the authorizer allows it only for an active platform workload whose component holds the permission, and denies every other actor. Organization requests are unchanged, and a platform workload asking about an Organization's resource is denied, because it has no grants there. Decisions get `dcn_` IDs as before.

**The gateway signs with its own key.** `gateway.identity.private_key_file` is a gateway-wide setting, because MCP will use the same identity. It replaces `gateway.webhooks.route_lookup_token_file` on both sides: the control plane's route lookup authenticates the caller through the API's verifier chain and asks the authorizer, instead of comparing a token.

A gateway in a customer's data plane under BYOC belongs to that customer's Organization and enrols as an ordinary Organization workload. That path is not part of this decision.

## Consequences

- A platform component has an identity IAM knows: an operator can suspend it by removing it from the configuration, its credentials can be listed and revoked, and its requests carry its principal ID.
- No secret is shared between the gateway and the control plane. The gateway's private key never leaves it.
- Code that loads a workload by Organization must handle the platform case: the workload verifier, the workload repository, and the authorizer get explicit platform paths. Organization-scoped code needs no change.
- Platform workload changes are visible in the outbox but not in any Organization's audit log. A platform audit log, if one is needed, is a separate decision.
- Adding a platform component means adding it to IAM's component list and its permissions, a deliberate code change.

## Alternatives considered

- **A gateway key pair outside IAM.** The control plane trusts a configured public key directly. Simpler, but the identity would be invisible to IAM, the authorizer, and provenance, and each later platform component would need its own trust setting.
- **An Organization-scoped workload for the gateway.** Reuses enrolment as it is, but its lookups would be limited to one Organization, so a shared gateway could not route webhooks for the others.
- **A reserved "platform" Organization that owns platform principals.** Avoids nullable columns, but every Organization listing, onboarding step, and tenant check would have to know to skip it, and a mistake would expose a platform principal as a tenant member.
- **Separate tables for platform principals.** Keeps tenant tables untouched, but duplicates workload credentials and their verification, and platform principals could not be referred to like other principals.
- **Bearer tokens issued at bootstrap.** Workload tokens (`iwt_`) work for Organization workloads, but a platform workload has no enrolment step to receive one, so its token would have to be shared through configuration, the problem this replaces.
