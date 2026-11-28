# Secret-store access

- Status: Draft for review
- Date: 2026-11-20
- Related: [ADR 0017](../adr/0017-control-plane-grants-secret-store-access.md), [ADR 0004](../adr/0004-credentials-and-keys.md), [ADR 0008](../adr/0008-reaching-customer-systems.md), [ADR 0012](../adr/0012-token-issuer-and-signing-keys.md), [ADR 0013](../adr/0013-one-agent-per-runtime.md), [Kubernetes agent](k8s-agent.md), [deployment variables](deployment-variables.md), [Secret Stores (self-hosting)](../self-hosting/secret-stores.md)

This document designs how a runtime agent gets access to the Secret Stores a Deployment reads, under [ADR 0017](../adr/0017-control-plane-grants-secret-store-access.md). The control plane decides access, and grants it in one of three modes: `federated`, `brokered`, or `agent`. It also adds Infisical as a store kind.

What does not change: Ivaldi still keeps only references to secrets ([deployment variables](deployment-variables.md)), the agent still reads the values, fingerprints them, masks them, and delivers them to the chart ([Kubernetes agent](k8s-agent.md#variables-and-secrets)), and no value is ever stored in or passes through the control plane.

## Decisions this design is built on

- **A store's settings say where it is, and its access says how it is opened.** Location (address, project, config, region, mount) is the same whoever reads the store. Access is the mode and that mode's settings. Neither ever names where a credential is kept on a reader's machine.
- **Grants are per Deployment.** The agent names a Deployment; the control plane works out which stores it needs, and grants only those.
- **Federated exchanges happen at the agent, brokered minting at the control plane.** So federated works for stores the control plane cannot reach, and brokered works for stores that cannot reach the control plane.
- **Store-side code that runs at the agent lives in `pkg/secretstore`; code that signs or mints lives in the control plane.** No agent ever holds a credential that can create credentials.

## Store settings

A Secret Store keeps `kind`, `settings` (its location), and `access` (its mode and that mode's settings). `credentials.secret_stores` gains an `access` JSONB column holding both; `settings` loses everything about authentication.

| Kind | Location settings |
|---|---|
| `kubernetes` | none: Secrets in the Target's namespace |
| `aws_secrets_manager` | `region` |
| `vault` | `address`, KV v2 `mount`, optional Enterprise `namespace` |
| `doppler` | `project`, `config` (optional in `agent` mode with a service token, which implies them) |
| `infisical` | `site_url` (default `https://app.infisical.com`), `project_id`, `environment` (slug), `secret_path` (default `/`) |

| Kind | `federated` | `brokered` | `agent` |
|---|---|---|---|
| `kubernetes` | — | — | nothing; RBAC is the grant |
| `aws_secrets_manager` | `role_arn` | `role_arn` | optional `role_arn`, assumed on top of the agent's own credentials |
| `vault` | `role`, `auth_mount` (default `jwt`) | `token_role`, `auth_mount` (default `approle`) | `role`, `auth_mount` (default `kubernetes`) |
| `doppler` | `identity_id` | nothing | `credential` |
| `infisical` | `identity_id`, `method` (`oidc` or `jwt`) | nothing | `method` (`kubernetes` with `identity_id`, or `universal_auth` with `credential`) |

`credential` is a name, such as `doppler-production`, that the agent's operator maps to wherever the agent keeps that credential ([Agent mode](#agent-mode)).

Changing a store's kind is still refused. Changing its mode, or any location setting a credential is sent to (`address`, `site_url`, `region`, `project`, `config`, `project_id`), deletes a brokered store's minting credential, so it must be given again. Otherwise someone who manages stores could point a store at their own server and have the control plane send it the credential.

## Granting

### The request

```
POST /v1/agent/deployments/{deployment_id}/secret-grants
```

The agent sends no body. The control plane:

1. checks the caller is the agent of the Cluster of the Deployment's Target (`delivery.ErrNotClusterAgent` otherwise, as other agent operations);
2. checks the Deployment is `applying`;
3. works out the Deployment's secrets the way its assignment does, and from them the stores in `federated` or `brokered` mode;
4. checks each store again: active, and allowed for the Target's Environment;
5. grants each store, records a grant and an audit record for each, and answers.

```json
{
  "grants": [
    { "store_id": "sst_…", "mode": "federated", "expires_at": "…", "jwt": "eyJ…" },
    { "store_id": "sst_…", "mode": "brokered",  "expires_at": "…",
      "credential": { "token": "dp.st.…" } }
  ]
}
```

A brokered credential's fields depend on the kind: `token` for Doppler, Vault, and Infisical, and `access_key_id`, `secret_access_key`, and `session_token` for AWS.

| Failure | Answer | The agent |
|---|---|---|
| Not the Cluster's agent | 403 | as for any agent operation |
| Deployment not applying | 409 | drops the assignment |
| A store archived, or no longer allowed for the Environment | 422, naming the store | fails the Deployment |
| A store refused the minting credential | 422, naming the store | fails the Deployment |
| A store unreachable or throttling while minting | 503 | retries with backoff, as for a store read |

The response is all-or-nothing: if one store cannot be granted, nothing is, and what was already minted in that call is revoked.

The decision carries a `dcn_` ID and the reason "the cluster's agent asked for the secret stores its applying Deployment reads", as other agent operations do (`delivery/agentapi`). Granting is the credentials capability's: `credentials/grants` serves the operation and imports `delivery` to read the Deployment, its Target, and its secrets.

### Grants and audit

A grant is a record, `credentials.secret_store_grants` (prefix `ssg`): the store, the Deployment, the mode, `expires_at`, `revoked_at`, and for a brokered grant the provider's ID of what was minted (a Doppler token slug, a Vault token accessor, an Infisical token ID, or the AWS session name). It never holds the credential.

Each grant is an audit record (`secret_store.access_granted`) by the agent's workload principal, naming the Deployment, the store, the mode, and the expiry. Grants raise no domain event.

### Revocation

A handler on the outbox stream, with its own consumer group as `notifications` has, revokes a Deployment's brokered grants when the Deployment leaves `applying`: Doppler deletes the token, Vault revokes by accessor, and Infisical revokes the access token. AWS sessions cannot be revoked one by one and expire after 15 minutes, the shortest AWS allows. A federated JWT lives five minutes and is not revoked. The store's token the agent got for it lives as long as the customer's store configuration says.

### Lifetimes

| | Lifetime |
|---|---|
| Federated JWT | 5 minutes |
| Brokered Doppler, Vault, or Infisical credential | 15 minutes, or the store's shortest if longer, revoked when the Deployment leaves `applying` |
| Brokered AWS session | 15 minutes |

An apply resolves its secrets before anything is applied, so a grant only has to last for the reads. An agent that restarts mid-apply asks again; one that finds the revision already committed does not read secrets, so it does not ask ([Kubernetes agent](k8s-agent.md#restarting-mid-apply)).

## Federated

### The issuer

The control plane is an OIDC issuer for Secret Stores, separate from the API's token issuer ([ADR 0012](../adr/0012-token-issuer-and-signing-keys.md)):

- Issuer: `<external URL>/federation`.
- Discovery: `<external URL>/federation/.well-known/openid-configuration`, the path OIDC Discovery appends to an issuer with a path.
- Keys: `<external URL>/federation/jwks.json`.
- Signing keys: `secret_stores.federation.signing_key_files`, PEM private keys as for `auth.signing_key_files`. The first signs and all are published, so a key is rotated by putting a new one first and removing the old one five minutes later. It must not share a key with `auth.signing_key_files`.

Without federation keys, a store cannot be set to `federated` ("this installation has no federation signing keys"), and a federated store already saved fails the deploy plan's `secret_stores_allowed` check.

AWS and Doppler fetch discovery documents over the internet, so a federated store there needs a control plane with a public HTTPS external URL. Vault's JWT auth (`jwt_validation_pubkeys`) and Infisical's JWT auth take the keys directly, for a control plane they cannot reach. Then the customer updates the store's configuration when a key is rotated, and the self-hosting guide says so.

### The JWT

| Claim | Value |
|---|---|
| `iss` | `<external URL>/federation` |
| `sub` | `org:<organization ID>:store:<store ID>:env:<environment ID>` |
| `aud` | `ivaldi:<store ID>`, fixed and unique to the store |
| `exp`, `iat`, `nbf`, `jti` | five minutes |
| `org`, `workspace`, `project`, `application`, `environment`, `target`, `cluster`, `deployment`, `release` | their IDs |
| `environment_tier` | the Environment's tier, such as `production` |

The subject uses IDs, not slugs, so renaming an Environment never makes a JWT match another policy. The subject carries what a store that matches only the subject (AWS, Doppler) needs to separate Environments; stores with bound claims (Vault, Infisical) can also match the Target, Application, or tier.

The audience is unique per store and cannot be changed. One issuer serves every Organization on the managed cloud, so a customer whose store checked only a shared audience would accept any tenant's JWT. A per-store audience closes that, and the self-hosting guide still has every store check the subject.

### Exchanges, at the agent

| Kind | Exchange |
|---|---|
| `aws_secrets_manager` | `AssumeRoleWithWebIdentity` with `role_arn` and session name `ivaldi-<deployment ID>`. The customer registers the issuer as an IAM OIDC provider with the store's audience as a client ID, and conditions the role's trust policy on `sub`. |
| `vault` | `POST /v1/auth/<auth_mount>/login` with `role` and the JWT, to an address the agent's operator allowed. The role binds the audience and the claims it needs. |
| `doppler` | `POST /v3/auth/oidc` with `identity_id` and the JWT, giving a service-account token. The identity matches the audience and subject. Needs Doppler's Team or Enterprise plan. |
| `infisical` | `POST /api/v1/auth/oidc-auth/login` or `/api/v1/auth/jwt-auth/login` with `identity_id` and the JWT, giving an access token. |

The agent sends the JWT only to the store's own address. As for `agent` mode today, an agent logs in only at addresses its operator allowed: Doppler's API, Infisical Cloud (`app.infisical.com`, `eu.infisical.com`), and AWS STS are allowed by default, and every other address, such as a Vault or a self-hosted Infisical, must be listed (`secrets.addresses`, which replaces `secrets.vault.addresses`). Someone who manages stores could otherwise change a store's address and collect a JWT that the real store accepts.

## Brokered

### The minting credential

| Kind | What the customer gives | How the control plane mints |
|---|---|---|
| `aws_secrets_manager` | nothing | Assumes `role_arn` with its own AWS identity and `ExternalId` set to the store's ID, with a session policy allowing `secretsmanager:GetSecretValue` on exactly the Deployment's secrets (by ARN, or `…:secret:<name>-??????` for a name) and `BatchGetSecretValue`, for 15 minutes. |
| `vault` | an AppRole `role_id` and `secret_id` | Logs in at `auth_mount`, then creates a non-renewable child token from `token_role` with a 15-minute TTL. |
| `doppler` | a service-account token that can manage the config's service tokens | Creates a read-only config service token named `ivaldi-<deployment ID>` that expires in 15 minutes. |
| `infisical` | a machine identity's Universal Auth client ID and secret | Logs in and hands over the access token, whose TTL the customer keeps short on the identity. |

AWS needs no stored credential: on the managed cloud the role trusts Ivaldi's AWS account, with the store's ID as the external ID against the confused-deputy problem. Self-hosted, the control plane uses whatever AWS identity it runs with.

Infisical scopes an access token to the identity, not the request, so a brokered Infisical store should have an identity per Environment. Doppler scopes a service token to one config, which is the store.

### Keeping it

- `PUT /v1/secret-stores/{id}/credential` sets or replaces it, under `secret_store.manage`. It is write-only: reads show `credential_set_at` and who set it, never the credential. `DELETE` removes it. Agent Sessions are already denied Secret Store permissions.
- It is stored in `credentials.secret_store_credentials`, one row per store, encrypted with envelope encryption: a data key per Organization encrypts it, and a key-encryption key protects the data key. That is ADR 0004's key-management interface, built here as `pkg/kms`. Its first implementation reads key-encryption keys from `credentials.key_encryption_key_files` (the first encrypts, every one decrypts, as for signing keys); AWS KMS and Vault Transit follow when a customer needs their own key.
- Setting, replacing, and removing it are audited, with no credential in the record.
- Brokering contacts the store's API from the control plane. A store the control plane cannot reach cannot be brokered; minting fails with 503 and the self-hosting guide points to `federated` or `agent`.

## Agent mode

The agent opens the store with its own identity, as today:

- `kubernetes`: its RBAC in the Target's namespace.
- `aws_secrets_manager`: the default AWS credential chain, which is EKS Pod Identity or IRSA in a cluster and an instance role, task role, or environment on a VM or platform, plus `role_arn` on top when set.
- `vault`: a JWT its runtime gives it, logged in at `auth_mount` as `role`. In Kubernetes that is the token projected for Vault's audience.
- `infisical` with `kubernetes`: the same, at Infisical's Kubernetes auth.
- `doppler`, and `infisical` with `universal_auth`: a named credential.

A **named credential** is a credential the agent's operator configures under a name, which a store's `credential` refers to. The Kubernetes agent maps names to Secrets in its namespace:

```yaml
secrets:
  credentials:
    doppler-production: { secretName: doppler-production, key: token }
```

A VM agent maps them to files, and a platform agent to files or environment variables. An Infisical Universal Auth credential is a JSON object with `client_id` and `client_secret`.

The heartbeat reports, besides `secret_store_kinds`, the access modes the agent can open stores in (`secret_store_modes`: `agent`, `federated`, `brokered`) and the credential names it has (`secret_store_credentials`), never their values. The deploy plan's `secret_stores_allowed` check refuses a store in a mode the Cluster's agent did not report, so an agent too old to ask for grants is not sent a federated or brokered store, and an `agent`-mode store whose credential the agent did not report. An agent that reports no modes, as one built before they were reported, opens stores in `agent` mode only.

## The agent

Executing an assignment gains one step before resolving secrets:

1. If any of the assignment's secrets is in a `federated` or `brokered` store, ask for grants.
2. Add every JWT, and every credential a store gives for one, to the `Masker` before using it, and remove them once the values are read: nothing uses them after.
3. Build each store from its grant: exchange a JWT, or use a brokered credential as is. Build `agent`-mode stores as today.
4. Resolve, fingerprint, and deliver as today.

Grants are never written to the state Secret, logged, or reported. A JWT is exchanged when its store is first read, seconds after the grant, and the store's credential serves the retries, so a grant is not asked for again. The agent drops an apply whose grant is refused with 409, because its Deployment is no longer applying, and reports nothing.

The assignment's `StoreSettings` carry the location and the mode, never a credential, so an assignment can still be logged after masking.

## Infisical

- A reference's `path` is the secret's name, or `folder/NAME` relative to the store's `secret_path`, and its `field` is a key of a JSON value.
- `GetMany` lists the folder once when every reference is in it, and reads by name otherwise.
- Versions: the v4 secrets API reads a past version by number (`GET /api/v4/secrets/{name}?version=`), so Infisical pins versions like Vault.

## Package layout

- `pkg/secretstore`: the readers, given a credential rather than how to get one. `TokenSource` (`func(ctx) (string, error)`) with `FileToken` and `EnvToken`. The exchanges the agent makes: Doppler OIDC, Infisical OIDC, JWT, Kubernetes, and Universal Auth logins, Vault JWT login, and AWS web identity. A new `infisical` reader. `Kinds` gains `KindInfisical`, and the package comment names the access modes.
- `pkg/kms`: the key-management interface and its local implementation.
- `apps/control-plane/internal/credentials`: access modes on `SecretStore`; `grants` (the operation, the grant records, revocation); `federation` (keys, discovery, signing); `brokers` (one file per kind, the only code that holds minting credentials).
- `apps/k8s-agent/internal/secrets`: asks for grants, maps named credentials, reports them.

## Migration

`credentials` gets a migration that only adds the `access` column, an object that defaults to `{}`. The database knows nothing about each kind's settings or access: their shape is the application's, checked by `SecretStore`'s constructor on create and on load, so a new kind such as Infisical needs no migration. An empty access is read as `agent` mode.

No control plane had been released, so no stored store has the old shape and nothing converts one. Where the old shape was:

| Kind | Before | After |
|---|---|---|
| `kubernetes` | `{}` | `agent`, `{}` |
| `aws_secrets_manager` | `region`, `role_arn` | `region`; `agent`, `role_arn` |
| `vault` | `address`, `mount`, `namespace`, `auth_role`, `auth_mount` | `address`, `mount`, `namespace`; `agent`, `role`, `auth_mount` (default `kubernetes`) |
| `doppler` | `token_secret_name`, `token_secret_key` | no project or config; `agent`, `credential` = `token_secret_name` |

The chart's `secrets.dopplerTokenSecrets` becomes `secrets.credentials`, keyed by the old Secret name with the key the store named, which the agent reads from `--secret-credentials` (`name=secret/key,…`). The assignment's `StoreSettings` change shape; no agent had been released, so the control plane serves the new shape to every agent.

## Invariants

- No secret value, grant credential, federated JWT, or minting credential appears in an event, audit record, report, log, API response other than the grant itself, or the agent's state.
- A grant is made only for a Deployment that is `applying`, to its Cluster's agent, for stores its secrets name that are active and allowed for its Environment.
- Every grant has an audit record.
- No agent holds a credential that can create credentials.
- A brokered store's minting credential is deleted when its mode or a location it is sent to changes.
- A federated JWT is sent only to an address the agent's operator allowed.

## Building it

Each step is its own pull request:

1. **Settings split.** Access modes on `SecretStore`, the migration, named credentials in the agent and chart, and Doppler's `project` and `config`. Every store is `agent`, so behaviour stays the same. Built: neither the control plane nor an agent had been released, so stored stores are not converted and the assignment's new `StoreSettings` shape is served to every agent.
2. **Grants and federated mode.** The issuer, the grant operation and records, the agent's grant step and exchanges for AWS, Vault, and Doppler, and `secrets.addresses`. Built: a federated store on an installation without federation keys fails the plan's `secret_stores_allowed` check through `SecretStoreRef.Unusable`, so delivery never learns what federation is.
3. **Infisical.** The kind, its reader, and its `agent` and `federated` exchanges. Built: an Infisical store's access names its `method`, and the access rules hold one rule per method. The agent reads with Infisical's v4 secrets API, and `secrets.infisical.kubernetesAuth` in the chart mounts the token for Kubernetes auth.
4. **Brokered mode.** `pkg/kms`, the credential API and table, the brokers, and revocation. Built, with these differences:
   - The key-encryption key is the existing `encryption.local_key_file` (provider `local`), not `credentials.key_encryption_key_files`. It holds one key, so rotating it needs re-wrapping each data key, which is not built. `aws-kms` and `vault-transit` refuse to start.
   - Infisical's API revokes an access token only given the token, which the control plane does not keep, so a brokered Infisical token is not revoked: it expires on the identity's TTL, like an AWS session. Its grant names it by a digest of the token.
   - The Vault broker leaves its AppRole login token to expire, so the token role should be orphan.
   - Doppler's API reference gives `expire_at` as a date-time and as a Unix timestamp; the broker sends an ISO 8601 date-time, not yet tried against Doppler.
   - Revocation runs on the outbox stream in its own consumer group (`secret_stores.revocation`), only on an installation with a key-encryption key, and records `secret_store.access_revoked` by the system. A grant minted while its Deployment leaves `applying` can miss the event, and expires.
5. **The web app.** Store forms for modes and credentials, and the self-hosting guide for each kind and mode. Built: the Secret Stores page (`delivery/secret-stores`) adds a store in any mode its kind allows, sets, replaces, and removes a brokered store's minting credential (write-only, and cleared from the page once saved), and shows a federated store's issuer, audience, and subject for each Environment. Changing a store reuses the form, with its kind fixed, and warns before a change of mode or destination removes a brokered store's credential.

## Open questions

- **Doppler's shortest token lifetime.** Whether a config service token can expire in 15 minutes. Doppler's CLI makes one-minute tokens, which suggests it can; untested against the API.
- **A policy rule for production grants.** Whether a grant for a production Environment needs a rule beyond the Deployment's approval ([ADR 0017](../adr/0017-control-plane-grants-secret-store-access.md#open-questions)).
- **Session credentials.** Whether ADR 0004's broker for Sessions reuses grants, with a Session in place of a Deployment.
