# ADR 0003: Identity and access management, and its boundary with policy

- Status: Proposed
- Date: 2026-10-01
- Amended by: [ADR 0007](0007-teams-and-agent-identity.md), which adds durable Agent identities and Sessions without a human delegator

## Context

Ivaldi must know who is acting and decide what they may do. The actors are:

- **Humans** using the web UI, CLI, or API.
- **Agent Sessions** that act on behalf of a human with a narrower scope.
- **Data-plane runners and services** (worker, indexer, runtime agent, gateway).
- **External MCP clients** used by developers.

Self-hosted installations must work without depending on any external SaaS. Managed-cloud tenants each bring their own identity provider. See [ADR 0001](0001-deployment-topology.md).

Ivaldi also has to govern the work itself: which tools and network destinations a Session may use, and which actions need human approval. These governance questions are different from access questions.

## Decision

### IAM owns identity and access

The IAM capability lives in `apps/control-plane/internal/iam`. It owns two things.

**Identity:**
- the actors: users, service accounts, runners, and agent Sessions
- tenant membership
- each tenant's identity-provider configuration
- issuing tokens

**Access authorization:**
- roles, grants, and membership
- answering questions such as "can this user read this Source", "can this account administer this Workspace", and "can this Session query code intelligence for repository X"

IAM exposes this as a single decision, conceptually `Authorize(actor, action, resource)`.

### Policy governs the work

Policy answers governance questions about what work may happen and under what conditions, for example:

- May this Session run this command or use this tool?
- May it reach this network destination?
- Does this deployment need a human approval?
- May an agent merge without review?

Policy is separate from IAM because:

- **Its outcomes go beyond allow and deny.** A rule can return allow, deny, requires approval, or allow with constraints, which connects to human review, steering, and intervention.
- **It depends on context:** environment, Work Item, the risk of a Change, and what the Session has already done.
- **Organizations write it.** Policies are governance rules authored by customers, not grants.

Policy storage, versioning, and approval workflows live in the control plane. `pkg/policy` holds only the evaluation engine, so the data plane can enforce policy locally.

### One enforcement call

Callers do not choose between IAM and policy. A single authorizer first checks IAM (may this actor touch this resource at all?) and then policy (should this action happen now, under these conditions?). The gateway's MCP handler, the worker's tool dispatcher, and the control-plane API all make that one call.

An agent Session's effective permissions are the intersection of:

- the delegating human's access, narrowed to the Session's scope, and
- what the organization's policy allows that work to do.

### Authentication and token verification

**Humans** authenticate through the control-plane API against their tenant's identity provider:

- OIDC or SAML for sign-in, plus SCIM for provisioning in the managed cloud.
- Self-hosted installations support Google and GitHub sign-in, which most of them are expected to use, any OIDC provider (e.g. Keycloak, Dex, Entra ID, Okta), and a basic built-in account option for small installations.
- Google is used through OIDC. GitHub's OAuth sign-in is not OIDC: it issues no ID token, so the user is read from GitHub's API, identified by the stable numeric user ID rather than the login name, which can change, and their email is accepted only if GitHub marks it verified.
- **A self-hosted installation's sign-in providers come from its configuration.** The operator registers an OAuth app with each provider they want, and gives the control plane its client ID and secret. The control plane applies them on every start as identity providers of the installation's Organization, so the configuration stays their source of truth: they cannot be edited through the API, and removing one disables it. In the managed cloud, each Organization configures its own identity providers in IAM.
- **A public provider must be restricted.** Any Google or GitHub account can authenticate, so an identity provider of either kind must name who may sign in: Google Workspace domains (the ID token's `hd` claim), GitHub organizations the user belongs to, or individual email addresses. One without any restriction is refused.
- The identity provider sits behind an Ivaldi-owned interface. No single auth vendor is hard-wired.

**The control plane is the OAuth 2.1 authorization server** for MCP clients and other API consumers. It delegates the human login step to the tenant's identity provider.

**Agent Sessions** receive short-lived credentials issued by the control plane. Each credential is scoped to the Session and linked to its Work Item and its delegating human. Sessions never reuse a human's token. This linkage is what lets every Change be traced back to a Session and a person.

**Runners and services** authenticate with workload identity after enrollment ([ADR 0001](0001-deployment-topology.md)).

**`pkg/identity`** provides shared, offline token verification: claims types, a verifier interface, signature checks against JWKS, and audience checks. The control plane, the gateway, and data-plane runners use it. It only verifies credentials; it holds no identity data and makes no access decisions.

### Summary

| Concern | Owner |
|---|---|
| Actors, tenancy, identity-provider configuration, issuing tokens | IAM (`control-plane/internal/iam`) |
| Roles, grants, access checks | IAM |
| Offline token verification | `pkg/identity` |
| Governance: tool, network, approval, and environment rules | Policy (control plane; evaluation engine in `pkg/policy`) |
| The single enforcement call that combines both | Authorizer used by every entry point |

## Consequences

- Entry points stay simple and consistent because they make one authorization call.
- IAM and policy can change independently: grant models and governance rules have different authors, lifecycles, and audit needs.
- Data-plane components can verify tokens and evaluate policy locally, which supports bring-your-own-compute and keeps request paths free of calls back to the control plane.
- Short-lived Session credentials require reliable issuance and rotation in the control plane.
- Revoking access takes effect within the lifetime of a short-lived token unless a revocation check is added; token lifetimes must be chosen with that in mind.

## Alternatives considered

- **Policy handles all authorization, and IAM only provides identity.** Rejected: access grants are a conventional IAM concern, and mixing them with organization-authored governance rules makes both harder to reason about and audit.
- **IAM absorbs governance policy.** Rejected: governance needs outcomes beyond allow and deny, depends on rich context, and is authored by organizations, which does not fit a grant model.
- **Agent Sessions reuse the delegating human's token.** Rejected: it breaks least privilege and loses the Session-level provenance.
- **IAM as a separate service from the start.** Rejected for now: IAM data is closely tied to Workspaces and Projects in the control plane. A clean `internal/iam` boundary keeps extraction possible later.

## Open questions

- Which policy language or engine to use (e.g. OPA/Rego, Cedar, or an Ivaldi-defined rule format).
- Default lifetimes for Session credentials, and whether a revocation check is needed for high-risk actions.
