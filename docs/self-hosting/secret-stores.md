# Secret Stores

A **Secret Store** tells a Cluster's agent where to read an Application's secret variables from. Ivaldi keeps only where a secret is: the store, a path, an optional field, and an optional version. It never holds a secret or a credential for the store. The agent reads the secret inside your cluster when it deploys ([ADR 0004](../adr/0004-credentials-and-keys.md), [deployment variables](../design/deployment-variables.md)).

A store has **settings**, which say where it is, and an **access**, which says how it is opened, in one of three modes ([ADR 0017](../adr/0017-control-plane-grants-secret-store-access.md), [secret-store access](../design/secret-store-access.md)):

- **`agent`**: the agent opens the store with its own identity, or with a credential its operator configured on it under a name the store gives. Ivaldi can neither sign nor mint anything for the store.
- **`federated`**: for each Deployment, the control plane signs a JWT that lives five minutes, and the agent exchanges it at the store for the store's own short-lived credential. Your store trusts Ivaldi's issuer and decides what a JWT for a given Environment may read. The agent needs nothing store-specific. See [Federated mode](#federated-mode).
- **`brokered`**: you give the control plane a credential that can create narrower ones, and for each Deployment it mints one that is read-only and expires within minutes. The agent reads with it as it is. The control plane must reach the store's API. See [Brokered mode](#brokered-mode).

Add stores under **Secret Stores** in the web app, or with `POST /v1/workspaces/{workspace_id}/secret-stores`. A store belongs to a Workspace. Reading stores needs `secret_store.read` on the Workspace and changing them needs `secret_store.manage`.

## Which Environments may read a store

A store lists the Environments whose Deployments may read it. A store that lists none serves every Environment in its Workspace.

One agent serves a whole Cluster with one identity, so it can read every secret that identity is allowed to. Two things keep a staging Deployment from reading production's secrets:

- **In Ivaldi:** give production its own store and list only production on it. Ivaldi refuses a secret value scoped to an Environment the store does not list, and refuses a Deployment whose secrets are in a store its Environment may not read.
- **In your secret manager:** scope the policy or role the agent's identity holds as narrowly as you need.

## Kinds

### `kubernetes`

Reads Secrets in the namespace the Target deploys to. The path is the Secret's name, and the field is one of its keys. It has no settings.

The agent may read Secrets only in the namespaces you list in the agent chart:

```yaml
secrets:
  kubernetesNamespaces: [payments, checkout]
```

Use this store when another tool, such as External Secrets or the Doppler operator, already writes the Secret.

### `aws_secrets_manager`

Settings: the AWS `region`. Access: optionally a `role_arn` the agent assumes on top of its own identity. The path is the secret's name or ARN, and the field is one key of a JSON secret.

Give the agent's ServiceAccount an AWS identity with EKS Pod Identity or IRSA:

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/ivaldi-agent
```

The role needs `secretsmanager:GetSecretValue` on the secrets it should read.

### `vault`

Settings: Vault's `address`, the KV v2 `mount`, and optionally an Enterprise `namespace`. Access: the Kubernetes auth `role` the agent logs in as, and optionally the `auth_mount` (default `kubernetes`). The path is the secret's path within the mount, and the field is one of its keys.

Bind the role to the agent's ServiceAccount and namespace in Vault's Kubernetes auth method, with a policy that can read the paths it should.

The agent logs in by sending Vault a service-account token, or a federated JWT, and a store's address is set in Ivaldi by whoever manages the Workspace's stores. So the agent's operator decides which servers it may log in to, in the agent chart's values:

```yaml
secrets:
  addresses:
    - https://vault.example.com
  vault:
    audience: vault
```

- A `vault` store at any other address fails the Deployment before anything is sent, and an agent with no addresses does not offer to read `vault` stores at all, so the deploy plan refuses them.
- The token is projected for `audience` (default `vault`), never the Pod's Kubernetes API token, so a Vault that receives it cannot use it on the cluster's API. Set the same `audience` on the Kubernetes auth role, or Vault rejects the login.
- The agent follows no redirect from Vault. Give it the address that answers, such as the active node's or a load balancer's.

### `doppler`

Settings: optionally the Doppler `project` and `config`, both or neither. Access: the name of the `credential` that holds the Doppler token, or in `federated` mode the `identity_id` of a service account identity. The path is the secret's name in Doppler.

A Doppler store serves one config, so it must list the Environments that config is for. A service token implies its config; another token, such as a service account's, needs the store's `project` and `config`.

A store names a credential, never where it is kept. The agent's operator maps each name to a Secret in the agent's namespace, and its key, in the chart; the agent may read only those Secrets, and a Deployment whose store names a credential the Cluster's agent does not have is refused before it starts:

```yaml
secrets:
  credentials:
    doppler-production: { secretName: doppler-production, key: token }
```

### `infisical`

Settings: the `project_id`, the `environment` slug, optionally the `site_url` (Infisical Cloud's US site, `https://app.infisical.com`, by default), and optionally the `secret_path` folder (`/` by default). Access: a `method`, which in `agent` mode is one of:

- `universal_auth`, with the name of the `credential` that holds a machine identity's Universal Auth client ID and secret, as a JSON object:

  ```yaml
  secrets:
    credentials:
      infisical-production: { secretName: infisical-production, key: universal-auth }
  ```

  ```sh
  kubectl -n ivaldi-system create secret generic infisical-production \
    --from-literal=universal-auth='{"client_id":"…","client_secret":"…"}'
  ```

- `kubernetes`, with the `identity_id` of a machine identity whose Kubernetes auth accepts the agent's ServiceAccount. The chart mounts a token for it with `secrets.infisical.kubernetesAuth: true`, made for the audience `secrets.infisical.audience` (default `infisical`); set the same Allowed Audience on the identity. Infisical checks the token with your cluster's TokenReview API, so it must reach the cluster's API server.

The path is the secret's name, or `folder/NAME` relative to the store's folder; it cannot leave that folder. The field is one key of a JSON value, and the version is Infisical's version number. Secret references in values are expanded, and secrets the folder imports are read, as Infisical does by default.

The agent logs in to Infisical Cloud (`app.infisical.com`, `us.infisical.com`, `eu.infisical.com`) without being told to. A self-hosted Infisical must be listed in `secrets.addresses`, because a login sends it a credential.

A store reads one Infisical environment, so it should list the Ivaldi Environments that environment is for.

## Federated mode

In `federated` mode the control plane is an OIDC issuer for your stores. Before an agent reads a Deployment's secrets, it asks the control plane for access (`POST /v1/agent/deployments/{id}/secret-grants`). The control plane checks the Deployment is applying on that agent's Cluster, and that each store is active and allowed for its Environment. It then signs one JWT per store, records the grant, and writes an audit record (`secret_store.access_granted`) on the Deployment. The agent exchanges each JWT at its store and never stores, logs, or reports it.

### The issuer

Give the issuer its own keys, PEM private keys as for `auth.signing_key_files` but never the same ones:

```yaml
secret_stores:
  federation:
    signing_key_files: [/etc/ivaldi/federation/current.pem]
```

`IVALDI_SECRET_STORES_FEDERATION_SIGNING_KEY_FILES` sets the same list. Without keys, no store can be set to `federated`, and a Deployment that reads one is refused before it starts.

| | |
|---|---|
| Issuer | `<external_url>/federation` |
| Discovery | `<external_url>/federation/.well-known/openid-configuration` |
| Keys | `<external_url>/federation/jwks.json` |
| Audience | `ivaldi:<store ID>`, fixed and unique to each store |
| Subject | `org:<Organization ID>:store:<store ID>:env:<Environment ID>` |
| Other claims | `org`, `workspace`, `project`, `application`, `environment`, `environment_tier`, `target`, `cluster`, `deployment`, `release`: IDs, and the Environment's tier |

The first key signs and every key is published. To rotate, put a new key first, and remove the old one five minutes after the control plane stops signing with it. A store that copied the keys rather than fetching them must be given the new key first.

The web app shows each federated store's issuer, audience, and subjects under **Set-up for the store**. Every store must check the audience **and** the subject, or the Environment claims it binds. One issuer serves every Organization on Ivaldi's managed cloud. The audience already keeps one tenant's JWT out of another's store, and the subject keeps staging's Deployments out of production's secrets.

### AWS Secrets Manager

Access: the `role_arn` to assume, which is required. The agent calls `AssumeRoleWithWebIdentity` with the JWT, with the session named `ivaldi-<Deployment ID>`, which CloudTrail records. It needs no AWS credentials of its own.

1. In IAM, add an OpenID Connect identity provider with the issuer URL, and the store's audience, `ivaldi:<store ID>`, as its client ID. AWS fetches the discovery document, so the control plane's external URL must be public HTTPS.
2. Give the role a trust policy that allows `sts:AssumeRoleWithWebIdentity` from that provider, with conditions on both the audience and the subject:

```json
"Condition": {
  "StringEquals": {
    "ivaldi.example.com/federation:aud": "ivaldi:sst_…",
    "ivaldi.example.com/federation:sub": "org:org_…:store:sst_…:env:env_…"
  }
}
```

3. Give the role `secretsmanager:GetSecretValue`, and `secretsmanager:BatchGetSecretValue`, on the secrets that Environment may read.

### Vault

Access: the JWT auth `role` to log in as, and optionally the `auth_mount` (default `jwt`). The agent logs in only at an address in `secrets.addresses`.

Enable a JWT auth method. Point it at the issuer with `oidc_discovery_url` if Vault can reach the control plane. If it cannot, give it the keys with `jwt_validation_pubkeys`, and update them when the keys rotate. Then bind a role to the store:

```sh
vault write auth/jwt/role/payments-production \
  role_type=jwt user_claim=sub \
  bound_audiences="ivaldi:sst_…" \
  bound_subject="org:org_…:store:sst_…:env:env_…" \
  token_policies=payments-production token_ttl=10m
```

`bound_claims` can also match `application`, `target`, or `environment_tier`.

### Doppler

Settings: the `project` and `config`, which are required, because a service account's token can read any config its account may. Access: the `identity_id` of a service account identity. Identities need Doppler's Team or Enterprise plan.

On the service account, add an identity with the issuer as its discovery URL, the store's audience, and the subject. Give the service account read access to the config only. The agent exchanges the JWT at `https://api.doppler.com/v3/auth/oidc`, which is always allowed, and reads the config with the token it gets.

### Infisical

Access: the `identity_id` of a machine identity, and the `method` it logs in with:

- `oidc` when Infisical can reach the control plane: give the identity OIDC auth with the issuer as its discovery URL.
- `jwt` when it cannot: give the identity JWT auth with static keys, the keys at `<external_url>/federation/jwks.json` as PEM, and update them when the keys rotate.

Either way, set the issuer, the store's audience as the bound audience, and the subject as the bound subject. Bound claims can also match `application`, `target`, or `environment_tier`. Give the identity read access to the store's environment and folder only, and keep its access token TTL short: the agent uses the token only while it reads one Deployment's secrets.

## Brokered mode

In `brokered` mode the control plane holds a **minting credential** for the store, and mints a credential for each Deployment when its agent asks for access (`POST /v1/agent/deployments/{id}/secret-grants`), as in federated mode. Each one is read-only, lasts 15 minutes where the store lets the control plane choose, is recorded as a grant and an audit record (`secret_store.access_granted`) on the Deployment, and goes only to the agent. The response is all or nothing: if one store refuses, nothing is granted, and what was already minted is revoked.

When the Deployment leaves `applying`, the control plane revokes what it minted for it, where the store can (`secret_store.access_revoked`). An AWS session and an Infisical access token cannot be revoked one by one, and expire instead.

### The key-encryption key

Minting credentials are kept encrypted with envelope encryption: each Organization's data key seals its stores' credentials, and the installation's key-encryption key wraps the data keys. The key-encryption key is never stored in the database. Give the control plane one, 32 bytes, raw or as base64 or hex:

```sh
head -c 32 /dev/urandom | base64 > /etc/ivaldi/kek
```

```yaml
encryption:
  provider: local
  local_key_file: /etc/ivaldi/kek
```

`IVALDI_ENCRYPTION_LOCAL_KEY_FILE` sets the same file. Without it, no store can be set to `brokered`, and a Deployment that reads one is refused before it starts. Back the key up: without it, the stored credentials cannot be opened, and each brokered store's credential must be set again. `aws-kms` and `vault-transit` are not supported yet, and the control plane refuses to start with them.

### The minting credential

Set it with **Set credential** on the store under **Secret Stores** in the web app, or with `PUT /v1/secret-stores/{id}/credential`, under `secret_store.manage` on the Workspace; remove it there or with `DELETE`. It is never read back: the store shows only `credential_set_at` and `credential_set_by`. Setting and removing it are audited, without the credential.

Changing the store's mode, or where its credential is sent (the Vault `address`, the Doppler `project` or `config`, the Infisical `site_url` or `project_id`, or the AWS `region`), or archiving the store, removes the credential, so it must be set again. Nobody who manages stores can point one at their own server and have the control plane send it there.

### AWS Secrets Manager

Access: the `role_arn` to assume, which is required. There is no minting credential: the control plane assumes the role with its own AWS identity, such as its Pod's IAM role, with the store's ID as the external ID, for 15 minutes, under a session policy that allows `secretsmanager:GetSecretValue` on only the Deployment's secrets, `secretsmanager:BatchGetSecretValue`, and `kms:Decrypt` through Secrets Manager.

Give the role a trust policy that allows the control plane's identity to assume it, with the external ID:

```json
"Condition": { "StringEquals": { "sts:ExternalId": "sst_…" } }
```

and permission to read the secrets that store's Environments may read. The session policy narrows that to one Deployment's secrets. A Deployment that reads very many secrets in one store can exceed AWS's session policy size, and its grant is refused.

### Vault

Access: the `token_role` to create each Deployment's token from, and optionally the `auth_mount` of the AppRole auth method (default `approle`). Credential: the AppRole's `role_id` and `secret_id`.

The control plane logs in with the AppRole and creates a non-renewable token from the token role with a 15-minute TTL, and revokes it by accessor when the Deployment leaves `applying`. Make the token role orphan, so the token does not end with the AppRole login, and let the AppRole's policy create tokens from that role and revoke them by accessor:

```sh
vault write auth/token/roles/payments-production \
  allowed_policies=payments-production orphan=true renewable=false token_explicit_max_ttl=15m
vault policy write ivaldi-broker - <<'HCL'
path "auth/token/create/payments-production" { capabilities = ["update"] }
path "auth/token/revoke-accessor"            { capabilities = ["update"] }
HCL
vault write auth/approle/role/ivaldi-broker token_policies=ivaldi-broker token_ttl=5m
```

The agent sends the token only to an address in `secrets.addresses`.

### Doppler

Settings: the `project` and `config`, which are required. Credential: the `token` of a service account that can manage the config's service tokens (the project permission Manage Service Tokens). The control plane creates a read-only service token for the config, named `ivaldi-<Deployment ID>`, that expires in 15 minutes, and deletes it when the Deployment leaves `applying`.

### Infisical

Credential: a machine identity's Universal Auth `client_id` and `client_secret`. The control plane logs in and hands the access token to the agent. Infisical scopes the token to the identity, not the request, and revokes one only given the token itself, which the control plane never keeps, so the token is not revoked. Give each Environment's store its own identity, with read access to that environment's folder only, and set its access token TTL short, such as 15 minutes.

## Versions and rollbacks

A secret value may pin a version. Without one, each Deployment reads the latest, which is how a redeploy picks up a rotated secret.

The agent reports the version it read and a keyed fingerprint of the value, and the Deployment records both; never the value. The fingerprint's key stays in the cluster, in the Secret `fingerprint.secretName` names. Back that Secret up with the cluster's others: a rollback compares against fingerprints made with it.

A rollback reads the versions the Deployment it restores recorded, so keep enough history in the store. Vault's KV v2 keeps ten versions by default, AWS Secrets Manager removes versions that have no staging label, and Infisical keeps versions for as long as its settings say. Doppler has no per-secret versions, so a rollback checks the value against the fingerprint it recorded instead.

## How the chart receives a secret

By default the agent writes one immutable Kubernetes Secret per Deployment in the Target's namespace, with one key per secret variable, and passes its name to the chart as `global.ivaldi.secrets.name`.

A secret variable can instead be delivered as a value, written into the chart's values at its values path, for charts that cannot read a Secret. Helm keeps values in its release history, so the secret then sits in the namespace's Helm release Secrets and shows in `helm get values`.
