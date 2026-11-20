# ADR 0017: The control plane grants access to Secret Stores

- Status: Proposed
- Date: 2026-11-20
- Amends: [ADR 0004](0004-credentials-and-keys.md) (where provider credentials live), [ADR 0013](0013-one-agent-per-runtime.md) (what a runtime's agent must hold)
- Related: [ADR 0003](0003-identity-access-and-policy.md), [ADR 0008](0008-reaching-customer-systems.md), [ADR 0012](0012-token-issuer-and-signing-keys.md), [ADR 0014](0014-agents-serve-clusters.md), [secret-store access](../design/secret-store-access.md), [Kubernetes agent](../design/k8s-agent.md), [deployment variables](../design/deployment-variables.md)

## Context

A Secret Store tells an agent where an Application's secret variables live. Today the agent authenticates to the store with credentials it holds itself. A Kubernetes RBAC role, EKS Pod Identity or IRSA, and Vault's Kubernetes auth are all identities the cluster gives it. A Doppler service token sits in a Kubernetes Secret in the agent's namespace, and the store's settings name that Secret. Ivaldi holds no credential for any store, and [ADR 0004](0004-credentials-and-keys.md) promises that the managed control plane cannot read a customer's secrets.

That model assumes every reader runs in Kubernetes:

- **Non-Kubernetes agents.** [ADR 0013](0013-one-agent-per-runtime.md) gives each runtime its own agent. An agent on a VM, or on a platform such as Railway or Fly.io, has no ServiceAccount, no projected token, and no Secret for a store's settings to name. Each would need its own way to hold each store's credentials.
- **Store settings describe one runtime.** `token_secret_name` and the default Kubernetes auth mount are written into a Workspace-level object that should not know where its reader runs.
- **Scoping is per cluster, not per Deployment.** One cluster-wide agent with one cloud identity can read everything that identity can read, for every Target in the cluster. Ivaldi's `environment_ids` check and the customer's own policies are the only things separating staging from production ([Kubernetes agent](../design/k8s-agent.md#who-can-read-what)).
- **More stores are coming.** Infisical is next, with its own auth methods. Each new store would add another kind of credential for every agent to hold.

ADR 0004 already describes a credential broker that gives a Session only the credentials its grants and policy allow, for a limited time. Agents need the same thing.

## Decision

**The control plane controls access to Secret Stores. A runtime agent asks it for access to the stores a Deployment uses, and the control plane authorizes the request and grants short-lived, scoped access.** The agent then reads the values from the store itself. Values never pass through or rest in Ivaldi.

### Access modes

Every Secret Store has an access mode. Whoever manages the Workspace's stores chooses it.

- **`federated`: the control plane signs, the store trusts it.** The control plane is an OIDC issuer for Secret Stores. For each grant it signs a short-lived JWT naming the Organization, the store, the Environment, the Target, and the Deployment. The agent exchanges that JWT for a store token through the store's own federation: Doppler's service-account identities, Infisical's OIDC or JWT auth, Vault's JWT auth, or AWS's `AssumeRoleWithWebIdentity`. The customer's store decides what a token with those claims may read. The control plane holds no store credential. The agent, not the control plane, makes the exchange, so this mode works for stores inside the customer's network. A store that cannot fetch the issuer's discovery document can be set up with its keys instead, where the store supports static keys.
- **`brokered`: the control plane holds a credential and mints narrower ones.** The customer gives the store a credential that can create other credentials. For each grant, the control plane uses it to mint one that is read-only, expires within minutes, and is scoped as narrowly as the store allows: an ephemeral Doppler config token, a Vault child token from a token role, an Infisical access token, or AWS session credentials whose session policy lists the Deployment's secrets. The minting credential is encrypted with envelope encryption under ADR 0004's key-management interface. It is write-only through the API and never leaves the control plane. Brokering contacts the store's API from the control plane, so it is only for stores the control plane can reach ([ADR 0008](0008-reaching-customer-systems.md)).
- **`agent`: the agent uses its own identity, as today.** The control plane still authorizes the read, but grants nothing. The agent authenticates with what its runtime gives it, or with a credential its operator configured under a name the store refers to. It never uses a Kubernetes Secret named by the store's settings. `kubernetes` stores are always in this mode, because RBAC is their grant. The mode is also for customers who will not let Ivaldi into their secrets' trust chain.

### Granting

- **Access is granted per Deployment, never per agent.** The agent asks for grants for a Deployment that is applying on a Target in its Cluster. The control plane works out which stores and secrets that Deployment's recorded references need. The agent does not name stores or paths, so it cannot widen the request.
- **Every grant is authorized and audited.** At grant time the control plane checks the Deployment again: it is still applying, the store is active, and the store is still allowed for the Target's Environment. The decision carries a `dcn_` ID, as other agent operations do. Every issuance is an audit record on the Deployment, naming the store, the mode, the expiry, and the provider's ID of anything minted, never the credential (ADR 0004).
- **Grants are short-lived and revoked when done.** A federated JWT lives five minutes. A brokered credential lives as briefly as the store allows, and the control plane revokes it when the Deployment reports `progressing` or `failed`, wherever the store can revoke it.
- **The credentials capability owns grants.** It owns Secret Stores already, and it imports `delivery` to read the Deployment. Delivery does not import it.

### Trust

**The control plane, including the managed cloud, can now open a customer's Secret Stores, for a store in `federated` or `brokered` mode.** This changes ADR 0004's promise. The promise becomes:

- No secret value is stored in Ivaldi or passes through it. Only references, provider versions, and keyed fingerprints do, as before.
- Every access to a store is authorized for a specific Deployment, scoped, short-lived, and audited.
- A customer who does not accept this keeps every store in `agent` mode, where Ivaldi can neither sign nor mint.

The federation issuer is not the API's token issuer ([ADR 0012](0012-token-issuer-and-signing-keys.md)): it has its own path, keys, and audiences. A store that trusts it does not accept API access tokens, and the API does not accept its JWTs. Each store's audience is fixed and unique to it, so a store that checks only the audience still refuses a JWT made for another tenant's store. The customer must still check the subject, and the self-hosting guide says so for each store.

### Infisical

Infisical becomes a Secret Store kind (`infisical`), in all three modes: OIDC or JWT auth when federated, Universal Auth when brokered, and a named credential or Kubernetes auth in `agent` mode.

## Consequences

- An agent on any runtime needs nothing store-specific to read a `federated` or `brokered` store, only its Ivaldi workload credential. A VM or platform agent can read Doppler, Infisical, Vault, and AWS stores without its operator setting up credentials.
- Scoping becomes per Deployment and Environment. Federated claims let a customer write one policy per Environment or Target. Brokered credentials are minted for exactly one Deployment's secrets where the store allows it. This settles the Kubernetes agent's open question about a per-namespace secret identity for these modes.
- The control plane gains three responsibilities:
  - an OIDC issuer with its own keys;
  - the first real use of ADR 0004's key-management interface;
  - minting and revoking credentials in each store's API.

  Each store kind's brokering code runs with credentials that can create credentials, so it needs the same care as the agent's masking.
- A compromised control plane could grant itself access to every store in `federated` or `brokered` mode. On the managed cloud that spans tenants. Keeping the issuer's keys and the key-encryption keys out of the database, rotating them like signing keys, and auditing every grant limit this but do not remove it.
- Grants add a round trip before each apply and depend on the control plane being reachable then. The agent already depends on it to receive the assignment.
- Existing Doppler stores, which name a token Secret, move to `agent` mode with a named credential. The agent chart's `secrets.dopplerTokenSecrets` becomes a general `secrets.credentials` map.

## Alternatives considered

- **Agents keep their own identities everywhere (`agent` mode only).** Rejected as the only mode. Every runtime's agent would need its own way to hold each store's credentials, and access would stay scoped per cluster rather than per Deployment. It remains available.
- **The control plane reads the values and sends them to the agent.** Rejected. Values would pass through Ivaldi, which ADR 0004 still rules out. The control plane would also need to reach every store, including stores inside customer networks, which ADR 0008 rules out.
- **Federated only.** Rejected. Some stores cannot fetch a discovery document or take static keys. Doppler identities need its Team or Enterprise plan. Infisical and Doppler scope per identity, so a Deployment-scoped credential needs brokering.
- **Brokered only.** Rejected. It puts a credential that can create credentials in the control plane for every store, and it cannot reach stores inside a customer's network.
- **The control plane exchanges federated JWTs itself and hands the agent the store's token.** Rejected for federated mode. It would need the control plane to reach the store, and that need is what separates brokered from federated.

## Open questions

- Whether a grant for a production Environment should pass a policy rule of its own, beyond the Deployment's approval.
- Whether Sessions use the same grants when the credential broker for Sessions is built.
